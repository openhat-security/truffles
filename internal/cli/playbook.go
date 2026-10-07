package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Playbook struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Search      SearchConfig      `yaml:"search"`
	Scan        ScanConfig        `yaml:"scan"`
	Env         map[string]string `yaml:"env"`
	LogDir      string            `yaml:"log_dir"`
	// Optional remote workers: when set, search runs locally then scan
	// is distributed over SSH (same shape as cluster.yaml slaves).
	Master     string  `yaml:"master"`
	Slaves     []Slave `yaml:"slaves"`
	DataDir    string  `yaml:"data_dir"`    // parent dir for runs (default: data)
	CollectOut string  `yaml:"collect_out"` // scan name under data_dir/ (auto-suffix -1,-2,…)
	EnvFile    string  `yaml:"env_file"`    // dotenv loaded before ${VAR} expansion
	// AutoHeal defaults to true when any slave has gce_instance set.
	AutoHeal *bool `yaml:"auto_heal"`
}

type SearchConfig struct {
	Owner     string   `yaml:"owner"`
	Queries   []string `yaml:"queries"`
	Filter    string   `yaml:"filter"`
	Regex     bool     `yaml:"regex"`
	Limit     int      `yaml:"limit"`
	Out       string   `yaml:"out"`
	Workers   int      `yaml:"workers"`
	PoolSize  int      `yaml:"pool_size"`
	NoProxy   bool     `yaml:"no_proxy"`
	UseDirect *bool    `yaml:"use_direct"`
	PoolWait  string   `yaml:"pool_wait"`
	Token     string   `yaml:"token"`
	Progress  string   `yaml:"progress"`
}

type ScanConfig struct {
	File           string   `yaml:"file"`
	Workers        int      `yaml:"workers"`
	Token          string   `yaml:"token"`
	Format         string   `yaml:"format"`
	Out            string   `yaml:"out"`
	JSON           *bool    `yaml:"json"`
	NoVerification bool     `yaml:"no_verification"`
	MaxDepth       int      `yaml:"max_depth"`
	ExcludePaths   []string `yaml:"exclude_paths"`
	Color          string   `yaml:"color"`
	Progress       string   `yaml:"progress"`
	// NoProxy: nil/true = direct clones (default, best for parallel throughput);
	// false = use proxy pool (-no-proxy=false).
	NoProxy       *bool  `yaml:"no_proxy"`
	PoolSize      int    `yaml:"pool_size"`
	UseDirect     *bool  `yaml:"use_direct"`
	PoolWait      string `yaml:"pool_wait"`
	SkipFile      string `yaml:"skip_file"`
	AppendScanned string `yaml:"append_scanned"`
}

