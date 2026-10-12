package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var githubURLRe = regexp.MustCompile(`(?i)^https?://github\.com/[^/\s]+/[^/\s]+`)

func normGitHubKey(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimRight(u, "/")
	if strings.HasSuffix(strings.ToLower(u), ".git") {
		u = u[:len(u)-4]
	}
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(strings.ToLower(u), p) {
			u = u[len(p):]
			break
		}
	}
	return strings.ToLower(u)
}

func parseGitHubURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "file:") {
		return ""
	}
	if strings.Count(raw, "/") == 1 && !strings.Contains(raw, "://") {
		raw = "https://github.com/" + raw
	}
	m := githubURLRe.FindString(raw)
	if m == "" {
		return ""
	}
	m = strings.TrimRight(m, "/")
	if strings.HasSuffix(strings.ToLower(m), ".git") {
		m = m[:len(m)-4]
	}
	if strings.HasPrefix(strings.ToLower(m), "http://") {
		m = "https://" + m[len("http://"):]
	}
	return m
}

func loadURLFileMap(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return out
	}
	if strings.HasPrefix(text, "{") {
		var raw map[string]interface{}
		if json.Unmarshal(b, &raw) != nil {
			return out
		}
		for k, v := range raw {
			url := ""
			if m, ok := v.(map[string]interface{}); ok {
				if u, ok := m["url"].(string); ok {
					url = u
				}
			}
			if url == "" {
				if strings.Contains(k, "://") {
					url = k
				} else {
					url = "https://" + k
				}
			}
			g := parseGitHubURL(url)
			if g == "" {
				g = parseGitHubURL("https://" + k)
			}
			if g != "" {
				out[normGitHubKey(g)] = g
			}
		}
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if g := parseGitHubURL(line); g != "" {
			out[normGitHubKey(g)] = g
		}
	}
	return out
}

type cleanRebuildOpts struct {
	Inputs         []string
	Out            string
	IncludeScanned bool
	DryRun         bool
	DataDir        string
}

// rebuildCleanRepos builds clean-repos.txt from results CSVs (update-clean-repos.py).
func rebuildCleanRepos(o cleanRebuildOpts) (cleanTotal, added int, err error) {
	dataDir := o.DataDir
	if dataDir == "" {
		dataDir = "data"
	}
	outPath := o.Out
	if outPath == "" {
		outPath = filepath.Join(dataDir, "clean-repos.txt")
	}

	candidates, verifiedFiles, err := findResultsCSVs(o.Inputs, dataDir)
	if err != nil {
		return 0, 0, err
	}
	if len(candidates) == 0 && !o.IncludeScanned {
		return 0, 0, fmt.Errorf("no results*.csv files found")
	}

	verified := map[string]struct{}{}
	for _, p := range verifiedFiles {
		for k := range reposFromCSV(p) {
			verified[k] = struct{}{}
		}
	}

	dirtyFromRows := map[string]struct{}{}
	found := map[string]string{}
	csvKeys := map[string]struct{}{}

	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		r := csv.NewReader(f)
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		recs, err := r.ReadAll()
		f.Close()
		if err != nil || len(recs) == 0 {
			continue
		}
		header := recs[0]
		urlIdx, verIdx := -1, -1
		for i, h := range header {
			switch strings.ToLower(strings.TrimSpace(h)) {
			case "repository_url":
				urlIdx = i
			case "verified":
				verIdx = i
			}
		}
		for _, rec := range recs[1:] {
			if urlIdx < 0 || urlIdx >= len(rec) {
				continue
			}
			g := parseGitHubURL(rec[urlIdx])
			if g == "" {
				continue
			}
			k := normGitHubKey(g)
			csvKeys[k] = struct{}{}
			found[k] = g
			if verIdx >= 0 && verIdx < len(rec) {
				v := strings.ToLower(strings.TrimSpace(rec[verIdx]))
				if v == "true" || v == "1" || v == "yes" {
					dirtyFromRows[k] = struct{}{}
				}
			}
		}
	}

	dirty := map[string]struct{}{}
	for k := range verified {
		dirty[k] = struct{}{}
	}
	for k := range dirtyFromRows {
		dirty[k] = struct{}{}
	}

	clean := map[string]string{}
	for _, prior := range []string{outPath, filepath.Join(dataDir, "clean-repos.txt"), filepath.Join(dataDir, "clean-repos.json")} {
		for k, v := range loadURLFileMap(prior) {
			clean[k] = v
		}
	}
	if o.IncludeScanned {
		scannedPaths := []string{filepath.Join(dataDir, "scanned-repos.txt")}
		_ = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			name := info.Name()
			if strings.HasPrefix(name, "scanned-") && strings.HasSuffix(name, ".txt") {
				scannedPaths = append(scannedPaths, path)
			}
			return nil
		})
		for _, p := range scannedPaths {
			for k, v := range loadURLFileMap(p) {
				clean[k] = v
			}
		}
	}

	for k := range csvKeys {
		if _, bad := dirty[k]; bad {
			continue
		}
		if _, ok := clean[k]; !ok {
			added++
		}
		if u := found[k]; u != "" {
			clean[k] = u
		} else if clean[k] == "" {
			clean[k] = "https://" + k
		}
	}
	for k := range dirty {
		delete(clean, k)
	}

	urls := make([]string, 0, len(clean))
	for _, u := range clean {
		urls = append(urls, u)
	}
	sort.Slice(urls, func(i, j int) bool {
		return normGitHubKey(urls[i]) < normGitHubKey(urls[j])
	})

	fmt.Fprintf(os.Stderr, "csvs=%d verified_csvs=%d repos_in_csv=%d dirty=%d clean_total=%d (+%d new from csv)\n",
		len(candidates), len(verifiedFiles), len(csvKeys), len(dirty), len(urls), added)

	if o.DryRun {
		return len(urls), added, nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		return 0, 0, err
	}
	tmp := outPath + ".tmp"
	var b strings.Builder
	for _, u := range urls {
		b.WriteString(u)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return 0, 0, err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return 0, 0, err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d urls)\n", outPath, len(urls))
	return len(urls), added, nil
}

