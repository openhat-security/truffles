package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	combinedCSVName         = "results.csv"
	combinedVerifiedCSVName = "results-verified.csv"
)

// combineResultCSVs merges results-*.csv in outDir into results.csv and also
// writes results-verified.csv (verified=true rows only) for spreadsheet use.
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
		if base == combinedCSVName || base == combinedVerifiedCSVName {
			continue
		}
		if strings.HasSuffix(base, "-verified.csv") {
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

	verPath := filepath.Join(outDir, combinedVerifiedCSVName)
	verOut, err := os.Create(verPath)
	if err != nil {
		return "", 0, err
	}
	defer verOut.Close()
	vw := bufio.NewWriter(verOut)
	defer vw.Flush()

	var header string
	verifiedCol := -1
	rows := 0
	verifiedRows := 0
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
					verifiedCol = csvColumnIndex(header, "verified")
					if _, err := fmt.Fprintln(w, line); err != nil {
						f.Close()
						return "", 0, err
					}
					if _, err := fmt.Fprintln(vw, line); err != nil {
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
					if csvLineVerified(line, verifiedCol) {
						if _, err := fmt.Fprintln(vw, line); err != nil {
							f.Close()
							return "", 0, err
						}
						verifiedRows++
					}
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
			if csvLineVerified(line, verifiedCol) {
				if _, err := fmt.Fprintln(vw, line); err != nil {
					f.Close()
					return "", 0, err
				}
				verifiedRows++
			}
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
	if err := vw.Flush(); err != nil {
		return "", 0, err
	}
	_ = verifiedRows
	return outPath, rows, nil
}

func countCSVDataRows(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	n := 0
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo == 1 {
			continue
		}
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		n++
	}
	return n, sc.Err()
}

func csvColumnIndex(header, name string) int {
	cols := splitCSVLine(header)
	for i, c := range cols {
		if strings.EqualFold(strings.TrimSpace(c), name) {
			return i
		}
	}
	return -1
}

func csvLineVerified(line string, verifiedCol int) bool {
	if verifiedCol < 0 {
		return false
	}
	cols := splitCSVLine(line)
	if verifiedCol >= len(cols) {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(cols[verifiedCol]))
	return v == "true" || v == "1" || v == "yes"
}

// splitCSVLine is a minimal splitter for our results CSVs (no embedded commas
// in the verified column; secrets may contain commas — we only need an early column).
func splitCSVLine(line string) []string {
	var cols []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if inQuotes && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
				continue
			}
			inQuotes = !inQuotes
		case c == ',' && !inQuotes:
			cols = append(cols, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	cols = append(cols, cur.String())
	return cols
}
