package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// jobStatusOpts controls `truffles job status` rendering.
type jobStatusOpts struct {
	ID      string
	JobsDir string
	Full    bool
	Color   string
}

func parseJobStatusArgs(args []string) (jobStatusOpts, error) {
	opts := jobStatusOpts{JobsDir: defaultJobsDir, Color: "auto"}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "-help":
			return opts, flagErrHelp
		case a == "--full" || a == "-full":
			opts.Full = true
		case a == "--jobs-dir" || a == "-jobs-dir":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			opts.JobsDir = args[i+1]
			i++
		case strings.HasPrefix(a, "--jobs-dir="):
			opts.JobsDir = strings.TrimPrefix(a, "--jobs-dir=")
		case a == "--color" || a == "-color":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			opts.Color = args[i+1]
			i++
		case strings.HasPrefix(a, "--color="):
			opts.Color = strings.TrimPrefix(a, "--color=")
		case strings.HasPrefix(a, "-"):
			return opts, fmt.Errorf("unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) > 0 {
		opts.ID = pos[0]
	}
	return opts, nil
}

// flagErrHelp is distinguished so callers can print job usage.
var flagErrHelp = fmt.Errorf("help")

func jobStatus(args []string) error {
	opts, err := parseJobStatusArgs(args)
	if err != nil {
		if err == flagErrHelp {
			return jobUsage()
		}
		return fmt.Errorf("usage: truffles job status [id] [--full]: %w", err)
	}
	root, err := filepath.Abs(opts.JobsDir)
	if err != nil {
		return err
	}
	id := opts.ID
	if id == "" {
		id, err = latestJobID(root)
		if err != nil {
			return err
		}
	} else {
		id, err = resolveJobID(root, id)
		if err != nil {
			return err
		}
	}
	m, err := loadJobMeta(root, id)
	if err != nil {
		return fmt.Errorf("job %s: %w", id, err)
	}
	m, _ = refreshJobStatus(root, m)
	enrichJobMetaFromCmd(m)

	rep, err := gatherJobStatusReport(m, opts.Full)
	if err != nil {
		return err
	}
	p := palette{on: resolveColor(opts.Color, os.Stdout)}
	printJobStatusReport(os.Stdout, p, rep)
	return nil
}

func latestJobID(root string) (string, error) {
	ids, err := listJobIDs(root)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no jobs (start with: truffles job start -c remote-playbook.yaml)")
	}
	// Prefer a running job; else newest id (lexicographic = chronological for j-YYYYMMDD-HHMMSS).
	var running []string
	for _, id := range ids {
		m, err := loadJobMeta(root, id)
		if err != nil {
			continue
		}
		m, _ = refreshJobStatus(root, m)
		if m.Status == jobStatusRunning && pidAlive(m.PID) {
			running = append(running, id)
		}
	}
	if len(running) == 1 {
		return running[0], nil
	}
	if len(running) > 1 {
		sort.Strings(running)
		return running[len(running)-1], nil
	}
	return ids[len(ids)-1], nil
}

// enrichJobMetaFromCmd fills TermsFile/DataDir/Workers from Cmd when older
// meta.json lacked those fields (current overnight job keeps running).
func enrichJobMetaFromCmd(m *jobMeta) {
	if m == nil || m.Cmd == "" {
		return
	}
	parts := splitCmdArgs(m.Cmd)
	for i := 0; i < len(parts); i++ {
		a := parts[i]
		next := func() string {
			if i+1 < len(parts) {
				i++
				return parts[i]
			}
			return ""
		}
		switch {
		case a == "--terms-file" || a == "-terms-file":
			if v := next(); v != "" && m.TermsFile == "" {
				m.TermsFile = v
			}
		case strings.HasPrefix(a, "--terms-file="):
			if m.TermsFile == "" {
				m.TermsFile = strings.TrimPrefix(a, "--terms-file=")
			}
		case a == "--data-dir" || a == "-data-dir":
			if v := next(); v != "" && m.DataDir == "" {
				m.DataDir = v
			}
		case strings.HasPrefix(a, "--data-dir="):
			if m.DataDir == "" {
				m.DataDir = strings.TrimPrefix(a, "--data-dir=")
			}
		case a == "--workers" || a == "-workers":
			if v := next(); v != "" && m.Workers == 0 {
				m.Workers, _ = strconv.Atoi(v)
			}
		case strings.HasPrefix(a, "--workers="):
			if m.Workers == 0 {
				m.Workers, _ = strconv.Atoi(strings.TrimPrefix(a, "--workers="))
			}
		case a == "--prefix-file" || a == "-prefix-file":
			if v := next(); v != "" && m.PrefixFile == "" {
				m.PrefixFile = v
			}
		}
	}
	if m.DataDir == "" {
		m.DataDir = "data"
	}
}