// runPlaybookHeal is `truffles playbook heal -f <yaml>` — same as cluster heal -c.
func runPlaybookHeal(rest []string) error {
	fs := flag.NewFlagSet("playbook heal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	healUsage := `truffles playbook heal -f <playbook.yaml>

Stop local playbook + remote scans, gcloud reset workers, wait for SSH,
cross-compile and deploy truffles, then print the re-run command.

Same as: truffles cluster heal -c <playbook.yaml>
`
	fs.Usage = func() { fmt.Fprint(os.Stderr, healUsage) }
	file := fs.String("f", "", "playbook YAML file")
	fs.StringVar(file, "c", "", "alias for -f")
	skipReset := fs.Bool("no-reset", false, "Skip gcloud instance reset")
	skipDeploy := fs.Bool("no-deploy", false, "Skip binary deploy")
	wait := fs.Duration("wait", 3*time.Minute, "How long to wait for SSH after reset")
	local := fs.String("bin", "", "local binary to upload (default: cross-compile linux)")
	if err := fs.Parse(rest); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *file == "" {
		return fmt.Errorf("playbook heal: -f <playbook.yaml> required")
	}
	args := []string{"-c", *file, "-wait", wait.String()}
	if *skipReset {
		args = append(args, "-no-reset")
	}
	if *skipDeploy {
		args = append(args, "-no-deploy")
	}
	if *local != "" {
		args = append(args, "-bin", *local)
	}
	return clusterHeal(args)
}

func runPlaybook(rest []string) error {
	fs := newFlagSet("playbook")
	file := fs.String("f", "", "playbook YAML file")
	detach := fs.Bool("d", false, "run in background (daemon)")
	pidfile := fs.String("pidfile", "", "write PID to file")
	dataDir := fs.String("data-dir", "", "parent output directory (default: data_dir from YAML, or data)")
	out := fs.String("out", "", "scan name under data-dir (default: collect_out from YAML, or remote-results)")
	yes := fs.Bool("y", false, "non-interactive (skip data-dir / scan-name prompts)")
	for _, r := range rest {
		if r == "-h" || r == "--help" || r == "-help" {
			p := palette{on: resolveColor("auto", os.Stdout)}
			p.printPlaybookUsage(os.Stdout)
			return nil
		}
	}
	if err := parseFlags(fs, rest); err != nil {
		if err == flag.ErrHelp || strings.Contains(err.Error(), "help") {
			p := palette{on: resolveColor("auto", os.Stdout)}
			p.printPlaybookUsage(os.Stdout)
			return nil
		}
		return err
	}
	if *file == "" {
		p := palette{on: resolveColor("auto", os.Stdout)}
		p.printPlaybookUsage(os.Stdout)
		return fmt.Errorf("-f playbook required")
	}

	pb, err := loadPlaybook(*file)
	if err != nil {
		return err
	}

	if pb.LogDir == "" {
		if isMac() {
			pb.LogDir = "/usr/local/var/log/truffles"
		} else {
			pb.LogDir = "/var/log/truffles"
		}
	}
	if err := os.MkdirAll(pb.LogDir, 0755); err != nil {
		// fallback to temp
		pb.LogDir = os.TempDir()
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	logPath := filepath.Join(pb.LogDir, fmt.Sprintf("%s-%s.log", safeName(pb.Name), timestamp))
	outPath := filepath.Join(pb.LogDir, fmt.Sprintf("%s-%s.out", safeName(pb.Name), timestamp))

	if *detach {
		args := []string{os.Args[0], "playbook", "-f", *file, "-y"}
		if *dataDir != "" {
			args = append(args, "-data-dir", *dataDir)
		}
		if *out != "" {
			args = append(args, "-out", *out)
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = os.Environ()
		for k, v := range pb.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		logf, err := os.Create(logPath)
		if err != nil {
			return err
		}
		cmd.Stdout = logf
		cmd.Stderr = logf
		if err := cmd.Start(); err != nil {
			return err
		}
		if *pidfile != "" {
			ioutil.WriteFile(*pidfile, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0644)
		}
		fmt.Printf("started playbook %q (pid=%d)\nlog: %s\n", pb.Name, cmd.Process.Pid, logPath)
		return nil
	}

	// foreground
	if len(pb.Env) > 0 {
		for k, v := range pb.Env {
			os.Setenv(k, v)
		}
	}

	fmt.Printf("running playbook: %s\nlog: %s\n", pb.Name, logPath)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer logf.Close()
	logger := bufio.NewWriter(logf)
	defer logger.Flush()

	lay, err := resolveRunLayout(runLayoutOpts{
		DataDirFlag: *dataDir,
		OutFlag:     *out,
		YAMLDataDir: pb.DataDir,
		YAMLOut:     pb.CollectOut,
		Prompt:      !*yes && !*detach,
	})
	if err != nil {
		fmt.Fprintf(logger, "collect dir error: %v\n", err)
		return err
	}
	collectDir := lay.CollectDir
	fmt.Printf("[*] run dir: %s\n", collectDir)

	var reposFile string
	if playbookHasSearch(pb) {
		reposFile = filepath.Join(collectDir, reposListBasename(pb))
		pb.Search.Out = reposFile
		if err := runSearch(buildSearchArgs(*pb)); err != nil {
			fmt.Fprintf(logger, "search error: %v\n", err)
			return err
		}
	} else if existing := firstExisting(pb.Scan.File); existing != "" {
		reposFile = existing
	} else {
		return fmt.Errorf("playbook has no search.owner/queries and no existing scan.file")
	}
	pb.Scan.File = reposFile

	if len(pb.Slaves) > 0 {
		c := &Cluster{
			Name:       pb.Name,
			Master:     pb.Master,
			Slaves:     pb.Slaves,
			AutoHeal:   pb.AutoHeal,
			CollectOut: pb.CollectOut,
		}
		workers := pb.Scan.Workers
		if workers == 0 {
			workers = 4
		}
		format := pb.Scan.Format
		if format == "" {
			format = "csv"
		}
		fmt.Printf("remote scan via %d slave(s)\n", len(enabledSlaves(c)))
		if clusterAutoHealEnabled(c) {
			fmt.Println("[*] auto_heal on — unreachable workers will be reset and redeployed")
		}
		if err := clusterRunOn(c, reposFile, workers, format, pb.Scan); err != nil {
			fmt.Fprintf(logger, "remote scan error: %v\n", err)
			return err
		}
		if err := clusterCollectOnWith(c, collectDir, pb.Scan); err != nil {
			fmt.Fprintf(logger, "collect error: %v\n", err)
			return err
		}
		fmt.Printf("completed. results: %s\n", collectDir)
		return nil
	}

	// Local scan
	scanArgs := buildScanArgs(*pb)
	if err := runScan(scanArgs); err != nil {
		fmt.Fprintf(logger, "scan error: %v\n", err)
		return err
	}

	fmt.Printf("completed. out: %s (repos: %s)\n", outPath, reposFile)
	return nil
}

func loadPlaybook(path string) (*Playbook, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pb Playbook
	if err := yaml.Unmarshal(data, &pb); err != nil {
		return nil, err
	}
	if ef := resolveEnvFile(path, pb.EnvFile); ef != "" {
		if err := loadEnvFile(ef); err != nil {
			return nil, fmt.Errorf("env_file %s: %w", ef, err)
		}
	}
	expandPlaybook(&pb)
	return &pb, nil
}

func playbookHasSearch(pb *Playbook) bool {
	if pb == nil {
		return false
	}
	return pb.Search.Owner != "" || len(pb.Search.Queries) > 0
}

// reposListBasename is the filename for the repo URL list inside the run dir.
func reposListBasename(pb *Playbook) string {
	if pb == nil {
		return "repos.txt"
	}
	if pb.Search.Out != "" {
		return filepath.Base(pb.Search.Out)
	}
	if pb.Scan.File != "" {
		return filepath.Base(pb.Scan.File)
	}
	return "repos.txt"
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func buildSearchArgs(pb Playbook) []string {
	var args []string
	if pb.Search.Owner != "" {
		args = append(args, "-owner", pb.Search.Owner)
	}
	if len(pb.Search.Queries) > 0 {
		args = append(args, pb.Search.Queries...)
	}
	if pb.Search.Filter != "" {
		args = append(args, "-filter", pb.Search.Filter)
	}
	if pb.Search.Regex {
		args = append(args, "-regex")
	}
	if pb.Search.Limit != 0 {
		args = append(args, "-limit", fmt.Sprintf("%d", pb.Search.Limit))
	}
	if pb.Search.Out != "" {
		args = append(args, "-out", pb.Search.Out)
	} else {
		args = append(args, "-out", "repos.txt")
	}
	if pb.Search.Workers != 0 {
		args = append(args, "-workers", fmt.Sprintf("%d", pb.Search.Workers))
	}
	if pb.Search.PoolSize != 0 {
		args = append(args, "-pool-size", fmt.Sprintf("%d", pb.Search.PoolSize))
	}
	if pb.Search.NoProxy {
		args = append(args, "-no-proxy")
	}
	if pb.Search.UseDirect != nil && !*pb.Search.UseDirect {
		args = append(args, "-no-direct")
	}
	if pb.Search.PoolWait != "" {
		args = append(args, "-pool-wait", pb.Search.PoolWait)
	}
	if pb.Search.Token != "" {
		args = append(args, "-token", pb.Search.Token)
	}
	if pb.Search.Progress != "" {
		args = append(args, "-progress", pb.Search.Progress)
	}
	return args
}

func buildScanArgs(pb Playbook) []string {
	var args []string
	f := pb.Scan.File
	if f == "" {
		if pb.Search.Out != "" {
			f = pb.Search.Out
		} else {
			f = "repos.txt"
		}
	}
	args = append(args, "-f", f)
	if pb.Scan.Workers != 0 {
		args = append(args, "-workers", fmt.Sprintf("%d", pb.Scan.Workers))
	}
	if pb.Scan.Token != "" {
		args = append(args, "-token", pb.Scan.Token)
	}
	if pb.Scan.Format != "" {
		args = append(args, "-format", pb.Scan.Format)
	}
	if pb.Scan.Out != "" {
		args = append(args, "-out", pb.Scan.Out)
	}
	if pb.Scan.JSON != nil && !*pb.Scan.JSON {
		args = append(args, "-json=false")
	}
	if pb.Scan.NoVerification {
		args = append(args, "-no-verification")
	}
	if pb.Scan.MaxDepth != 0 {
		args = append(args, "-max-depth", fmt.Sprintf("%d", pb.Scan.MaxDepth))
	}
	if len(pb.Scan.ExcludePaths) > 0 {
		args = append(args, "-exclude-paths", strings.Join(pb.Scan.ExcludePaths, ","))
	}
	if pb.Scan.Color != "" {
		args = append(args, "-color", pb.Scan.Color)
	}
	if pb.Scan.Progress != "" {
		args = append(args, "-progress", pb.Scan.Progress)
	}
	args = append(args, scanNoProxyFlag(pb.Scan.NoProxy))
	if pb.Scan.PoolSize != 0 {
		args = append(args, "-pool-size", fmt.Sprintf("%d", pb.Scan.PoolSize))
	}
	if pb.Scan.UseDirect != nil && !*pb.Scan.UseDirect {
		args = append(args, "-no-direct")
	}
	if pb.Scan.PoolWait != "" {
		args = append(args, "-pool-wait", pb.Scan.PoolWait)
	}
	if pb.Scan.SkipFile != "" {
		args = append(args, "-skip-file", pb.Scan.SkipFile)
	}
	if pb.Scan.AppendScanned != "" {
		args = append(args, "-append-scanned", pb.Scan.AppendScanned)
	}
	return args
}

func safeName(s string) string {
	if s == "" {
		return "playbook"
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

func playbookUsage() string {
	return `truffles playbook - run a playbook from YAML

usage:
  truffles playbook -f <playbook.yaml> [flags]

flags:
  -f string      playbook YAML file (required)
  -d             run in background (daemon)
  -pidfile string write PID to file

Playbook YAML example:
  name: sample
  description: find and scan
  search:
    owner: BurntSushi
    queries:
      - "*llm*"
    out: repos.txt
    limit: 100
    workers: 4
  scan:
    file: repos.txt
    workers: 4
    format: csv
    skip_file: data/scanned-repos.txt
    append_scanned: data/scanned-repos.txt
    exclude_paths:
      - node_modules
      - vendor

Logs go to log_dir if set, otherwise /usr/local/var/log/truffles (macOS)
or /var/log/truffles (Linux). Falls back to temp dir if not writable.
`
}

func genPlaybook(name string) *Playbook {
	pb := &Playbook{
		Name:        name,
		Description: "search and scan repos",
		LogDir:      "",
		Env:         map[string]string{},
	}
	pb.Search.Out = "repos.txt"
	pb.Search.Workers = 4
	pb.Scan.File = "repos.txt"
	pb.Scan.Workers = 4
	pb.Scan.Format = "csv"
	return pb
}

func writePlaybook(pb *Playbook, path string) error {
	b, err := yaml.Marshal(pb)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, b, 0644)
}
