package disclose

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ParseCSVFiles reads one or more results CSV files (or a directory of results*.csv).
func ParseCSVFiles(paths ...string) ([]Finding, error) {
	var files []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			// Prefer combined results.csv (from cluster collect) so worker
			// files are not double-counted alongside it.
			combined := filepath.Join(p, "results.csv")
			if cst, err := os.Stat(combined); err == nil && !cst.IsDir() {
				files = append(files, combined)
				continue
			}
			matches, err := filepath.Glob(filepath.Join(p, "results-*.csv"))
			if err != nil {
				return nil, err
			}
			files = append(files, matches...)
			continue
		}
		files = append(files, p)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no results*.csv files found")
	}
	var all []Finding
	for _, f := range files {
		rows, err := parseCSVFile(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		all = append(all, rows...)
	}
	return all, nil
}

func parseCSVFile(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseCSV(f)
}

// ParseCSV reads truffles CSV columns from r.
func ParseCSV(r io.Reader) ([]Finding, error) {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	required := []string{"detector", "verified", "secret", "repository_url"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("csv missing column %q (got %v)", col, header)
		}
	}

	var out []Finding
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(name string) string {
			i, ok := idx[name]
			if !ok || i >= len(rec) {
				return ""
			}
			return rec[i]
		}
		repoURL := strings.TrimSpace(get("repository_url"))
		owner, repo, ok := ParseGitHubRepo(repoURL)
		if !ok {
			continue
		}
		verified, _ := strconv.ParseBool(strings.TrimSpace(get("verified")))
		out = append(out, Finding{
			Detector:  get("detector"),
			Verified:  verified,
			Timestamp: get("timestamp"),
			Secret:    get("secret"),
			RepoURL:   normalizeRepoURL(repoURL),
			FileLine:  get("file_line"),
			Commit:    get("commit"),
			Author:    get("author"),
			Decoder:   get("decoder"),
			VerifyErr: get("verification_error"),
			Owner:     owner,
			Repo:      repo,
		})
	}
	return out, nil
}

// GroupByRepo aggregates findings; verifiedOnly drops unverified rows.
func GroupByRepo(findings []Finding, verifiedOnly bool) []RepoReport {
	m := map[string]*RepoReport{}
	order := []string{}
	for _, f := range findings {
		if verifiedOnly && !f.Verified {
			continue
		}
		key := f.Owner + "/" + f.Repo
		rr, ok := m[key]
		if !ok {
			rr = &RepoReport{
				Owner:   f.Owner,
				Repo:    f.Repo,
				RepoURL: f.RepoURL,
			}
			m[key] = rr
			order = append(order, key)
		}
		rr.Findings = append(rr.Findings, f)
	}
	sort.Strings(order)
	out := make([]RepoReport, 0, len(order))
	for _, k := range order {
		out = append(out, *m[k])
	}
	return out
}

// ParseGitHubRepo extracts owner/repo from a github.com URL.
func ParseGitHubRepo(raw string) (owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		rest := strings.TrimPrefix(raw, "git@github.com:")
		rest = strings.TrimSuffix(rest, ".git")
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], true
		}
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

func normalizeRepoURL(raw string) string {
	owner, repo, ok := ParseGitHubRepo(raw)
	if !ok {
		return raw
	}
	return "https://github.com/" + owner + "/" + repo
}