func splitCmdArgs(cmd string) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	for _, r := range cmd {
		switch {
		case r == '"' || r == '\'':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

type jobStatusReport struct {
	Meta       *jobMeta
	Alive      bool
	Uptime     time.Duration
	DataDir    string
	Phase      string
	Cycle      int
	LogRepos   int // last reported search/cluster repo count from log
	LogSnippet []string

	// Live scan progress aggregated from worker heartbeats in the job log.
	ScanDone     int
	ScanTotal    int
	ScanFailed   int
	ScanInFlight int
	ScanFindings int // live heartbeat findings (may lag CSV)
	ScanVerified int
	ScanETA      string

	// Lifetime skip-list stats (--full only).
	Scanned int
	Clean   int

	// This job session (artifacts with stamp >= job start).
	SessionPulled   int
	SessionRuns     int
	SessionFindings int
	SessionVerified int
	SessionReposHit int // unique repos with ≥1 finding this session
	LifeFindings    int
	LifeVerified    int
	LifeReposHit    int

	Workers      []workerStatusRow
	Full         bool
	PlaybookGCP  bool // any slave has gce_instance
	CostEstimate *fleetCostEstimate
}

type workerStatusRow struct {
	Name         string
	Host         string
	Enabled      bool
	Workers      int
	SSH          string // ok | down | skipped
	Uptime       string
	Load         string
	CPUPct       string
	Mem          string // used/total MiB
	Disk         string // use%
	DiskPct      int    // parsed use%, 0 if unknown
	Machine      string
	GCEStatus    string
	GCEInstance  string
	GCEZone      string
	GCEProject   string
	CPUs         int
	MemTotalMB   int
	DiskTotalGB  int
	ReposScanned int // this job session (from scanned-<name>.txt)
	IsGCE        bool
	Err          string
}

func gatherJobStatusReport(m *jobMeta, full bool) (*jobStatusReport, error) {
	dataDir := m.DataDir
	if dataDir == "" {
		dataDir = "data"
	}
	started, _ := time.Parse(time.RFC3339, m.StartedAt)
	alive := pidAlive(m.PID)
	var up time.Duration
	if !started.IsZero() {
		up = time.Since(started)
	}

	rep := &jobStatusReport{
		Meta:    m,
		Alive:   alive,
		Uptime:  up,
		DataDir: dataDir,
		Full:    full,
	}

	stamp := jobStartStamp(m)
	scannedPath := filepath.Join(dataDir, "scanned-repos.txt")
	cleanPath := filepath.Join(dataDir, "clean-repos.txt")
	if n, err := countURLLinesErr(scannedPath); err == nil {
		rep.Scanned = n
	}
	if n, err := countURLLinesErr(cleanPath); err == nil {
		rep.Clean = n
	}

	sessionDirs, pulled, runs := collectJobSessionArtifacts(dataDir, stamp)
	rep.SessionPulled = pulled
	rep.SessionRuns = runs

	find, ver, hit := aggregateCSVStats(sessionDirs)
	rep.SessionFindings = find
	rep.SessionVerified = ver
	rep.SessionReposHit = hit

	if full {
		lifeDirs := listRemoteResultDirs(dataDir, "")
		lf, lv, lh := aggregateCSVStats(lifeDirs)
		consAll := filepath.Join(dataDir, "consolidated", "results-all.csv")
		consVer := filepath.Join(dataDir, "consolidated", "results-all-verified.csv")
		if n, err := countCSVDataRows(consAll); err == nil && n > 0 {
			lf = n
		}
		if n, err := countCSVDataRows(consVer); err == nil && n > 0 {
			lv = n
		}
		if u := countUniqueReposInCSV(consAll); u > 0 {
			lh = u
		}
		rep.LifeFindings = lf
		rep.LifeVerified = lv
		rep.LifeReposHit = lh
	}

	prog := parseJobLogProgress(m.LogPath)
	rep.Phase = prog.Phase
	rep.Cycle = prog.Cycle
	rep.LogRepos = prog.Repos
	rep.LogSnippet = prog.Snippet
	rep.ScanDone = prog.ScanDone
	rep.ScanTotal = prog.ScanTotal
	rep.ScanFailed = prog.ScanFailed
	rep.ScanInFlight = prog.ScanInFlight
	rep.ScanFindings = prog.ScanFindings
	rep.ScanVerified = prog.ScanVerified
	rep.ScanETA = prog.ScanETA

	if m.Playbook != "" {
		if c, err := loadCluster(m.Playbook); err == nil {
			rep.PlaybookGCP = clusterHasGCE(c)
			// Always probe CPU/mem/disk; GCE machine-type describe only with --full.
			rep.Workers = probeWorkerStatus(c, full)
			attachWorkerSessionRepos(rep.Workers, sessionDirs)
			if full {
				est := estimateFleetCost(rep.Workers, up, rep.PlaybookGCP)
				rep.CostEstimate = &est
			}
		}
	}
	return rep, nil
}

// attachWorkerSessionRepos fills ReposScanned from the newest session dir's
// scanned-<worker>.txt (URLs completed on that worker this job).
func attachWorkerSessionRepos(workers []workerStatusRow, sessionDirs []string) {
	if len(workers) == 0 || len(sessionDirs) == 0 {
		return
	}
	// Newest last (lexicographic timestamps).
	dir := sessionDirs[len(sessionDirs)-1]
	for i := range workers {
		name := workers[i].Name
		path := filepath.Join(dir, fmt.Sprintf("scanned-%s.txt", name))
		if n, err := countURLLinesErr(path); err == nil {
			workers[i].ReposScanned = n
			continue
		}
		// Fall back: max across all session dirs.
		best := 0
		for _, d := range sessionDirs {
			p := filepath.Join(d, fmt.Sprintf("scanned-%s.txt", name))
			if n, err := countURLLinesErr(p); err == nil && n > best {
				best = n
			}
		}
		workers[i].ReposScanned = best
	}
}

func diskPctFromCell(disk string) int {
	s := strings.TrimSpace(strings.TrimSuffix(disk, "%"))
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func jobStartStamp(m *jobMeta) string {
	if m == nil {
		return ""
	}
	if strings.HasPrefix(m.ID, "j-") && len(m.ID) >= 17 {
		return m.ID[2:] // YYYYMMDD-HHMMSS
	}
	if t, err := time.Parse(time.RFC3339, m.StartedAt); err == nil {
		return t.Local().Format("20060102-150405")
	}
	return ""
}

func countURLLinesErr(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
	}
	return n, nil
}

func collectJobSessionArtifacts(dataDir, stamp string) (resultDirs []string, pulled, runs int) {
	ents, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, 0, 0
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, "repos-loop-") && strings.HasSuffix(name, ".txt") {
			ts := strings.TrimSuffix(strings.TrimPrefix(name, "repos-loop-"), ".txt")
			if stamp == "" || ts >= stamp {
				n := countURLLines(filepath.Join(dataDir, name))
				pulled += n
			}
			continue
		}
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "remote-results-") {
			ts := strings.TrimPrefix(name, "remote-results-")
			if stamp == "" || ts >= stamp {
				resultDirs = append(resultDirs, filepath.Join(dataDir, name))
				runs++
			}
		}
	}
	sort.Strings(resultDirs)
	return resultDirs, pulled, runs
}

