package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func runCluster(rest []string) error {
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		fmt.Print(clusterUsage())
		return nil
	}
	cmd := rest[0]
	r := rest[1:]
	switch cmd {
	case "init":
		return clusterInit(r)
	case "gen", "generate":
		return clusterInit(r)
	case "distribute", "dist", "split":
		return clusterDistribute(r)
	case "collect":
		return clusterCollect(r)
	case "run":
		return clusterRun(r)
	case "status":
		return clusterStatus(r)
	case "stop":
		return clusterStop(r)
	case "reset":
		return clusterReset(r)
	case "deploy":
		return clusterDeploy(r)
	case "heal", "recover":
		return clusterHeal(r)
	case "clean-disk", "disk-clean", "disk":
		return clusterCleanDisk(r)
	case "scale", "resize":
		return clusterScale(r)
	default:
		fmt.Print(clusterUsage())
		return fmt.Errorf("unknown cluster command %q", cmd)
	}
}

func clusterUsage() string {
	return `truffles cluster - distributed scanning across multiple machines via SSH

usage:
  truffles cluster init [-o cluster.yaml]        create cluster config
  truffles cluster run -c playbook.yaml [-f repos.txt] [-w N] [-format csv]
  truffles cluster distribute -c cluster.yaml -f repos.txt
  truffles cluster collect -c cluster.yaml -out ./results
  truffles cluster status -c cluster.yaml
  truffles cluster stop -c cluster.yaml          kill remote scans (+ local playbook)
  truffles cluster reset -c cluster.yaml         gcloud compute instances reset
  truffles cluster deploy -c cluster.yaml        cross-compile + scp binary to workers
  truffles cluster heal -c cluster.yaml          stop → reset → wait SSH → deploy
  truffles cluster clean-disk -c playbook.yaml   purge orphan clones + workdir artifacts
  truffles cluster clean-disk -c … --dry-run     show what would be deleted
  truffles cluster clean-disk -c … --orphans-only  only /tmp/truffles-since-*
  truffles cluster scale -c playbook.yaml --workers 4
  truffles cluster scale -c … --worker worker3 --machine e2-standard-2 --workers 4

  After collect, workers auto-purge copied artifacts. Scans stall when disk ≥ 90%.
  scale: bump YAML concurrency (next cycle) and/or GCE machine type (stops that VM).

  Omit -f with a playbook YAML: runs search from the YAML into
  <data_dir>/<scan_name>/repos.txt, then scans and collects into that same dir.
  Interactive TTY prompts ask for data_dir (default data) and scan name
  (default remote-results); use -y to skip prompts. After collect, worker
  CSVs are merged into results.csv; verified=true rows also → results-verified.csv.

  Playbook/cluster run auto-heals when auto_heal is true (default if gce_instance
  is set): unreachable SSH or mid-run transport errors trigger reset+deploy once.
  cluster run scans all slaves in parallel (repo list is split round-robin).

  Playbook YAML with slaves works as -c too:
  truffles cluster heal -c remote-playbook.yaml

Cluster YAML example:
  name: mycluster
  master: alice@master
  slaves:
    - name: node1
      host: 10.0.0.10
      user: alice
      enabled: true
      workdir: /home/alice/truffles-work
      truffles_path: /usr/local/bin/truffles
      gce_instance: truffles-worker
      gce_zone: us-central1-a
      gce_project: sandbox420
`
}

func clusterInit(rest []string) error {
	fs := flag.NewFlagSet("cluster init", flag.ContinueOnError)
	out := fs.String("o", "cluster.yaml", "output cluster YAML")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c := genCluster("")
	if err := writeCluster(c, *out); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}

