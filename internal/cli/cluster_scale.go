package cli

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// clusterScale resizes GCE machine types and/or bumps playbook scan concurrency
// (slaves[].workers). Machine resize stops the VM (interrupts that worker's
// current scan). Concurrency changes apply on the next cluster cycle.
func clusterScale(rest []string) error {
	fs := flag.NewFlagSet("cluster scale", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster / playbook YAML")
	workerFlag := fs.String("worker", "", "comma-separated slave names (default: all enabled)")
	fs.StringVar(workerFlag, "workers-named", "", "alias for -worker")
	concurrency := fs.Int("workers", -1, "set slaves[].workers (concurrent clones) on selected VMs")
	machine := fs.String("machine", "", "GCE machine type (e2-standard-2, e2-small, …) — stops/starts VM")
	account := fs.String("account", "", "gcloud user account (default: first non-service-account from auth list)")
	yes := fs.Bool("y", false, "skip confirmation for GCE resize")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	gcloudAccountOverride = strings.TrimSpace(*account)
	defer func() { gcloudAccountOverride = "" }()
	if *concurrency < 0 && strings.TrimSpace(*machine) == "" {
		return fmt.Errorf("usage: truffles cluster scale -c playbook.yaml [--worker name,…] --workers N [--machine TYPE]\n" +
			"  --workers N     bump concurrent clones in the YAML (next cycle)\n" +
			"  --machine TYPE  resize GCE VM (stop → set-machine-type → start)\n" +
			"examples:\n" +
			"  truffles cluster scale -c remote-playbook.yaml --workers 4\n" +
			"  truffles cluster scale -c remote-playbook.yaml --worker worker3,worker4 --machine e2-standard-2 --workers 4")
	}

	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	targets, err := selectScaleSlaves(c, *workerFlag)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("no matching enabled workers")
	}

	fmt.Printf("scaling %d worker(s):", len(targets))
	for _, sl := range targets {
		fmt.Printf(" %s", sl.Name)
	}
	fmt.Println()

	if *concurrency >= 0 {
		if *concurrency == 0 {
			return fmt.Errorf("--workers must be ≥ 1")
		}
		n, err := patchPlaybookSlaveWorkers(*cfg, namesOf(targets), *concurrency)
		if err != nil {
			return err
		}
		fmt.Printf("[+] updated workers: %d on %d slave(s) in %s (takes effect next scan cycle)\n",
			*concurrency, n, *cfg)
		for _, sl := range targets {
			fmt.Printf("    %s: workers %d → %d\n", sl.Name, sl.Workers, *concurrency)
		}
	}

	if mt := strings.TrimSpace(*machine); mt != "" {
		if !*yes {
			fmt.Fprintf(os.Stderr, "[!] GCE resize STOPS each VM (current scan on that worker is interrupted).\n")
			fmt.Fprintf(os.Stderr, "    Continue for %d worker(s)? [y/N] ", len(targets))
			var ans string
			_, _ = fmt.Scanln(&ans)
			if !strings.EqualFold(strings.TrimSpace(ans), "y") && !strings.EqualFold(strings.TrimSpace(ans), "yes") {
				return fmt.Errorf("aborted")
			}
		}
		for _, sl := range targets {
			if sl.GCEInstance == "" || sl.GCEZone == "" {
				fmt.Fprintf(os.Stderr, "[!] %s has no gce_instance/gce_zone — skip machine resize\n", sl.Name)
				continue
			}
			fmt.Printf("[*] resizing %s (%s) → %s…\n", sl.Name, sl.GCEInstance, mt)
			if err := gcloudResizeMachine(sl, mt); err != nil {
				return fmt.Errorf("%s: %w", sl.Name, err)
			}
			fmt.Printf("[+] %s is now %s\n", sl.Name, mt)
			if sug := suggestedWorkersForMachine(mt); sug > 0 && *concurrency < 0 {
				fmt.Printf("    tip: suggested --workers %d for %s\n", sug, mt)
			}
		}
		fmt.Println("[*] waiting for SSH after resize…")
		deadline := time.Now().Add(3 * time.Minute)
		for _, sl := range targets {
			if sl.GCEInstance == "" {
				continue
			}
			for time.Now().Before(deadline) {
				if slaveSSHReady(sl) {
					fmt.Printf("[+] %s SSH ready\n", sl.Name)
					break
				}
				time.Sleep(5 * time.Second)
			}
		}
	}

	CoreLog("cluster-scale", fmt.Sprintf("workers=%v concurrency=%d machine=%s", namesOf(targets), *concurrency, *machine))
	fmt.Println("done. Running job keeps going; new concurrency applies on the next cycle.")
	fmt.Println("  check:  truffles job status")
	fmt.Println("  deploy: truffles cluster deploy -c " + *cfg + "   # if binary missing after resize")
	return nil
}

func namesOf(slaves []Slave) []string {
	out := make([]string, len(slaves))
	for i, s := range slaves {
		out[i] = s.Name
	}
	return out
}

