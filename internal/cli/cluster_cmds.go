package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

  Omit -f with a playbook YAML: runs search from the YAML into
  <data_dir>/<scan_name>/repos.txt, then scans and collects into that same dir.
  Interactive TTY prompts ask for data_dir (default data) and scan name
  (default remote-results); use -y to skip prompts. After collect, worker
  CSVs are merged into results.csv (per-worker files kept).

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
	strat := fs.String("split", "roundrobin", "split strategy: roundrobin|owner|hash")
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
		if err := runSearch(buildSearchArgs(*pb)); err != nil {
			return err
		}
	case pb != nil && firstExisting(pb.Scan.File) != "":
		reposFile = firstExisting(pb.Scan.File)
		fmt.Printf("[*] using existing repo list %s\n", reposFile)
	default:
		return fmt.Errorf("no repo list: pass -f <file>, or add search.owner/queries to %s", *cfg)
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

	if err := clusterRunOn(c, reposFile, w, fmtStr, sc); err != nil {
		return err
	}
	if *noCollect {
		fmt.Printf("cluster run done (reports left on workers; repos: %s)\n", reposFile)
		return nil
	}
	if err := clusterCollectOnWith(c, collectDir, sc); err != nil {
		return fmt.Errorf("run ok but collect failed: %w", err)
	}
	fmt.Printf("cluster run done. results: %s (repos: %s)\n", collectDir, reposFile)
	return nil
}

// clusterRunOn distributes repos and runs scan on each enabled slave.
// When auto_heal is enabled (default if gce_instance is set), unreachable
// workers are reset/redeployed and a transport failure retries once.
func clusterRunOn(c *Cluster, reposFile string, workers int, format string, sc ScanConfig) error {
	if err := clusterEnsureReady(c); err != nil {
		return err
	}
	err := clusterRunOnOnce(c, reposFile, workers, format, sc)
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
	return clusterRunOnOnce(c, reposFile, workers, format, sc)
}

func clusterRunOnOnce(c *Cluster, reposFile string, workers int, format string, sc ScanConfig) error {
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
	splits, err := splitLines(reposFile, len(enabled))
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
	}
	var stagedSlaves []staged
	for i, sl := range enabled {
		if i >= len(splits) {
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
		if sc.SkipFile != "" {
			if st, err := os.Stat(sc.SkipFile); err == nil && !st.IsDir() {
				if err := runScpTo(sl, sc.SkipFile, filepath.Join(wd, "skip-repos.txt")); err != nil {
					fmt.Fprintf(os.Stderr, "scp skip-file to %s failed: %v\n", sl.Name, err)
					if isSSHTransportErr(err) {
						return fmt.Errorf("scp skip-file %s: %w", sl.Name, err)
					}
				} else {
					useSkip = true
				}
			} else if err != nil && os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "[*] skip-file %s not found yet — scanning all repos\n", sc.SkipFile)
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "[!] skip-file %s: %v\n", sc.SkipFile, err)
			}
		}
		stagedSlaves = append(stagedSlaves, staged{sl: sl, wd: wd, useSkip: useSkip})
	}
	if len(stagedSlaves) == 0 {
		return fmt.Errorf("no slaves received a repo chunk")
	}

	fmt.Printf("[*] scanning on %d slave(s) in parallel (%d workers each)\n", len(stagedSlaves), workers)

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
			scanCmd := fmt.Sprintf("cd %s && %s scan -f repos.txt -workers %d -format %s -out results-%s.%s",
				st.wd, binOrPath(sl.TrufflesPath), workers, format, sl.Name, ext)
			scanCmd += " " + scanNoProxyFlag(sc.NoProxy)
			if sc.PoolSize > 0 {
				scanCmd += fmt.Sprintf(" -pool-size %d", sc.PoolSize)
			}
			if sc.PoolWait != "" {
				scanCmd += " -pool-wait " + sc.PoolWait
			}
			if sc.UseDirect != nil && !*sc.UseDirect {
				scanCmd += " -no-direct"
			}
			if len(sc.ExcludePaths) > 0 {
				scanCmd += " -exclude-paths " + shellSingleQuote(strings.Join(sc.ExcludePaths, ","))
			}
			if sc.NoVerification {
				scanCmd += " -no-verification"
			}
			if sc.MaxDepth > 0 {
				scanCmd += fmt.Sprintf(" -max-depth %d", sc.MaxDepth)
			}
			if st.useSkip {
				scanCmd += " -skip-file skip-repos.txt"
			}
			if sc.AppendScanned != "" {
				scanCmd += fmt.Sprintf(" -append-scanned scanned-%s.txt", sl.Name)
			}
			run := sshCmd(sl, scanCmd)
			var errBuf bytes.Buffer
			run.Stdout = os.Stdout
			run.Stderr = io.MultiWriter(os.Stderr, &errBuf)
			if err := run.Run(); err != nil {
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
	return clusterCollectOnWith(c, outDir, ScanConfig{})
}

func clusterCollectOnWith(c *Cluster, outDir string, sc ScanConfig) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	enabled := enabledSlaves(c)
	var deltas []string
	for _, sl := range enabled {
		wd := slaveWorkDir(sl)
		// Pull every artifact that may exist (csv + pretty txt dual-write).
		for _, name := range []string{
			fmt.Sprintf("results-%s.csv", sl.Name),
			fmt.Sprintf("results-%s.txt", sl.Name),
			fmt.Sprintf("results-%s.jsonl", sl.Name),
			fmt.Sprintf("scanned-%s.txt", sl.Name),
		} {
			remote := filepath.Join(wd, name)
			local := filepath.Join(outDir, name)
			if err := scpFrom(sl, remote, local).Run(); err == nil {
				if strings.HasPrefix(name, "scanned-") {
					deltas = append(deltas, local)
				}
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
	if combined, rows, err := combineResultCSVs(outDir); err != nil {
		return fmt.Errorf("combine csv: %w", err)
	} else if combined != "" {
		fmt.Printf("combined %d row(s) → %s (per-worker CSVs kept)\n", rows, combined)
	}
	fmt.Printf("collected to %s\n", outDir)
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
