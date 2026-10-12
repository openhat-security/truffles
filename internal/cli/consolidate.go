package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func runConsolidate(args []string) error {
	out := "data/consolidated"
	dataDir := "data"
	updateMaster := false
	updateClean := false
	purge := false
	purgeYes := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprint(os.Stderr, consolidateUsage())
			return nil
		case a == "-o" || a == "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			out = args[i+1]
			i++
		case a == "--data-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			dataDir = args[i+1]
			i++
		case a == "--update-master":
			updateMaster = true
		case a == "--update-clean":
			updateClean = true
		case a == "--purge":
			purge = true
		case a == "--yes" || a == "-y":
			purgeYes = true
		default:
			return fmt.Errorf("unknown option: %s", a)
		}
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	allCSV := filepath.Join(out, "results-all.csv")
	fmt.Fprintf(os.Stderr, "[*] combining secrets from %s → %s\n", dataDir, allCSV)
	files, err := resolveCombineInputs(nil)
	if err != nil {
		return err
	}
	// Prefer files under dataDir when walking from cwd data/
	if dataDir != "data" {
		files, err = resolveCombineInputs([]string{dataDir})
		if err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no results-*.csv files found under %s", dataDir)
	}
	_, _, _, _, err = combineResultsDedup(files, allCSV)
	if err != nil {
		return err
	}

	verCSV := filepath.Join(out, "results-all-verified.csv")
	n, err := filterVerifiedCSV(allCSV, verCSV)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[+] %d verified row(s) → %s\n", n, verCSV)

	fmt.Fprintln(os.Stderr, "[*] merging scanned URL lists")
	scannedOut := filepath.Join(out, "scanned-repos.txt")
	master := filepath.Join(dataDir, "scanned-repos.txt")
	nScan, err := mergeScannedLists(dataDir, master, scannedOut, updateMaster)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[+] %d scanned URL(s) → %s\n", nScan, scannedOut)
	if updateMaster {
		fmt.Fprintf(os.Stderr, "[+] updated master %s\n", master)
	}

	manifest := filepath.Join(out, "manifest.txt")
	if err := writeConsolidateManifest(manifest, out, dataDir); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[+] wrote %s\n", manifest)

	if updateClean {
		fmt.Fprintln(os.Stderr, "[*] refreshing data/clean-repos.txt")
		if _, _, err := rebuildCleanRepos(cleanRebuildOpts{
			Out:            filepath.Join(dataDir, "clean-repos.txt"),
			IncludeScanned: true,
			DataDir:        dataDir,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[!] update-clean failed: %v\n", err)
		}
	}

	fmt.Fprintf(os.Stderr, "[+] done — open %s\n", out)
	if lines, err := countFileLines(allCSV); err == nil {
		fmt.Fprintf(os.Stderr, "    %d lines results-all.csv (incl. header)\n", lines)
	}
	if lines, err := countFileLines(verCSV); err == nil {
		fmt.Fprintf(os.Stderr, "    %d lines results-all-verified.csv (incl. header)\n", lines)
	}

	if purge {
		if !updateMaster {
			fmt.Fprintln(os.Stderr, "[!] --purge strongly recommended with --update-master (skip lists before deleting runs)")
		}
		return purgeRunArtifacts(dataDir, out, purgeYes)
	}
	return nil
}

func consolidateUsage() string {
	return `truffles consolidate — roll up data/ into one directory

usage:
  truffles consolidate [-o data/consolidated] [--data-dir data]
  truffles consolidate --update-master --update-clean
  truffles consolidate --update-master --purge --yes

Writes results-all.csv, results-all-verified.csv, scanned-repos.txt, manifest.txt.
`
}

func mergeScannedLists(dataDir, master, outPath string, updateMaster bool) (int, error) {
	urls := map[string]string{}
	addFile := func(p string) {
		for _, line := range strings.Split(string(mustRead(p)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			g := parseGitHubURL(line)
			if g == "" {
				continue
			}
			k := normGitHubKey(g)
			if strings.HasPrefix(line, "http") {
				urls[k] = strings.TrimRight(line, "/")
			} else {
				urls[k] = g
			}
		}
	}
	if st, err := os.Stat(master); err == nil && !st.IsDir() {
		addFile(master)
	}
	_ = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if name == "scanned-repos.txt" || (strings.HasPrefix(name, "scanned") && strings.HasSuffix(name, ".txt")) {
			addFile(path)
		}
		return nil
	})

	lines := make([]string, 0, len(urls))
	for _, u := range urls {
		lines = append(lines, u)
	}
	sort.Slice(lines, func(i, j int) bool {
		return normGitHubKey(lines[i]) < normGitHubKey(lines[j])
	})
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		return 0, err
	}
	var b strings.Builder
	for _, u := range lines {
		b.WriteString(u)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return 0, err
	}
	if updateMaster {
		tmp := master + ".tmp"
		if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
			return 0, err
		}
		if err := os.Rename(tmp, master); err != nil {
			return 0, err
		}
	}
	return len(lines), nil
}

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