func clusterDistribute(rest []string) error {
	fs := flag.NewFlagSet("cluster distribute", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster config")
	f := fs.String("f", "repos.txt", "repos list to split")
	chunks := fs.Int("chunks", 0, "number of chunks (default: number of enabled slaves)")
	strat := fs.String("split", "roundrobin", "split strategy: roundrobin|shuffle|owner|hash")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	enabled := enabledSlaves(c)
	n := *chunks
	if n <= 0 {
		n = len(enabled)
	}
	if n <= 0 {
		return fmt.Errorf("no enabled slaves")
	}
	var splits [][]string
	switch strings.ToLower(*strat) {
	case "owner":
		urls := readURLsList(*f)
		splits = splitByOwner(urls, n)
	case "hash":
		sh, err := splitLinesHash(*f, n)
		if err != nil {
			return err
		}
		splits = sh
	case "shuffle":
		sh, err := splitLinesShuffled(*f, n)
		if err != nil {
			return err
		}
		splits = sh
	default:
		sh, err := splitLines(*f, n)
		if err != nil {
			return err
		}
		splits = sh
	}
	tmpdir, _ := os.MkdirTemp("", "truffles-dist-")
	defer os.RemoveAll(tmpdir)

	for i, sl := range enabled {
		if i >= len(splits) {
			continue
		}
		chunkPath := filepath.Join(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name))
		if _, err := writeLines(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name), splits[i]); err != nil {
			return err
		}
		_ = chunkPath
		wd := slaveWorkDir(sl)
		if err := ensureRemoteWorkDir(sl); err != nil {
			fmt.Fprintf(os.Stderr, "workdir %s: %v\n", sl.Name, err)
			continue
		}
		remote := filepath.Join(wd, "repos.txt")
		if err := runScpTo(sl, filepath.Join(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name)), remote); err != nil {
			fmt.Fprintf(os.Stderr, "scp to %s failed: %v\n", sl.Name, err)
		}
	}
	fmt.Println("distributed")
	return nil
}

func binOrPath(p string) string {
	if p == "" {
		return "truffles"
	}
	return p
}

func enabledSlaves(c *Cluster) []Slave {
	enabled := []Slave{}
	for _, s := range c.Slaves {
		if !s.Enabled {
			continue
		}
		enabled = append(enabled, s)
	}
	if len(enabled) == 0 {
		return c.Slaves
	}
	return enabled
}