func selectScaleSlaves(c *Cluster, workerCSV string) ([]Slave, error) {
	enabled := enabledSlaves(c)
	want := map[string]bool{}
	for _, p := range strings.Split(workerCSV, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			want[p] = true
		}
	}
	if len(want) == 0 {
		return enabled, nil
	}
	var out []Slave
	for _, sl := range enabled {
		if want[sl.Name] {
			out = append(out, sl)
			delete(want, sl.Name)
		}
	}
	if len(want) > 0 {
		var missing []string
		for n := range want {
			missing = append(missing, n)
		}
		return out, fmt.Errorf("unknown worker(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// patchPlaybookSlaveWorkers sets `workers: N` under matching `- name:` slave blocks.
func patchPlaybookSlaveWorkers(path string, names []string, workers int) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	lines := strings.Split(string(b), "\n")
	updated := 0
	current := ""
	inSlaves := false
	workersRe := regexp.MustCompile(`^(\s*workers:\s*)\d+(.*)$`)
	nameRe := regexp.MustCompile(`^\s*-\s*name:\s*(\S+)\s*$`)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "slaves:" {
			inSlaves = true
			current = ""
			continue
		}
		if !inSlaves {
			continue
		}
		// Left top-level key → left slaves block.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(trim, "#") && trim != "" {
			inSlaves = false
			current = ""
			continue
		}
		if m := nameRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current != "" && want[current] {
			if m := workersRe.FindStringSubmatch(line); m != nil {
				lines[i] = m[1] + strconv.Itoa(workers) + m[2]
				updated++
				continue
			}
		}
	}
	if updated < len(want) {
		var inserted int
		lines, inserted = insertMissingSlaveWorkers(lines, want, workers)
		updated += inserted
	}
	if updated == 0 {
		return 0, fmt.Errorf("no workers: lines found for %v in %s", names, path)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return 0, err
	}
	return updated, nil
}

func insertMissingSlaveWorkers(lines []string, want map[string]bool, workers int) ([]string, int) {
	// Track which names already got a workers: line by scanning again after replace.
	has := map[string]bool{}
	nameRe := regexp.MustCompile(`^\s*-\s*name:\s*(\S+)\s*$`)
	workersRe := regexp.MustCompile(`^\s*workers:\s*\d+`)
	current := ""
	inSlaves := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "slaves:" {
			inSlaves = true
			continue
		}
		if !inSlaves {
			continue
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(trim, "#") && trim != "" {
			inSlaves = false
			continue
		}
		if m := nameRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current != "" && want[current] && workersRe.MatchString(line) {
			has[current] = true
		}
	}
	inserted := 0
	var out []string
	current = ""
	inSlaves = false
	for _, line := range lines {
		out = append(out, line)
		trim := strings.TrimSpace(line)
		if trim == "slaves:" {
			inSlaves = true
			continue
		}
		if !inSlaves {
			continue
		}
		if m := nameRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			if want[current] && !has[current] {
				// Indent like sibling keys (2 spaces past the "- ").
				indent := "    "
				if i := strings.Index(line, "-"); i >= 0 {
					indent = strings.Repeat(" ", i+2)
				}
				out = append(out, fmt.Sprintf("%sworkers: %d", indent, workers))
				has[current] = true
				inserted++
			}
		}
	}
	return out, inserted
}

func gcloudResizeMachine(sl Slave, machine string) error {
	base := []string{"compute", "instances"}
	proj := []string{}
	if sl.GCEProject != "" {
		proj = []string{"--project", sl.GCEProject}
	}
	zone := []string{"--zone", sl.GCEZone}
	name := sl.GCEInstance

	stopArgs := append(append(append([]string{}, base...), "stop", name), append(zone, proj...)...)
	stopArgs = append(stopArgs, "--quiet")
	if err := runGcloud(stopArgs...); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	setArgs := append(append(append([]string{}, base...), "set-machine-type", name), append(zone, proj...)...)
	setArgs = append(setArgs, "--machine-type", machine, "--quiet")
	if err := runGcloud(setArgs...); err != nil {
		// Best-effort restart so the worker is not left stopped.
		_ = runGcloud(append(append(append([]string{}, base...), "start", name), append(append(zone, proj...), "--quiet")...)...)
		return fmt.Errorf("set-machine-type: %w", err)
	}
	startArgs := append(append(append([]string{}, base...), "start", name), append(zone, proj...)...)
	startArgs = append(startArgs, "--quiet")
	if err := runGcloud(startArgs...); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

func suggestedWorkersForMachine(machine string) int {
	switch {
	case strings.Contains(machine, "e2-small"):
		return 2
	case strings.Contains(machine, "e2-medium"):
		return 3
	case strings.Contains(machine, "e2-standard-2"):
		return 4
	case strings.Contains(machine, "e2-standard-4"):
		return 6
	case strings.Contains(machine, "e2-standard-8"):
		return 10
	default:
		return 0
	}
}
