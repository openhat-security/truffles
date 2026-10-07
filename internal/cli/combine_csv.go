package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const combinedCSVName = "results.csv"

// combineResultCSVs merges results-*.csv in outDir into results.csv.
// Per-worker files are left in place. Header is taken from the first file;
// subsequent headers are skipped when they match.
func combineResultCSVs(outDir string) (string, int, error) {
	matches, err := filepath.Glob(filepath.Join(outDir, "results-*.csv"))
	if err != nil {
		return "", 0, err
	}
	var sources []string
	for _, m := range matches {
		base := filepath.Base(m)
		if base == combinedCSVName {
			continue
		}
		sources = append(sources, m)
	}
	sort.Strings(sources)
	if len(sources) == 0 {
		return "", 0, nil
	}

	outPath := filepath.Join(outDir, combinedCSVName)
	out, err := os.Create(outPath)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()

	var header string
	rows := 0
	for _, src := range sources {
		f, err := os.Open(src)
		if err != nil {
			return "", 0, err
		}
		sc := bufio.NewScanner(f)
		// Allow long CSV lines (secrets / paths).
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		lineNo := 0
		for sc.Scan() {
			line := sc.Text()
			lineNo++
			if lineNo == 1 {
				if header == "" {
					header = line
					if _, err := fmt.Fprintln(w, line); err != nil {
						f.Close()
						return "", 0, err
					}
				} else if line != header {
					// Divergent header — keep the row rather than drop data.
					if _, err := fmt.Fprintln(w, line); err != nil {
						f.Close()
						return "", 0, err
					}
					rows++
				}
				continue
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				f.Close()
				return "", 0, err
			}
			rows++
		}
		errScan := sc.Err()
		f.Close()
		if errScan != nil {
			return "", 0, errScan
		}
	}
	if err := w.Flush(); err != nil {
		return "", 0, err
	}
	return outPath, rows, nil
}