func listRemoteResultDirs(dataDir, minStamp string) []string {
	ents, err := os.ReadDir(dataDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name != "remote-results" && !strings.HasPrefix(name, "remote-results-") {
			continue
		}
		ts := ""
		if strings.HasPrefix(name, "remote-results-") {
			ts = strings.TrimPrefix(name, "remote-results-")
		}
		if minStamp != "" && ts != "" && ts < minStamp {
			continue
		}
		out = append(out, filepath.Join(dataDir, name))
	}
	sort.Strings(out)
	return out
}

func aggregateCSVStats(dirs []string) (findings, verified, uniqueRepos int) {
	repos := map[string]struct{}{}
	for _, dir := range dirs {
		combined := filepath.Join(dir, combinedCSVName)
		var sources []string
		if _, err := os.Stat(combined); err == nil {
			sources = []string{combined}
		} else {
			matches, _ := filepath.Glob(filepath.Join(dir, "results-*.csv"))
			for _, m := range matches {
				base := filepath.Base(m)
				if base == combinedCSVName || base == combinedVerifiedCSVName || strings.HasSuffix(base, "-verified.csv") {
					continue
				}
				sources = append(sources, m)
			}
			sort.Strings(sources)
		}
		dirVerified := 0
		for _, p := range sources {
			n, v, u := scanCSVFindingStats(p)
			findings += n
			dirVerified += v
			for k := range u {
				repos[k] = struct{}{}
			}
		}
		// If findings CSV lacked verified rows, fall back to results-verified.csv.
		if dirVerified == 0 {
			if n, err := countCSVDataRows(filepath.Join(dir, combinedVerifiedCSVName)); err == nil {
				dirVerified = n
			}
		}
		verified += dirVerified
	}
	return findings, verified, len(repos)
}

func scanCSVFindingStats(path string) (findings, verified int, repos map[string]struct{}) {
	repos = map[string]struct{}{}
	rows, header, err := readResultsCSV(path)
	if err != nil {
		return 0, 0, repos
	}
	hasVerified := false
	for _, h := range header {
		if h == "verified" {
			hasVerified = true
			break
		}
	}
	for _, row := range rows {
		findings++
		if u := githubRepoURL(row["repository_url"]); u != "" {
			if k := normalizeRepoURL(u); k != "" {
				repos[k] = struct{}{}
			}
		}
		if hasVerified {
			v := strings.ToLower(strings.TrimSpace(row["verified"]))
			if v == "true" || v == "1" || v == "yes" {
				verified++
			}
		}
	}
	return findings, verified, repos
}

func countUniqueReposInCSV(path string) int {
	_, _, repos := scanCSVFindingStats(path)
	return len(repos)
}

type jobLogProgress struct {
	Phase        string
	Cycle        int
	Repos        int
	Snippet      []string
	ScanDone     int
	ScanTotal    int
	ScanFailed   int
	ScanInFlight int
	ScanFindings int
	ScanVerified int
	ScanETA      string
}

// scanHeartbeatRe matches: [*] 36/818 done, 0 failed, 6 in flight, 97 finding(s) (3 verified), ...
var scanHeartbeatRe = regexp.MustCompile(`(\d+)/(\d+)\s+done,\s+(\d+)\s+failed,\s+(\d+)\s+in flight,\s+(\d+)\s+finding\(s\)\s+\((\d+)\s+verified\)`)

