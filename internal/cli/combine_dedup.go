package cli

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runCombine is `truffles combine` — secret-dedup merge of results-*.csv files.
func runCombine(args []string) error {
	fs := flag.NewFlagSet("combine", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	out := fs.String("o", "", "output CSV (default: <dir>/results-combined.csv or data/results-combined.csv)")
	fs.StringVar(out, "out", "", "output CSV")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(os.Stderr, combineUsage())
			return nil
		}
		return err
	}
	files, err := resolveCombineInputs(fs.Args())
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no results-*.csv files found")
	}
	outPath := *out
	if outPath == "" {
		if fs.NArg() == 1 {
			if st, err := os.Stat(fs.Arg(0)); err == nil && st.IsDir() {
				outPath = filepath.Join(fs.Arg(0), "results-combined.csv")
			}
		}
		if outPath == "" {
			outPath = "data/results-combined.csv"
		}
	}
	nFiles, totalIn, unique, skipped, err := combineResultsDedup(files, outPath)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("[+] %d file(s), %d rows in → %d unique secrets", nFiles, totalIn, unique)
	if skipped > 0 {
		msg += fmt.Sprintf(" (skipped %d empty secret)", skipped)
	}
	fmt.Fprintln(os.Stderr, msg)
	fmt.Fprintf(os.Stderr, "[+] wrote %s\n", outPath)
	fmt.Printf("done: %s\n", outPath)
	return nil
}

func combineUsage() string {
	return `truffles combine — merge results CSVs, dedupe by secret

usage:
  truffles combine [dirs|files|globs...] -o <csv>

Prefer verified=true, then https://github.com/… URLs over file:// paths.
With no inputs, scans data/**/results-*.csv (skips results.csv).
`
}

func resolveCombineInputs(inputs []string) ([]string, error) {
	var files []string
	seen := map[string]struct{}{}
	add := func(p string) {
		p = filepath.Clean(p)
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if _, ok := seen[abs]; ok {
			return
		}
		base := filepath.Base(p)
		if base == combinedCSVName || base == combinedVerifiedCSVName || base == "results-combined.csv" {
			return
		}
		if strings.HasSuffix(strings.ToLower(base), "-verified.csv") {
			return
		}
		seen[abs] = struct{}{}
		files = append(files, p)
	}
	if len(inputs) == 0 {
		_ = filepath.Walk("data", func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			name := info.Name()
			if !strings.HasPrefix(name, "results-") || !strings.HasSuffix(name, ".csv") {
				return nil
			}
			if name == combinedCSVName {
				return nil
			}
			add(path)
			return nil
		})
		sort.Strings(files)
		return files, nil
	}
	for _, arg := range inputs {
		st, err := os.Stat(arg)
		if err == nil && st.IsDir() {
			matches, _ := filepath.Glob(filepath.Join(arg, "results-*.csv"))
			sort.Strings(matches)
			for _, m := range matches {
				add(m)
			}
			continue
		}
		if err == nil && !st.IsDir() {
			add(arg)
			continue
		}
		matches, _ := filepath.Glob(arg)
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && !st.IsDir() {
				add(m)
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

type combineRow struct {
	fields map[string]string
}

func combineRowRank(r combineRow) (verified, github, notFile int) {
	v := strings.ToLower(strings.TrimSpace(r.fields["verified"]))
	if v == "true" || v == "1" || v == "yes" {
		verified = 1
	}
	url := r.fields["repository_url"]
	if strings.Contains(url, "github.com/") {
		github = 1
	}
	if !strings.HasPrefix(url, "file:") {
		notFile = 1
	}
	return
}

func rankBetter(a, b combineRow) bool {
	av, ag, an := combineRowRank(a)
	bv, bg, bn := combineRowRank(b)
	if av != bv {
		return av > bv
	}
	if ag != bg {
		return ag > bg
	}
	return an > bn
}

// combineResultsDedup merges CSVs by secret (same algorithm as scripts/combine-results.sh).
func combineResultsDedup(paths []string, outPath string) (nFiles, totalIn, unique, skippedEmpty int, err error) {
	var header []string
	best := map[string]combineRow{}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		r := csv.NewReader(f)
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		recs, err := r.ReadAll()
		f.Close()
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if len(recs) == 0 {
			continue
		}
		nFiles++
		if header == nil {
			header = recs[0]
		}
		for _, rec := range recs[1:] {
			totalIn++
			row := map[string]string{}
			for i, h := range header {
				if i < len(rec) {
					row[h] = rec[i]
				} else {
					row[h] = ""
				}
			}
			// Also map by position if fieldnames diverged.
			if len(recs[0]) == len(rec) {
				for i, h := range recs[0] {
					if _, ok := row[h]; !ok && i < len(rec) {
						row[h] = rec[i]
					}
				}
			}
			secret := strings.TrimSpace(row["secret"])
			if secret == "" {
				// Try index from this file's header.
				if si := csvColumnIndex(strings.Join(recs[0], ","), "secret"); si >= 0 && si < len(rec) {
					secret = strings.TrimSpace(rec[si])
					row["secret"] = secret
				}
			}
			if secret == "" {
				skippedEmpty++
				continue
			}
			norm := combineRow{fields: map[string]string{}}
			for _, h := range header {
				norm.fields[h] = row[h]
			}
			prev, ok := best[secret]
			if !ok || rankBetter(norm, prev) {
				best[secret] = norm
			}
		}
	}
	if header == nil {
		return 0, 0, 0, 0, fmt.Errorf("no CSV header found")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		return 0, 0, 0, 0, err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer out.Close()
	w := csv.NewWriter(out)
	if err := w.Write(header); err != nil {
		return 0, 0, 0, 0, err
	}
	rows := make([]combineRow, 0, len(best))
	for _, r := range best {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		ui, uj := rows[i].fields["repository_url"], rows[j].fields["repository_url"]
		if ui != uj {
			return ui < uj
		}
		return rows[i].fields["secret"] < rows[j].fields["secret"]
	})
	for _, r := range rows {
		rec := make([]string, len(header))
		for i, h := range header {
			rec[i] = r.fields[h]
		}
		if err := w.Write(rec); err != nil {
			return 0, 0, 0, 0, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return 0, 0, 0, 0, err
	}
	return nFiles, totalIn, len(best), skippedEmpty, nil
}

// filterVerifiedCSV writes only verified=true rows from src to dst.
func filterVerifiedCSV(src, dst string) (int, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil {
		return 0, err
	}
	if len(recs) == 0 {
		return 0, nil
	}
	header := recs[0]
	vi := -1
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), "verified") {
			vi = i
			break
		}
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	w := csv.NewWriter(out)
	_ = w.Write(header)
	n := 0
	for _, rec := range recs[1:] {
		if vi < 0 || vi >= len(rec) {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(rec[vi]))
		if v != "true" && v != "1" && v != "yes" {
			continue
		}
		_ = w.Write(rec)
		n++
	}
	w.Flush()
	return n, w.Error()
}
