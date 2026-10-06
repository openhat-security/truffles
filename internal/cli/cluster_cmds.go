package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	default:
		fmt.Print(clusterUsage())
		return fmt.Errorf("unknown cluster command %q", cmd)
	}
}

func clusterUsage() string {
	return `truffles cluster - distributed scanning across multiple machines via SSH

usage:
  truffles cluster init [-o cluster.yaml]        create cluster config
  truffles cluster run -c cluster.yaml -f repos.txt [-w N] [-format csv]
  truffles cluster distribute -c cluster.yaml -f repos.txt
  truffles cluster collect -c cluster.yaml -out ./results
  truffles cluster status -c cluster.yaml

Cluster YAML example:
  name: mycluster
  master: alice@master
  slaves:
    - name: node1
      host: 10.0.0.10
      user: alice
      enabled: true
      workdir: /home/alice/truffles-work
      truffles_path: truffles
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
	enabled := []Slave{}
	for _, s := range c.Slaves {
		if s.Enabled == false {
			continue
		}
		enabled = append(enabled, s)
	}
	if len(enabled) == 0 && len(c.Slaves) > 0 {
		enabled = c.Slaves
	}
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
		wd := sl.WorkDir
		if wd == "" {
			wd = "~/truffles-work"
		}
		remote := filepath.Join(wd, "repos.txt")
		cmd := scpTo(sl, filepath.Join(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name)), remote)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
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

func clusterRun(rest []string) error {
	fs := flag.NewFlagSet("cluster run", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster config")
	f := fs.String("f", "repos.txt", "repos list")
	workers := fs.Int("w", 4, "workers per slave")
	format := fs.String("format", "csv", "report format")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	enabled := []Slave{}
	for _, s := range c.Slaves {
		if s.Enabled == false {
			continue
		}
		enabled = append(enabled, s)
	}
	if len(enabled) == 0 {
		enabled = c.Slaves
	}
	if len(enabled) == 0 {
		return fmt.Errorf("no slaves configured")
	}
	splits, err := splitLines(*f, len(enabled))
	if err != nil {
		return err
	}
	tmpdir, _ := os.MkdirTemp("", "truffles-crun-")
	defer os.RemoveAll(tmpdir)

	// distribute and run in parallel is nice, but sequential is simple and safe
	for i, sl := range enabled {
		if i >= len(splits) {
			continue
		}
		wd := sl.WorkDir
		if wd == "" {
			wd = "~/truffles-work"
		}
		chunkPath := filepath.Join(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name))
		writeLines(tmpdir, fmt.Sprintf("repos-%s.txt", sl.Name), splits[i])
		scpTo(sl, chunkPath, filepath.Join(wd, "repos.txt")).Run()
		scanCmd := fmt.Sprintf("cd %s && %s scan -f repos.txt -workers %d -format %s -out results-%s.csv", wd, binOrPath(sl.TrufflesPath), *workers, *format, sl.Name)
		run := sshCmd(sl, scanCmd)
		run.Stdout = os.Stdout
		run.Stderr = os.Stderr
		if err := run.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "slave %s failed: %v\n", sl.Name, err)
		}
	}
	fmt.Println("cluster run done")
	return nil
}

func clusterCollect(rest []string) error {
	fs := flag.NewFlagSet("cluster collect", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster config")
	outDir := fs.String("out", "./results", "output dir for collected results")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	enabled := []Slave{}
	for _, s := range c.Slaves {
		if s.Enabled == false {
			continue
		}
		enabled = append(enabled, s)
	}
	if len(enabled) == 0 {
		enabled = c.Slaves
	}
	for _, sl := range enabled {
		wd := sl.WorkDir
		if wd == "" {
			wd = "~/truffles-work"
		}
		remote := filepath.Join(wd, fmt.Sprintf("results-%s.csv", sl.Name))
		local := filepath.Join(*outDir, fmt.Sprintf("results-%s.csv", sl.Name))
		scpFrom(sl, remote, local).Run()
	}
	fmt.Printf("collected to %s\n", *outDir)
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