func parseJobLogProgress(logPath string) jobLogProgress {
	out := jobLogProgress{Phase: "unknown"}
	b, err := os.ReadFile(logPath)
	if err != nil {
		return out
	}
	lines := strings.Split(string(b), "\n")
	var nonEmpty []string
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			nonEmpty = append(nonEmpty, ln)
		}
	}
	if len(nonEmpty) > 8 {
		out.Snippet = append([]string(nil), nonEmpty[len(nonEmpty)-8:]...)
	} else {
		out.Snippet = append([]string(nil), nonEmpty...)
	}

	// Aggregate the most recent heartbeat window (last ~30 matching lines) so
	// multi-worker muxed logs sum to fleet progress rather than one worker.
	type hb struct {
		done, total, failed, inflight, findings, verified int
		eta                                               string
	}
	var recent []hb
	for _, ln := range lines {
		if i := strings.Index(ln, "cycle "); i >= 0 {
			rest := ln[i+len("cycle "):]
			var n int
			if _, err := fmt.Sscanf(rest, "%d", &n); err == nil && n > 0 {
				out.Cycle = n
			}
		}
		if strings.Contains(ln, "cluster run ") {
			out.Phase = "scanning"
			var n int
			if _, err := fmt.Sscanf(afterToken(ln, "cluster run "), "%d", &n); err == nil {
				out.Repos = n
			}
		}
		if strings.Contains(ln, " — search ") || strings.Contains(ln, "repos found") || strings.Contains(ln, "code query") {
			if out.Phase != "scanning" {
				out.Phase = "searching"
			}
		}
		if strings.Contains(ln, "sleep ") && strings.Contains(ln, "before next cycle") {
			out.Phase = "sleeping"
		}
		if strings.Contains(ln, "loop stopped") {
			out.Phase = "stopped"
		}
		if idx := strings.Index(ln, "repos found"); idx > 0 {
			chunk := ln[:idx]
			fields := strings.Fields(chunk)
			if len(fields) >= 2 {
				if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
					out.Repos = n
				}
			}
		}
		if strings.Contains(ln, "no new repos") {
			out.Phase = "searching"
		}
		if m := scanHeartbeatRe.FindStringSubmatch(ln); m != nil {
			h := hb{}
			h.done, _ = strconv.Atoi(m[1])
			h.total, _ = strconv.Atoi(m[2])
			h.failed, _ = strconv.Atoi(m[3])
			h.inflight, _ = strconv.Atoi(m[4])
			h.findings, _ = strconv.Atoi(m[5])
			h.verified, _ = strconv.Atoi(m[6])
			if i := strings.Index(ln, "~"); i >= 0 {
				eta := strings.TrimSpace(ln[i+1:])
				if j := strings.IndexAny(eta, " \t"); j > 0 {
					// keep "~1h26m58s left" style — already includes ~
					eta = strings.TrimSpace(ln[i:])
					if k := strings.Index(eta, " left"); k > 0 {
						h.eta = strings.TrimSpace(eta[:k+len(" left")])
					} else {
						h.eta = eta
					}
				} else {
					h.eta = strings.TrimSpace(ln[i:])
				}
			}
			recent = append(recent, h)
			out.Phase = "scanning"
		}
	}
	if len(recent) > 0 {
		// Take the last N heartbeats (one per worker in a progress tick).
		window := 7
		if window > len(recent) {
			window = len(recent)
		}
		slice := recent[len(recent)-window:]
		for _, h := range slice {
			out.ScanDone += h.done
			out.ScanTotal += h.total
			out.ScanFailed += h.failed
			out.ScanInFlight += h.inflight
			out.ScanFindings += h.findings
			out.ScanVerified += h.verified
			if h.eta != "" {
				out.ScanETA = h.eta // last ETA seen
			}
		}
	}
	if out.Cycle == 0 && out.Phase == "unknown" && len(nonEmpty) > 0 {
		out.Phase = "starting"
	}
	return out
}

func afterToken(s, tok string) string {
	i := strings.Index(s, tok)
	if i < 0 {
		return ""
	}
	return s[i+len(tok):]
}

func probeWorkerStatus(c *Cluster, full bool) []workerStatusRow {
	slaves := c.Slaves
	if len(slaves) == 0 {
		return nil
	}
	out := make([]workerStatusRow, len(slaves))
	var wg sync.WaitGroup
	for i, sl := range slaves {
		i, sl := i, sl
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = probeOneWorker(sl, full)
		}()
	}
	wg.Wait()
	return out
}

