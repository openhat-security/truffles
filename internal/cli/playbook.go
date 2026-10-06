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
}

func runPlaybook(rest []string) error {
	fs := newFlagSet("playbook")
	file := fs.String("f", "", "playbook YAML file")
	detach := fs.Bool("d", false, "run in background (daemon)")
	pidfile := fs.String("pidfile", "", "write PID to file")
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

	data, err := ioutil.ReadFile(*file)
	if err != nil {
		return err
	}
	var pb Playbook
	if err := yaml.Unmarshal(data, &pb); err != nil {
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
		args := []string{os.Args[0], "playbook", "-f", *file}
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

	// Build search command
	searchArgs := buildSearchArgs(pb)
	if err := runSearch(searchArgs); err != nil {
		fmt.Fprintf(logger, "search error: %v\n", err)
		return err
	}

	// Build scan command
	scanArgs := buildScanArgs(pb)
	if err := runScan(scanArgs); err != nil {
		fmt.Fprintf(logger, "scan error: %v\n", err)
		return err
	}

	fmt.Printf("completed. out: %s\n", outPath)
	return nil
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
    format: pretty
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
	pb.Scan.Format = "pretty"
	return pb
}

func writePlaybook(pb *Playbook, path string) error {
	b, err := yaml.Marshal(pb)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, b, 0644)
}