func reposFromCSV(path string) map[string]struct{} {
	out := map[string]struct{}{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil || len(recs) == 0 {
		return out
	}
	urlIdx := -1
	for i, h := range recs[0] {
		if strings.EqualFold(strings.TrimSpace(h), "repository_url") {
			urlIdx = i
			break
		}
	}
	if urlIdx < 0 {
		return out
	}
	for _, rec := range recs[1:] {
		if urlIdx >= len(rec) {
			continue
		}
		if g := parseGitHubURL(rec[urlIdx]); g != "" {
			out[normGitHubKey(g)] = struct{}{}
		}
	}
	return out
}

func findResultsCSVs(inputs []string, dataDir string) (candidates, verified []string, err error) {
	addFile := func(p string) {
		name := strings.ToLower(filepath.Base(p))
		if !strings.HasSuffix(name, ".csv") {
			return
		}
		if strings.Contains(name, "verified") {
			verified = append(verified, p)
			return
		}
		if strings.HasPrefix(name, "results") || name == "results.csv" {
			candidates = append(candidates, p)
		}
	}
	if len(inputs) == 0 {
		_ = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			addFile(path)
			return nil
		})
		sort.Strings(candidates)
		sort.Strings(verified)
		return candidates, verified, nil
	}
	for _, arg := range inputs {
		st, err := os.Stat(arg)
		if err == nil && st.IsDir() {
			_ = filepath.Walk(arg, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				addFile(path)
				return nil
			})
			continue
		}
		if err == nil && !st.IsDir() {
			addFile(arg)
			continue
		}
		matches, _ := filepath.Glob(arg)
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil {
				continue
			}
			if st.IsDir() {
				_ = filepath.Walk(m, func(path string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return nil
					}
					addFile(path)
					return nil
				})
			} else {
				addFile(m)
			}
		}
	}
	sort.Strings(candidates)
	sort.Strings(verified)
	return candidates, verified, nil
}

func runCleanRebuild(args []string) error {
	includeScanned := false
	dryRun := false
	out := "data/clean-repos.txt"
	var inputs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprint(os.Stderr, `truffles clean rebuild — rebuild clean-repos.txt from results CSVs

usage:
  truffles clean rebuild [dirs...] [-o data/clean-repos.txt] [--include-scanned]
`)
			return nil
		case a == "-o" || a == "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			out = args[i+1]
			i++
		case a == "--include-scanned":
			includeScanned = true
		case a == "--dry-run":
			dryRun = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %s", a)
		default:
			inputs = append(inputs, a)
		}
	}
	_, _, err := rebuildCleanRepos(cleanRebuildOpts{
		Inputs:         inputs,
		Out:            out,
		IncludeScanned: includeScanned,
		DryRun:         dryRun,
	})
	return err
}