func clusterRun(rest []string) error {
	fs := flag.NewFlagSet("cluster run", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster or playbook YAML")
	f := fs.String("f", "", "repos list (omit to run search from playbook YAML into <data_dir>/<scan>/repos.txt)")
	workers := fs.Int("w", 0, "workers per slave (default: scan.workers from YAML, or 4)")
	format := fs.String("format", "", "report format (default: scan.format from YAML, or csv)")
	dataDir := fs.String("data-dir", "", "parent output directory (default: data_dir from YAML, or data)")
	out := fs.String("out", "", "scan name under data-dir (default: collect_out from YAML, or remote-results)")
	noCollect := fs.Bool("no-collect", false, "leave reports on workers only (skip scp home)")
	yes := fs.Bool("y", false, "non-interactive (skip data-dir / scan-name prompts)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	// Playbook YAML may carry search/scan/collect_out — load when present.
	pb, _ := loadPlaybook(*cfg)

	yamlData := c.DataDir
	yamlOut := c.CollectOut
	if pb != nil {
		if pb.DataDir != "" {
			yamlData = pb.DataDir
		}
		if pb.CollectOut != "" {
			yamlOut = pb.CollectOut
		}
	}
	lay, err := resolveRunLayout(runLayoutOpts{
		DataDirFlag: *dataDir,
		OutFlag:     *out,
		YAMLDataDir: yamlData,
		YAMLOut:     yamlOut,
		Prompt:      !*yes,
	})
	if err != nil {
		return err
	}
	collectDir := lay.CollectDir
	fmt.Printf("[*] run dir: %s\n", collectDir)

	runStart := time.Now()
	var (
		searchRan bool
		searchDur time.Duration
		scanDur   time.Duration
	)
	playbookName := c.Name
	if pb != nil && pb.Name != "" {
		playbookName = pb.Name
	}
	runPerfBegin(collectDir, PerformanceMetadata{
		Command:    "cluster run",
		Playbook:   playbookName,
		CollectDir: collectDir,
	})

	sc := ScanConfig{}
	if pb != nil {
		sc = pb.Scan
	}

	reposFile := *f
	switch {
	case reposFile != "":
		st, err := os.Stat(reposFile)
		if err != nil {
			return fmt.Errorf("open %s: %w", reposFile, err)
		}
		if st.IsDir() {
			return fmt.Errorf("open %s: is a directory", reposFile)
		}
	case pb != nil && playbookHasSearch(pb):
		reposFile = filepath.Join(collectDir, reposListBasename(pb))
		pb.Search.Out = reposFile
		fmt.Printf("[*] generating repo list via playbook search → %s\n", reposFile)
		searchStart := time.Now()
		if err := runSearch(buildSearchArgs(*pb)); err != nil {
			return err
		}
		searchRan = true
		searchDur = time.Since(searchStart)
	case pb != nil && firstExisting(pb.Scan.File) != "":
		reposFile = firstExisting(pb.Scan.File)
		fmt.Printf("[*] using existing repo list %s\n", reposFile)
	default:
		return fmt.Errorf("no repo list: pass -f <file>, or add search.owner/queries/prefix_file to %s", *cfg)
	}

	w := *workers
	if w <= 0 {
		w = sc.Workers
	}
	if w <= 0 {
		w = 4
	}
	fmtStr := *format
	if fmtStr == "" {
		fmtStr = sc.Format
	}
	if fmtStr == "" {
		fmtStr = "csv"
	}

	repoCount := countURLLines(reposFile)
	scanStart := time.Now()
	if err := clusterRunOn(c, reposFile, w, fmtStr, sc, collectDir); err != nil {
		return err
	}
	scanDur = time.Since(scanStart)
	perfRecordPhaseChunk("scan", "cluster", fmt.Sprintf("%d slaves", len(enabledSlaves(c))), true, scanDur, map[string]interface{}{
		"repos": repoCount, "workers_per_slave": w,
	})
	scanMeta := &ScanPerformanceMeta{Repos: repoCount}
	if *noCollect {
		if err := finalizePlaybookRun(collectDir, playbookName, "cluster run", runStart, searchRan, searchDur, scanDur, 0,
			nil, scanMeta, nil, nil); err != nil {
			return err
		}
		fmt.Printf("cluster run done (reports left on workers; repos: %s)\n", reposFile)
		return nil
	}
	collectStart := time.Now()
	collectFiles := 0
	if err := clusterCollectOnWith(c, collectDir, sc, &collectFiles); err != nil {
		return fmt.Errorf("run ok but collect failed: %w", err)
	}
	collectDur := time.Since(collectStart)
	if err := finalizePlaybookRun(collectDir, playbookName, "cluster run", runStart, searchRan, searchDur, scanDur, collectDur,
		nil, scanMeta, &CollectPerformanceMeta{FilesCopied: collectFiles}, nil); err != nil {
		return err
	}
	fmt.Printf("cluster run done. results: %s (repos: %s)\n", collectDir, reposFile)
	return nil
}

// clusterRunOn distributes repos and runs scan on each enabled slave.
// When auto_heal is enabled (default if gce_instance is set), unreachable
// workers are reset/redeployed and a transport failure retries once.
func clusterRunOn(c *Cluster, reposFile string, workers int, format string, sc ScanConfig, collectDir string) error {
	if err := clusterEnsureReady(c); err != nil {
		return err
	}
	err := clusterRunOnOnce(c, reposFile, workers, format, sc, collectDir)
	if err == nil || !clusterAutoHealEnabled(c) || !clusterShouldAutoRecover(err) {
		return err
	}
	fmt.Printf("[!] remote run failed (%v) — recovering and retrying once\n", err)
	if isStaleBinaryErr(err) && !isSSHTransportErr(err) {
		if herr := clusterDeployOn(c, ""); herr != nil {
			return fmt.Errorf("run failed: %v; deploy failed: %w", err, herr)
		}
	} else if herr := clusterHealOn(c, healOpts{StopRemote: true}); herr != nil {
		return fmt.Errorf("run failed: %v; heal failed: %w", err, herr)
	}
	return clusterRunOnOnce(c, reposFile, workers, format, sc, collectDir)
}

func clusterRunOnOnce(c *Cluster, reposFile string, workers int, format string, sc ScanConfig, collectDir string) error {
	enabled := enabledSlaves(c)
	if len(enabled) == 0 {
		return fmt.Errorf("no slaves configured")
	}
	if workers <= 0 {
		workers = 4
	}
	if format == "" {
		format = "csv"
	}
	ext := format
	if format == "pretty" {
		ext = "txt"
	}
	weights := make([]int, len(enabled))
	for i, sl := range enabled {
		weights[i] = slaveWorkers(sl, workers)
	}
	splits, err := splitLinesShuffledWeighted(reposFile, weights)
	if err != nil {
		return err
	}
	tmpdir, err := os.MkdirTemp("", "truffles-crun-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	// Stage repo chunks (and optional skip list) onto every slave first, then
	// run scans concurrently — sequential SSH was leaving N-1 workers idle.
	type staged struct {
		sl      Slave
		wd      string
		useSkip bool
		workers int
		repos   int
	}
	skipPath := filepath.Join(tmpdir, "skip-repos.txt")
	useSkipMaster := false
	if sc.SkipFile != "" || sc.CleanFile != "" {
		if err := writeCombinedSkipFile(skipPath, sc.SkipFile, sc.CleanFile); err != nil {
			return fmt.Errorf("build skip list: %w", err)
		}
		if st, err := os.Stat(skipPath); err == nil && !st.IsDir() && st.Size() > 0 {
			useSkipMaster = true
		} else if sc.SkipFile != "" {
			if _, err := os.Stat(sc.SkipFile); err != nil && os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "[*] skip-file %s not found yet — scanning all repos\n", sc.SkipFile)
			}
		}
	}

	var stagedSlaves []staged
	for i, sl := range enabled {
		if i >= len(splits) {
			continue
		}
		// Stall this worker (purge orphans, wait) when root disk is ≥ 90%.
		if err := ensureWorkerDiskSpace(sl, defaultDiskStallPct, 10*time.Minute); err != nil {
			fmt.Fprintf(os.Stderr, "[!] %v — skipping worker this cycle\n", err)
			continue
		}
		wd := slaveWorkDir(sl)
		if err := ensureRemoteWorkDir(sl); err != nil {
			fmt.Fprintf(os.Stderr, "workdir %s: %v\n", sl.Name, err)
			if isSSHTransportErr(err) {
				return fmt.Errorf("workdir %s: %w", sl.Name, err)
			}
			continue
		}
		if _, err := writeLines(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name), splits[i]); err != nil {
			return err
		}
		chunkPath := filepath.Join(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name))
		if err := runScpTo(sl, chunkPath, filepath.Join(wd, "repos.txt")); err != nil {
			fmt.Fprintf(os.Stderr, "scp to %s failed: %v\n", sl.Name, err)
			if isSSHTransportErr(err) {
				return fmt.Errorf("scp %s: %w", sl.Name, err)
			}
			continue
		}
		useSkip := false
		if useSkipMaster {
			if err := runScpTo(sl, skipPath, filepath.Join(wd, "skip-repos.txt")); err != nil {
				fmt.Fprintf(os.Stderr, "scp skip-file to %s failed: %v\n", sl.Name, err)
				if isSSHTransportErr(err) {
					return fmt.Errorf("scp skip-file %s: %w", sl.Name, err)
				}
			} else {
				useSkip = true
			}
		}
		stagedSlaves = append(stagedSlaves, staged{
			sl: sl, wd: wd, useSkip: useSkip,
			workers: weights[i], repos: len(splits[i]),
		})
	}
	if len(stagedSlaves) == 0 {
		return fmt.Errorf("no slaves received a repo chunk")
	}

	fmt.Printf("[*] scanning on %d slave(s) in parallel (capacity-weighted split)\n", len(stagedSlaves))
	for _, st := range stagedSlaves {
		fmt.Printf("[*]   %s: %d repos, %d workers\n", st.sl.Name, st.repos, st.workers)
	}

	if collectDir != "" {
		if err := os.MkdirAll(collectDir, 0755); err != nil {
			return err
		}
		fmt.Printf("[*] mirroring worker scan → %s (tail -f scan-*.log; results-*.csv refresh during scan)\n", collectDir)
	}

	mirrorWorkers := make([]workerStage, len(stagedSlaves))
	for i, st := range stagedSlaves {
		mirrorWorkers[i] = workerStage{sl: st.sl, wd: st.wd}
	}
	mirrorCtx, stopMirror := context.WithCancel(context.Background())
	var mirrorWg sync.WaitGroup
	if collectDir != "" {
		mirrorWg.Add(1)
		go func() {
			defer mirrorWg.Done()
			mirrorWorkerResultsDuringRun(mirrorCtx, mirrorWorkers, collectDir, sc)
		}()
	}

	var interrupted atomic.Bool
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigc
		if !interrupted.CompareAndSwap(false, true) {
			return
		}
		fmt.Fprintln(os.Stderr, "[!] interrupted — syncing worker files then exiting…")
		stopMirror()
		// One last pull so Ctrl-C still leaves a usable local snapshot.
		for _, w := range mirrorWorkers {
			pullRemoteWorkerFiles(w.sl, w.wd, collectDir)
		}
		printInterruptedLocalPaths(os.Stderr, collectDir)
		os.Exit(130)
	}()
	defer signal.Stop(sigc)

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		runErr   error
		fatalErr error
	)
	for _, st := range stagedSlaves {
		wg.Add(1)
		go func(st staged) {
			defer wg.Done()
			sl := st.sl
			scanCmd := remoteScanCommand(st.wd, sl, st.workers, format, ext, sc, st.useSkip)
			run := sshCmd(sl, scanCmd)
			var errBuf bytes.Buffer
			run.Stdout = os.Stdout
			stderrOut := []io.Writer{os.Stderr, &errBuf}
			if collectDir != "" {
				logPath := workerScanLogPath(collectDir, sl.Name)
				logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					fmt.Fprintf(os.Stderr, "scan log %s: %v\n", sl.Name, err)
				} else {
					defer logf.Close()
					stderrOut = append(stderrOut, logf)
				}
			}
			run.Stderr = io.MultiWriter(stderrOut...)
			if err := run.Run(); err != nil {
				if interrupted.Load() {
					return
				}
				wrapped := fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
				fmt.Fprintf(os.Stderr, "slave %s failed: %v\n", sl.Name, err)
				errMu.Lock()
				defer errMu.Unlock()
				if isSSHTransportErr(err) || isSSHTransportErr(wrapped) || isStaleBinaryErr(wrapped) {
					if fatalErr == nil {
						fatalErr = fmt.Errorf("ssh %s: %w", sl.Name, wrapped)
					}
					return
				}
				runErr = wrapped
			}
		}(st)
	}
	wg.Wait()
	stopMirror()
	mirrorWg.Wait()
	if interrupted.Load() {
		printInterruptedLocalPaths(os.Stderr, collectDir)
		os.Exit(130)
	}
	if fatalErr != nil {
		return fatalErr
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

// scanNoProxyFlag returns the CLI flag for scan's proxy mode. nil/true → direct
// (default); false → use the proxy pool.
func scanNoProxyFlag(noProxy *bool) string {
	if noProxy == nil || *noProxy {
		return "-no-proxy"
	}
	return "-no-proxy=false"
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func clusterCollect(rest []string) error {
	fs := flag.NewFlagSet("cluster collect", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster config")
	dataDir := fs.String("data-dir", "", "parent output directory (default: data)")
	outDir := fs.String("out", "", "scan name under data-dir (default: collect_out / remote-results)")
	yes := fs.Bool("y", false, "non-interactive (skip prompts)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	pb, _ := loadPlaybook(*cfg)
	yamlData, yamlOut := c.DataDir, c.CollectOut
	if pb != nil {
		if pb.DataDir != "" {
			yamlData = pb.DataDir
		}
		if pb.CollectOut != "" {
			yamlOut = pb.CollectOut
		}
	}
	lay, err := resolveRunLayout(runLayoutOpts{
		DataDirFlag: *dataDir,
		OutFlag:     *outDir,
		YAMLDataDir: yamlData,
		YAMLOut:     yamlOut,
		Prompt:      !*yes,
	})
	if err != nil {
		return err
	}
	return clusterCollectOn(c, lay.CollectDir)
}

func clusterCollectOn(c *Cluster, outDir string) error {
	return clusterCollectOnWith(c, outDir, ScanConfig{}, nil)
}

func clusterCollectOnWith(c *Cluster, outDir string, sc ScanConfig, filesCopied *int) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	enabled := enabledSlaves(c)
	var deltas []string
	for _, sl := range enabled {
		wd := slaveWorkDir(sl)
		for _, name := range remoteWorkerArtifactNames(sl) {
			local := filepath.Join(outDir, name)
			remote := filepath.Join(wd, name)
			t0 := time.Now()
			err := scpFrom(sl, remote, local).Run()
			ok := err == nil
			if ok {
				if filesCopied != nil {
					*filesCopied++
				}
				if strings.HasPrefix(name, "scanned-") {
					deltas = append(deltas, local)
				}
			}
			perfRecordCollectFile(sl.Name, name, ok, time.Since(t0))
			if !ok {
				continue
			}
		}
	}
	if sc.AppendScanned != "" && len(deltas) > 0 {
		n, err := mergeScannedFiles(sc.AppendScanned, deltas)
		if err != nil {
			return fmt.Errorf("merge append_scanned: %w", err)
		}
		fmt.Printf("merged %d new URL(s) into %s\n", n, sc.AppendScanned)
	}
	var cleanDeltas []string
	for _, sl := range enabled {
		local := filepath.Join(outDir, fmt.Sprintf("clean-%s.json", sl.Name))
		if st, err := os.Stat(local); err == nil && !st.IsDir() {
			cleanDeltas = append(cleanDeltas, local)
		}
	}
	if sc.CleanFile != "" && len(cleanDeltas) > 0 {
		n, err := mergeCleanFiles(sc.CleanFile, cleanDeltas)
		if err != nil {
			return fmt.Errorf("merge clean_file: %w", err)
		}
		fmt.Printf("merged %d clean repo(s) into %s\n", n, sc.CleanFile)
	}
	if combined, rows, err := combineResultCSVs(outDir); err != nil {
		return fmt.Errorf("combine csv: %w", err)
	} else if combined != "" {
		fmt.Printf("combined %d row(s) → %s (per-worker CSVs kept)\n", rows, combined)
		verPath := filepath.Join(outDir, combinedVerifiedCSVName)
		if st, err := os.Stat(verPath); err == nil && !st.IsDir() {
			if n, err := countCSVDataRows(verPath); err == nil {
				fmt.Printf("verified spreadsheet %d row(s) → %s\n", n, verPath)
			} else {
				fmt.Printf("verified spreadsheet → %s\n", verPath)
			}
		}
	}
	fmt.Printf("collected to %s\n", outDir)
	CoreLog("cluster-collect", fmt.Sprintf("out=%s", absPathForLog(outDir)))
	// Free worker disk now that artifacts are local.
	purgeWorkerArtifactsAfterCollect(c)
	return nil
}

func clusterStatus(rest []string) error {
	fs := flag.NewFlagSet("cluster status", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster config")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	fmt.Printf("cluster: %s (master %s)\n", c.Name, c.Master)
	for _, s := range c.Slaves {
		enabled := "yes"
		if s.Enabled == false {
			enabled = "no"
		}
		fmt.Printf("  %s\t%s@%s\tenabled=%s\n", s.Name, s.User, s.Host, enabled)
	}
	return nil
}