func writeConsolidateManifest(path, out, dataDir string) error {
	var b strings.Builder
	b.WriteString("# consolidated " + time.Now().UTC().Format(time.RFC3339) + "\n")
	b.WriteString("# secrets: " + filepath.Join(out, "results-all.csv") + "\n")
	b.WriteString("# verified: " + filepath.Join(out, "results-all-verified.csv") + "\n")
	b.WriteString("# scanned: " + filepath.Join(out, "scanned-repos.txt") + "\n")
	b.WriteString("# run directories under " + dataDir + ":\n")
	ents, _ := os.ReadDir(dataDir)
	var dirs []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "remote-results") {
			dirs = append(dirs, filepath.Join(dataDir, name))
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		b.WriteString(d + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func countFileLines(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	if b[len(b)-1] != '\n' {
		n++
	}
	return n, nil
}

func purgeRunArtifacts(dataDir, out string, yes bool) error {
	allCSV := filepath.Join(out, "results-all.csv")
	if _, err := os.Stat(allCSV); err != nil {
		return fmt.Errorf("--purge requires %s (run consolidation first)", allCSV)
	}
	outBase := filepath.Base(out)
	keepDirs := map[string]struct{}{outBase: {}, "jobs": {}}

	var toRmDirs, toRmFiles []string
	ents, err := os.ReadDir(dataDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		full := filepath.Join(dataDir, name)
		if e.IsDir() {
			if _, ok := keepDirs[name]; ok {
				continue
			}
			looksLikeRun := strings.HasPrefix(name, "remote-results")
			if !looksLikeRun {
				if _, err := os.Stat(filepath.Join(full, "results-worker1.csv")); err == nil {
					looksLikeRun = true
				} else if _, err := os.Stat(filepath.Join(full, "results.csv")); err == nil {
					looksLikeRun = true
				} else if _, err := os.Stat(filepath.Join(full, "repos.txt")); err == nil {
					if matches, _ := filepath.Glob(filepath.Join(full, "scan-worker*.log")); len(matches) > 0 {
						looksLikeRun = true
					}
				}
			}
			if looksLikeRun {
				toRmDirs = append(toRmDirs, full)
			}
			continue
		}
		if strings.HasPrefix(name, "repos-loop-") && strings.HasSuffix(name, ".txt") {
			toRmFiles = append(toRmFiles, full)
		}
	}
	sort.Strings(toRmDirs)
	sort.Strings(toRmFiles)

	if len(toRmDirs) == 0 && len(toRmFiles) == 0 {
		fmt.Fprintf(os.Stderr, "[=] --purge: nothing to remove under %s\n", dataDir)
		return nil
	}

	purgeLog := filepath.Join(out, "purge.log")
	var logBuf strings.Builder
	logBuf.WriteString("# purge " + time.Now().UTC().Format(time.RFC3339) + "\n")
	for _, d := range toRmDirs {
		logBuf.WriteString("dir " + d + "\n")
	}
	for _, f := range toRmFiles {
		logBuf.WriteString("file " + f + "\n")
	}
	pf, _ := os.OpenFile(purgeLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if pf != nil {
		_, _ = pf.WriteString(logBuf.String())
		_ = pf.Close()
	}

	if !yes {
		fmt.Fprintf(os.Stderr, "[!] --purge would remove %d dir(s) and %d file(s) under %s\n",
			len(toRmDirs), len(toRmFiles), dataDir)
		fmt.Fprintln(os.Stderr, "    Re-run with --purge --yes (recommended: --update-master --update-clean too)")
		return fmt.Errorf("purge requires --yes")
	}

	fmt.Fprintf(os.Stderr, "[*] purging %d dir(s), %d repos-loop file(s)\n", len(toRmDirs), len(toRmFiles))
	for _, d := range toRmDirs {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "    removed %s\n", d)
	}
	for _, f := range toRmFiles {
		_ = os.Remove(f)
		fmt.Fprintf(os.Stderr, "    removed %s\n", f)
	}
	fmt.Fprintf(os.Stderr, "[+] purge done (log appended to %s)\n", purgeLog)
	return nil
}
