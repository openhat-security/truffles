package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// printInterruptedLocalPaths writes absolute paths of known local artifacts under
// dir so terminals can offer clickable links after Ctrl-C / SIGTERM.
func printInterruptedLocalPaths(w io.Writer, dir string) {
	if dir == "" {
		return
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	fmt.Fprintf(w, "[!] interrupted — local files:\n")
	fmt.Fprintf(w, "  %s\n", absDir)

	patterns := []string{
		"results.csv",
		"results-verified.csv",
		"results-*.csv",
		"results-*.txt",
		"results-*.jsonl",
		"scan-*.log",
		"scanned-*.txt",
		"clean-*.json",
		"repos.txt",
		"performance.json",
	}
	seen := map[string]struct{}{}
	var paths []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(absDir, pat))
		for _, m := range matches {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			if st, err := os.Stat(m); err != nil || st.IsDir() {
				continue
			}
			paths = append(paths, m)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		fmt.Fprintf(w, "  (no result files mirrored yet — check the directory above)\n")
		return
	}
	for _, p := range paths {
		fmt.Fprintf(w, "  %s\n", p)
	}
}

// absPathOrSelf returns filepath.Abs(p) or p on error.
func absPathOrSelf(p string) string {
	if p == "" {
		return p
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}