func probeOneWorker(sl Slave, full bool) workerStatusRow {
	row := workerStatusRow{
		Name:        sl.Name,
		Host:        sl.Host,
		Enabled:     sl.Enabled,
		Workers:     sl.Workers,
		IsGCE:       sl.GCEInstance != "",
		GCEInstance: sl.GCEInstance,
		GCEZone:     sl.GCEZone,
		GCEProject:  sl.GCEProject,
	}
	if !sl.Enabled {
		row.SSH = "skipped"
		return row
	}
	// Always collect CPU/mem/disk/uptime (status table shows them by default).
	script := workerProbeScript(true)
	cmd := sshCmdConnect(sl, script, 10)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		row.SSH = "down"
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 60 {
			msg = msg[:57] + "..."
		}
		row.Err = msg
		return row
	}
	row.SSH = "ok"
	parseWorkerProbeOutput(strings.TrimSpace(stdout.String()), &row, true)
	row.DiskPct = diskPctFromCell(row.Disk)

	if full && sl.GCEInstance != "" {
		mt, st, cerr := gcloudInstanceBrief(sl)
		if cerr == nil {
			row.Machine = mt
			row.GCEStatus = st
		} else {
			row.Err = "gcloud: " + shortErr(cerr)
			if row.Machine == "" {
				eq := nearestGCPMachine(row.CPUs, row.MemTotalMB)
				row.Machine = eq.Type
			}
		}
	} else if full && row.Machine == "" {
		eq := nearestGCPMachine(row.CPUs, row.MemTotalMB)
		row.Machine = eq.Type
	}
	return row
}

// inferMachineFromMem guesses a size label when gcloud describe is unavailable.
func inferMachineFromMem(mem string, cpus int) string {
	// mem is "used/total" in MiB from free -m.
	parts := strings.Split(mem, "/")
	if len(parts) != 2 {
		return ""
	}
	total, err := strconv.Atoi(parts[1])
	if err != nil || total <= 0 {
		return ""
	}
	gb := float64(total) / 1024.0
	label := fmt.Sprintf("~%.0fGB", gb)
	if cpus > 0 {
		label = fmt.Sprintf("%s/%dvCPU", label, cpus)
	}
	return label
}

func workerProbeScript(full bool) string {
	// Pipe-delimited fields; keep portable across Debian/Ubuntu GCE images.
	if !full {
		return `printf '%s|%s\n' "$(cut -d. -f1 /proc/uptime 2>/dev/null || echo 0)" "$(cut -d' ' -f1 /proc/loadavg 2>/dev/null || echo -)"`
	}
	// Instantaneous CPU: short /proc/stat sample (≈200ms).
	return `u=$(cut -d. -f1 /proc/uptime 2>/dev/null || echo 0)
load=$(cut -d' ' -f1-3 /proc/loadavg 2>/dev/null || echo -)
np=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 0)
mem=$(free -m 2>/dev/null | awk '/^Mem:/{printf "%d/%d",$3,$2}')
disk_pct=$(df -P / 2>/dev/null | awk 'NR==2{gsub(/%/,"",$5); print $5}')
disk_gb=$(df -P / 2>/dev/null | awk 'NR==2{gb=int($2/1024/1024); if(gb<1) gb=1; print gb}')
[ -n "$disk_pct" ] || disk_pct="-"
[ -n "$disk_gb" ] || disk_gb="0"
read -r u1 t1 <<EOF
$(awk '/^cpu /{print $2+$4, $2+$4+$5; exit}' /proc/stat)
EOF
sleep 0.2
read -r u2 t2 <<EOF
$(awk '/^cpu /{print $2+$4, $2+$4+$5; exit}' /proc/stat)
EOF
cpu=0
if [ -n "$t1" ] && [ -n "$t2" ] && [ "$t2" -gt "$t1" ]; then
  cpu=$(( 100 * (u2 - u1) / (t2 - t1) ))
fi
# uptime|load|nproc|mem|diskPct|diskGB|cpu
printf '%s|%s|%s|%s|%s|%s|%s\n' "$u" "$load" "$np" "${mem:--}" "$disk_pct" "$disk_gb" "$cpu"`
}

func parseWorkerProbeOutput(line string, row *workerStatusRow, full bool) {
	parts := strings.Split(line, "|")
	if len(parts) >= 1 {
		if sec, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64); err == nil && sec > 0 {
			row.Uptime = formatUptime(time.Duration(sec) * time.Second)
		} else {
			row.Uptime = "-"
		}
	}
	if len(parts) >= 2 {
		row.Load = strings.TrimSpace(parts[1])
	}
	if !full {
		return
	}
	if len(parts) >= 3 {
		row.CPUs, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
	}
	if len(parts) >= 4 {
		row.Mem = strings.TrimSpace(parts[3])
		row.MemTotalMB = memTotalMBFromCell(row.Mem)
	}
	// New format: …|diskPct|diskGB|cpu  (7 fields)
	// Old format: …|diskPct|cpu       (6 fields)
	if len(parts) >= 7 {
		d := strings.TrimSpace(parts[4])
		if d != "" && d != "-" && !strings.HasSuffix(d, "%") {
			d += "%"
		}
		row.Disk = d
		row.DiskPct = diskPctFromCell(d)
		row.DiskTotalGB, _ = strconv.Atoi(strings.TrimSpace(parts[5]))
		c := strings.TrimSpace(parts[6])
		if c != "" && c != "-" {
			row.CPUPct = c + "%"
		}
	} else if len(parts) >= 6 {
		d := strings.TrimSpace(parts[4])
		if d != "" && d != "-" && !strings.HasSuffix(d, "%") {
			d += "%"
		}
		row.Disk = d
		row.DiskPct = diskPctFromCell(d)
		c := strings.TrimSpace(parts[5])
		if c != "" && c != "-" {
			row.CPUPct = c + "%"
		}
	}
}

