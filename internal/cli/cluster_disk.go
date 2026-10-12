package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDiskStallPct = 90
	diskPurgeMinAge     = 15 * time.Minute // orphan clone dirs older than this
)

// clusterCleanDisk purges worker disk pressure: orphaned /tmp/truffles-since-*
// clones and (by default) workdir scan artifacts already copied locally.
func clusterCleanDisk(rest []string) error {
	fs := flag.NewFlagSet("cluster clean-disk", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster / playbook YAML")
	threshold := fs.Int("threshold", defaultDiskStallPct, "warn when disk use% ≥ this")
	keepArtifacts := fs.Bool("keep-artifacts", false, "do not delete workdir results/scanned/clean/skip files")
	orphansOnly := fs.Bool("orphans-only", false, "only remove stale /tmp/truffles-since-* clones")
	minAge := fs.Duration("min-age", diskPurgeMinAge, "only delete clone dirs older than this")
	dryRun := fs.Bool("dry-run", false, "print actions without deleting")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	rows := cleanDiskAllWorkers(c, diskCleanOpts{
		Threshold:     *threshold,
		KeepArtifacts: *keepArtifacts,
		OrphansOnly:   *orphansOnly,
		MinAge:        *minAge,
		DryRun:        *dryRun,
	})
	printDiskCleanReport(rows, *dryRun)
	for _, r := range rows {
		if r.Err != "" && r.SSH == "ok" {
			return fmt.Errorf("disk clean had errors (see table)")
		}
	}
	return nil
}

type diskCleanOpts struct {
	Threshold     int
	KeepArtifacts bool
	OrphansOnly   bool
	MinAge        time.Duration
	DryRun        bool
	// AfterCollect: always purge workdir artifacts (results already on master).
	AfterCollect bool
	Quiet        bool // suppress per-worker progress lines
}

type diskCleanRow struct {
	Name       string
	Host       string
	SSH        string
	DiskBefore string
	DiskAfter  string
	FreedMB    int
	Actions    string
	Err        string
}

func cleanDiskAllWorkers(c *Cluster, opts diskCleanOpts) []diskCleanRow {
	enabled := enabledSlaves(c)
	out := make([]diskCleanRow, len(enabled))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, sl := range enabled {
		i, sl := i, sl
		wg.Add(1)
		go func() {
			defer wg.Done()
			row := cleanDiskOneWorker(sl, opts)
			out[i] = row
			if opts.Quiet {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			status := row.DiskBefore + "→" + row.DiskAfter
			if row.SSH != "ok" {
				status = row.SSH
			}
			fmt.Fprintf(os.Stderr, "[*] clean-disk %s %s %s\n", row.Name, status, utf8Trunc(row.Actions, 60))
		}()
	}
	wg.Wait()
	return out
}

func cleanDiskOneWorker(sl Slave, opts diskCleanOpts) diskCleanRow {
	row := diskCleanRow{Name: sl.Name, Host: sl.Host}
	before, err := remoteDiskUsePercent(sl)
	if err != nil {
		row.SSH = "down"
		row.Err = shortErr(err)
		return row
	}
	row.SSH = "ok"
	row.DiskBefore = fmt.Sprintf("%d%%", before)

	script := buildDiskCleanScript(slaveWorkDir(sl), opts)
	cmd := sshCmdConnect(sl, script, 20)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Hard cap: rm -rf of huge trees can block forever on a full disk.
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err = <-done:
		if err != nil {
			row.Err = shortErr(fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String())))
			return row
		}
	case <-time.After(2 * time.Minute):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		row.Err = "timeout after 2m (disk may still be reclaiming)"
		row.Actions = row.Err
		return row
	}
	freed, actions := parseDiskCleanOutput(strings.TrimSpace(stdout.String()))
	row.FreedMB = freed
	row.Actions = actions
	after, aerr := remoteDiskUsePercent(sl)
	if aerr == nil {
		row.DiskAfter = fmt.Sprintf("%d%%", after)
	} else {
		row.DiskAfter = row.DiskBefore
	}
	return row
}

func remoteDiskUsePercent(sl Slave) (int, error) {
	cmd := sshCmdConnect(sl, `df -P / | awk 'NR==2{gsub(/%/,"",$5); print $5}'`, 10)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse df: %q", strings.TrimSpace(string(out)))
	}
	return n, nil
}

func buildDiskCleanScript(workdir string, opts diskCleanOpts) string {
	wd := shellSingleQuote(workdir)
	minSec := int(opts.MinAge.Seconds())
	if minSec < 0 {
		minSec = 0
	}
	dry := "0"
	if opts.DryRun {
		dry = "1"
	}
	purgeArtifacts := "1"
	if opts.KeepArtifacts || (opts.OrphansOnly && !opts.AfterCollect) {
		purgeArtifacts = "0"
	}
	if opts.AfterCollect {
		purgeArtifacts = "1"
	}
	// Fast path: no du(1) on multi-GB clones (hangs when disk is 99% full).
	// Count removed dirs/files; FREED_MB stays 0 (unknown) unless small artifacts.
	return fmt.Sprintf(`set +e
DRY=%s
PURGE_ART=%s
MIN_AGE=%d
WD=%s
removed=0
actions=""
add() { actions="${actions}${actions:+; }$1"; }
mins=$((MIN_AGE/60))
[ "$mins" -lt 0 ] && mins=0
# Orphaned full-clone temp dirs (scan uses MkdirTemp truffles-since-*).
# Skip dirs newer than MIN_AGE so in-flight clones survive.
for base in /tmp /var/tmp; do
  [ -d "$base" ] || continue
  if [ "$mins" -gt 0 ]; then
    list=$(find "$base" -maxdepth 1 -type d -name 'truffles-since-*' -mmin +"$mins" 2>/dev/null)
  else
    list=$(find "$base" -maxdepth 1 -type d -name 'truffles-since-*' 2>/dev/null)
  fi
  for d in $list; do
    [ -d "$d" ] || continue
    bn=$(basename "$d")
    if [ "$DRY" = 1 ]; then
      add "would_rm $bn"
      removed=$((removed+1))
    else
      rm -rf "$d" 2>/dev/null && { add "rm $bn"; removed=$((removed+1)); }
    fi
  done
done
# Workdir artifacts (safe after collect).
if [ "$PURGE_ART" = 1 ] && [ -d "$WD" ]; then
  for pat in results-*.csv results-*.txt results-*.jsonl scanned-*.txt clean-*.json skip-repos.txt repos.txt; do
    for f in "$WD"/$pat; do
      [ -e "$f" ] || continue
      bn=$(basename "$f")
      if [ "$DRY" = 1 ]; then
        add "would_rm $bn"
        removed=$((removed+1))
      else
        rm -f "$f" 2>/dev/null && { add "rm $bn"; removed=$((removed+1)); }
      fi
    done
  done
fi
if [ "$DRY" != 1 ]; then
  rm -rf /tmp/trufflehog* 2>/dev/null
fi
printf 'FREED_MB=0|ACTIONS=%%s (%%s items)\n' "$actions" "$removed"
`, dry, purgeArtifacts, minSec, wd)
}

func parseDiskCleanOutput(line string) (freed int, actions string) {
	for _, part := range strings.Split(line, "|") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "FREED_MB":
			freed, _ = strconv.Atoi(strings.TrimSpace(kv[1]))
		case "ACTIONS":
			actions = strings.TrimSpace(kv[1])
		}
	}
	if actions == "" {
		actions = "nothing to purge"
	}
	return freed, actions
}

func printDiskCleanReport(rows []diskCleanRow, dry bool) {
	p := palette{on: resolveColor("auto", os.Stdout)}
	title := "Disk clean"
	if dry {
		title = "Disk clean (dry-run)"
	}
	fmt.Fprintln(os.Stdout)
	p.section(os.Stdout, title)
	headers := []string{"Name", "Host", "SSH", "Before", "After", "Freed", "Actions"}
	var table [][]string
	for _, r := range rows {
		act := r.Actions
		if r.Err != "" {
			act = r.Err
		}
		if trunc := utf8Trunc(act, 48); trunc != act {
			act = trunc + "…"
		}
		freed := "—"
		if r.FreedMB > 0 {
			freed = fmt.Sprintf("%dM", r.FreedMB)
		}
		table = append(table, []string{
			r.Name, r.Host, r.SSH,
			emptyDash(r.DiskBefore), emptyDash(r.DiskAfter),
			freed, emptyDash(act),
		})
	}
	printColorTable(os.Stdout, p, headers, table, map[int]func(string) string{2: sshColor(p)})
	fmt.Fprintln(os.Stdout)
}