func gcloudInstanceBrief(sl Slave) (machine, status string, err error) {
	if _, lookErr := exec.LookPath("gcloud"); lookErr != nil {
		return "", "", lookErr
	}
	if sl.GCEInstance == "" || sl.GCEZone == "" {
		return "", "", fmt.Errorf("missing gce_instance/zone")
	}
	args := []string{"compute", "instances", "describe", sl.GCEInstance,
		"--zone", sl.GCEZone,
		"--format=csv[no-heading](machineType.basename(),status)",
	}
	if sl.GCEProject != "" {
		args = append(args, "--project", sl.GCEProject)
	}
	out, err := runGcloudOutput(args...)
	if err != nil {
		return "", "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	line := strings.TrimSpace(string(out))
	fs := strings.Split(line, ",")
	if len(fs) >= 1 {
		machine = strings.TrimSpace(fs[0])
	}
	if len(fs) >= 2 {
		status = strings.TrimSpace(fs[1])
	}
	return machine, status, nil
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 48 {
		return s[:45] + "..."
	}
	return s
}

func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh%dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh%dm%ds", hours, mins, secs)
	case mins > 0:
		return fmt.Sprintf("%dm%ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func printJobStatusReport(w io.Writer, p palette, rep *jobStatusReport) {
	m := rep.Meta
	statusColored := m.Status
	switch m.Status {
	case jobStatusRunning:
		if rep.Alive {
			statusColored = p.bgreen(m.Status)
		} else {
			statusColored = p.byellow("stale")
		}
	case jobStatusStopped:
		statusColored = p.yellow(m.Status)
	case jobStatusExited:
		statusColored = p.dim(m.Status)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s  %s\n", p.bcyan("Job"), p.bold(m.ID))
	fmt.Fprintln(w, p.dim(strings.Repeat("─", 72)))

	printStatusKVTable(w, p, [][2]string{
		{"Status", statusColored},
		{"Running for", p.bold(formatUptime(rep.Uptime))},
		{"Started", m.StartedAt},
		{"PID", fmt.Sprintf("%d  alive=%s", m.PID, boolYN(p, rep.Alive))},
		{"Phase", phaseColor(p, rep.Phase)},
		{"Cycle", strconv.Itoa(rep.Cycle)},
		{"Playbook", m.Playbook},
		{"Prefix", emptyDash(m.PrefixFile)},
		{"Terms", emptyDash(m.TermsFile)},
		{"Mode", fmt.Sprintf("%s  limit=%d  sleep=%ds  search_workers=%d",
			m.Mode, m.Limit, m.SleepSecs, m.Workers)},
		{"Log", m.LogPath},
	})

	fmt.Fprintln(w)
	p.section(w, "This job")
	batchRepos := rep.SessionPulled
	if batchRepos == 0 && rep.LogRepos > 0 {
		batchRepos = rep.LogRepos
	}
	scanProgress := "—"
	if rep.ScanTotal > 0 {
		pct := 0
		if rep.ScanTotal > 0 {
			pct = (100 * rep.ScanDone) / rep.ScanTotal
		}
		scanProgress = fmt.Sprintf("%d/%d done (%d%%)  failed=%d  in_flight=%d",
			rep.ScanDone, rep.ScanTotal, pct, rep.ScanFailed, rep.ScanInFlight)
		if rep.ScanETA != "" {
			scanProgress += "  eta " + rep.ScanETA
		}
	} else if rep.Phase == "searching" {
		scanProgress = "searching for repos…"
	} else if rep.Phase == "sleeping" {
		scanProgress = "between cycles"
	}
	findingsCell := itoaDash(rep.SessionFindings)
	if rep.ScanFindings > rep.SessionFindings {
		findingsCell = fmt.Sprintf("%d  (live heartbeat %d)", rep.SessionFindings, rep.ScanFindings)
	}
	verifiedCell := itoaDash(rep.SessionVerified)
	if rep.ScanVerified > rep.SessionVerified {
		verifiedCell = fmt.Sprintf("%d  (live heartbeat %d)", rep.SessionVerified, rep.ScanVerified)
	}
	printStatusKVTable(w, p, [][2]string{
		{"Cycle", strconv.Itoa(rep.Cycle)},
		{"Repos in batch", itoaDash(batchRepos)},
		{"Scan progress", scanProgress},
		{"Findings", p.bgreen(findingsCell)},
		{"Verified secrets", p.bgreen(verifiedCell)},
		{"Repos with secrets", itoaDash(rep.SessionReposHit)},
		{"Result dirs", itoaDash(rep.SessionRuns)},
	})
	if !rep.Full {
		fmt.Fprintln(w, p.dim("  tip: --full adds lifetime totals, machine type, and cost estimate"))
	}

	if rep.Full {
		fmt.Fprintln(w)
		p.section(w, "Lifetime")
		printStatusKVTable(w, p, [][2]string{
			{"Repos scanned", itoaDash(rep.Scanned)},
			{"Clean repos", itoaDash(rep.Clean)},
			{"Findings", itoaDash(rep.LifeFindings)},
			{"Verified secrets", itoaDash(rep.LifeVerified)},
			{"Repos with secrets", itoaDash(rep.LifeReposHit)},
		})
	}

	if len(rep.Workers) > 0 {
		fmt.Fprintln(w)
		title := "Workers"
		if rep.Full {
			title = "Workers (full)"
		}
		p.section(w, title)
		wh := []string{"Name", "Host", "SSH", "Uptime", "CPU", "Mem", "Disk", "Repos", "workers"}
		if rep.Full {
			wh = append(wh, "Machine")
		}
		var wrows [][]string
		for _, wr := range rep.Workers {
			row := []string{
				wr.Name,
				wr.Host,
				wr.SSH,
				emptyDash(wr.Uptime),
				emptyDash(wr.CPUPct),
				formatMemCell(wr.Mem),
				formatDiskCell(wr.Disk, wr.DiskTotalGB),
				formatReposCell(wr.ReposScanned, rep.Uptime),
				strconv.Itoa(wr.Workers),
			}
			if rep.Full {
				row = append(row, machineCell(wr))
			}
			wrows = append(wrows, row)
		}
		printColorTable(w, p, wh, wrows, map[int]func(string) string{
			2: sshColor(p),
			6: func(s string) string {
				pct := diskPctFromCell(s)
				if pct >= 85 {
					return p.bred(s)
				}
				if pct >= 70 {
					return p.byellow(s)
				}
				return s
			},
		})
		if note := workerStatusNotes(rep.Workers); note != "" {
			fmt.Fprintln(w, p.dim("  note: "+note))
		}
		fmt.Fprintln(w, p.dim("  Repos = scanned this job · Mem = used/total MiB · workers = concurrent clones"))
	}

	if rep.Full && rep.CostEstimate != nil {
		printFleetCostEstimate(w, p, *rep.CostEstimate)
	}

	printJobActionCommands(w, p, rep)
	fmt.Fprintln(w)
}

func printJobActionCommands(w io.Writer, p palette, rep *jobStatusReport) {
	m := rep.Meta
	if m == nil {
		return
	}
	fmt.Fprintln(w)
	playbook := m.Playbook
	if playbook == "" {
		playbook = "remote-playbook.yaml"
	}
	if m.LogPath != "" {
		cmd := fmt.Sprintf("./truffles job logs %s -n 50 -f", m.ID)
		fmt.Fprintf(w, "  %s  %s\n", p.byellow("View Recent Logs"), p.cyan(cmd))
	}

	highDisk := false
	critDisk := false
	var critNames []string
	for _, wr := range rep.Workers {
		if wr.DiskPct >= 70 {
			highDisk = true
		}
		if wr.DiskPct >= 85 {
			critDisk = true
			critNames = append(critNames, wr.Name)
		}
	}
	if highDisk {
		clean := fmt.Sprintf("./truffles cluster clean-disk -c %s --orphans-only", playbook)
		fmt.Fprintf(w, "  %s  %s\n", p.byellow("Clean Disk"), p.cyan(clean))
	}
	if critDisk || highDisk {
		// Prefer grow for the worst offender; show one copy-pasteable gcloud line when GCE.
		var target *workerStatusRow
		for i := range rep.Workers {
			wr := &rep.Workers[i]
			if wr.DiskPct >= 70 && wr.GCEInstance != "" {
				if target == nil || wr.DiskPct > target.DiskPct {
					target = wr
				}
			}
		}
		if target != nil {
			newSize := target.DiskTotalGB * 2
			if newSize < 100 {
				newSize = 100
			}
			if target.DiskTotalGB >= newSize {
				newSize = target.DiskTotalGB + 50
			}
			proj := ""
			if target.GCEProject != "" {
				proj = " --project " + target.GCEProject
			}
			grow := fmt.Sprintf("gcloud compute disks resize %s --zone %s --size %dGB%s",
				target.GCEInstance, target.GCEZone, newSize, proj)
			label := "Grow Disk"
			if len(critNames) > 0 {
				label = fmt.Sprintf("Grow Disk (%s %d%%)", target.Name, target.DiskPct)
			} else {
				label = fmt.Sprintf("Grow Disk (%s %d%%)", target.Name, target.DiskPct)
			}
			fmt.Fprintf(w, "  %s  %s\n", p.byellow(label), p.cyan(grow))
			fmt.Fprintln(w, p.dim("  after resize: ssh in and run  sudo growpart /dev/sda 1 && sudo resize2fs /dev/sda1"))
		} else if highDisk {
			fmt.Fprintln(w, p.dim("  disk high but no gce_instance — free space with Clean Disk, or enlarge the volume in your provider UI"))
		}
	}
}

func workerStatusNotes(workers []workerStatusRow) string {
	var gcloudFail int
	var other []string
	for _, wr := range workers {
		if wr.Err == "" {
			continue
		}
		if strings.HasPrefix(wr.Err, "gcloud:") {
			gcloudFail++
			continue
		}
		other = append(other, wr.Name+": "+wr.Err)
	}
	var parts []string
	if gcloudFail > 0 {
		parts = append(parts, fmt.Sprintf("gcloud describe unavailable on %d worker(s) — showing size from free -m", gcloudFail))
	}
	parts = append(parts, other...)
	return strings.Join(parts, "; ")
}

func machineCell(wr workerStatusRow) string {
	if wr.Machine == "" {
		return "-"
	}
	if wr.GCEStatus != "" && !strings.EqualFold(wr.GCEStatus, "RUNNING") {
		return wr.Machine + " (" + wr.GCEStatus + ")"
	}
	return wr.Machine
}

// formatMemCell turns "260/7939" into "260/7939M" for the status table.
func formatMemCell(mem string) string {
	mem = strings.TrimSpace(mem)
	if mem == "" || mem == "-" {
		return "-"
	}
	if strings.HasSuffix(mem, "M") || strings.HasSuffix(mem, "G") {
		return mem
	}
	return mem + "M"
}

// formatDiskCell turns "96%" + 50 into "96% (50GB)".
func formatDiskCell(disk string, totalGB int) string {
	disk = emptyDash(strings.TrimSpace(disk))
	if disk == "-" {
		return "-"
	}
	if totalGB > 0 {
		return fmt.Sprintf("%s (%dGB)", disk, totalGB)
	}
	return disk
}

// formatReposCell turns 2862 + job uptime into "2862 (1572/hr)".
func formatReposCell(repos int, jobUptime time.Duration) string {
	if repos <= 0 {
		return "0"
	}
	hours := jobUptime.Hours()
	if hours < 1.0/60.0 { // < 1 minute — rate not meaningful yet
		return strconv.Itoa(repos)
	}
	rate := float64(repos) / hours
	if rate >= 100 {
		return fmt.Sprintf("%d (%.0f/hr)", repos, rate)
	}
	if rate >= 10 {
		return fmt.Sprintf("%d (%.1f/hr)", repos, rate)
	}
	return fmt.Sprintf("%d (%.2f/hr)", repos, rate)
}

func sshColor(p palette) func(string) string {
	return func(s string) string {
		switch s {
		case "ok":
			return p.bgreen(s)
		case "down":
			return p.bred(s)
		case "skipped":
			return p.dim(s)
		default:
			return s
		}
	}
}

func phaseColor(p palette, phase string) string {
	switch phase {
	case "searching":
		return p.cyan(phase)
	case "scanning":
		return p.bgreen(phase)
	case "sleeping":
		return p.dim(phase)
	case "stopped":
		return p.yellow(phase)
	default:
		return phase
	}
}

func boolYN(p palette, v bool) string {
	if v {
		return p.bgreen("true")
	}
	return p.bred("false")
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func itoaDash(n int) string {
	return strconv.Itoa(n)
}

func trimLogLine(s string, max int) string {
	s = strings.TrimRight(s, "\r\n")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

func printStatusKVTable(w io.Writer, p palette, kvs [][2]string) {
	labelW := 0
	for _, kv := range kvs {
		if n := utf8.RuneCountInString(kv[0]); n > labelW {
			labelW = n
		}
	}
	for _, kv := range kvs {
		fmt.Fprintf(w, "  %s  %s\n", p.dim(padRight(kv[0], labelW)), kv[1])
	}
}

// printColorTable prints an aligned ASCII table with optional per-column colorizers.
// Colorizers receive the cell text and must return a string of equal visible width
// (ANSI codes only) or the plain text.
func printColorTable(w io.Writer, p palette, headers []string, rows [][]string, color map[int]func(string) string) {
	cols := len(headers)
	widths := make([]int, cols)
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i := 0; i < cols && i < len(row); i++ {
			if n := utf8.RuneCountInString(row[i]); n > widths[i] {
				widths[i] = n
			}
		}
	}
	// Header
	var b strings.Builder
	b.WriteString("  ")
	for i, h := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(padRight(h, widths[i]))
	}
	fmt.Fprintln(w, p.bold(b.String()))
	// Rule
	b.Reset()
	b.WriteString("  ")
	for i := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(strings.Repeat("─", widths[i]))
	}
	fmt.Fprintln(w, p.dim(b.String()))
	// Rows
	for _, row := range rows {
		b.Reset()
		b.WriteString("  ")
		for i := 0; i < cols; i++ {
			if i > 0 {
				b.WriteString("  ")
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			padded := padRight(cell, widths[i])
			if color != nil {
				if fn, ok := color[i]; ok && fn != nil {
					// Color only the content, keep trailing spaces plain.
					content := cell
					pad := widths[i] - utf8.RuneCountInString(content)
					if pad < 0 {
						pad = 0
					}
					b.WriteString(fn(content))
					b.WriteString(strings.Repeat(" ", pad))
					continue
				}
			}
			b.WriteString(padded)
		}
		fmt.Fprintln(w, b.String())
	}
}