func utf8Trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ensureWorkerDiskSpace purges orphans when disk ≥ threshold, then stalls until
// under threshold (or maxWait). Used before staging a remote scan on a slave.
func ensureWorkerDiskSpace(sl Slave, threshold int, maxWait time.Duration) error {
	if threshold <= 0 {
		threshold = defaultDiskStallPct
	}
	pct, err := remoteDiskUsePercent(sl)
	if err != nil {
		return nil // don't block scan on probe failure
	}
	if pct < threshold {
		return nil
	}
	fmt.Fprintf(os.Stderr, "[!] %s disk %d%% ≥ %d%% — purging orphans…\n", sl.Name, pct, threshold)
	_ = cleanDiskOneWorker(sl, diskCleanOpts{
		Threshold:   threshold,
		OrphansOnly: true,
		MinAge:      2 * time.Minute,
	})
	deadline := time.Now().Add(maxWait)
	for {
		pct, err = remoteDiskUsePercent(sl)
		if err != nil {
			return nil
		}
		if pct < threshold {
			fmt.Fprintf(os.Stderr, "[+] %s disk now %d%% — continuing\n", sl.Name, pct)
			return nil
		}
		if maxWait > 0 && time.Now().After(deadline) {
			return fmt.Errorf("%s disk still %d%% after purge/wait (threshold %d%%)", sl.Name, pct, threshold)
		}
		fmt.Fprintf(os.Stderr, "[*] %s disk %d%% — stalling 30s (awaiting space)…\n", sl.Name, pct)
		_ = cleanDiskOneWorker(sl, diskCleanOpts{OrphansOnly: true, MinAge: time.Minute})
		time.Sleep(30 * time.Second)
		if maxWait <= 0 {
			// unbounded stall — keep looping until space frees
			continue
		}
	}
}

// purgeWorkerArtifactsAfterCollect removes remote result files after a successful
// local copy so worker disks do not accumulate CSV/skip lists between cycles.
func purgeWorkerArtifactsAfterCollect(c *Cluster) {
	rows := cleanDiskAllWorkers(c, diskCleanOpts{
		AfterCollect: true,
		MinAge:       5 * time.Minute,
		Quiet:        true,
	})
	var freed int
	for _, r := range rows {
		freed += r.FreedMB
		if r.SSH == "ok" && (r.FreedMB > 0 || r.DiskBefore != r.DiskAfter) {
			fmt.Printf("[*] cleaned %s disk %s→%s (freed ~%dM)\n", r.Name, r.DiskBefore, r.DiskAfter, r.FreedMB)
		}
	}
	if freed > 0 {
		CoreLog("cluster-disk", fmt.Sprintf("post-collect purged ~%dM across workers", freed))
	}
}

// localDiskUsePercent returns use%% for the filesystem containing path.
func localDiskUsePercent(path string) (int, error) {
	if path == "" {
		path = "."
	}
	cmd := exec.Command("df", "-P", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("df: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("df: unexpected output")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 5 {
		return 0, fmt.Errorf("df: bad line %q", lines[len(lines)-1])
	}
	return strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
}

// ensureLocalDiskSpace stalls the local scan process when the volume is ≥ threshold.
// Purges orphan /tmp/truffles-since-* dirs first. Called between repos.
func ensureLocalDiskSpace(threshold int) {
	if threshold <= 0 {
		threshold = defaultDiskStallPct
	}
	pct, err := localDiskUsePercent(".")
	if err != nil {
		return
	}
	if pct < threshold {
		return
	}
	fmt.Fprintf(os.Stderr, "[!] local disk %d%% ≥ %d%% — purging orphan clones…\n", pct, threshold)
	purgeLocalOrphanClones(2 * time.Minute)
	for {
		pct, err = localDiskUsePercent(".")
		if err != nil {
			return
		}
		if pct < threshold {
			fmt.Fprintf(os.Stderr, "[+] local disk now %d%% — continuing\n", pct)
			return
		}
		fmt.Fprintf(os.Stderr, "[*] local disk %d%% — stalling scan 30s…\n", pct)
		purgeLocalOrphanClones(time.Minute)
		time.Sleep(30 * time.Second)
	}
}

func purgeLocalOrphanClones(minAge time.Duration) {
	for _, pattern := range []string{
		filepath.Join(os.TempDir(), "truffles-since-*"),
		"/tmp/truffles-since-*",
		"/var/tmp/truffles-since-*",
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, d := range matches {
			st, err := os.Stat(d)
			if err != nil || !st.IsDir() {
				continue
			}
			if time.Since(st.ModTime()) < minAge {
				continue
			}
			_ = os.RemoveAll(d)
		}
	}
}
