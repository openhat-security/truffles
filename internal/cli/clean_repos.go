package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// cleanEntry is one repo known to have been scanned with zero verified secrets.
type cleanEntry struct {
	URL string `json:"url"`
	At  string `json:"at,omitempty"`
}

// cleanStore is an on-disk set (normalized repo URL → entry) of repos that
// finished a verifying scan with no verified findings. Used as an extra skip
// source so later search/scan runs do not revisit them.
//
// File formats: .txt = one URL per line (preferred, fastest); .json = object map.
type cleanStore struct {
	path   string
	mu     sync.Mutex
	m      map[string]cleanEntry
	loaded bool
}

func newCleanStore(path string) *cleanStore {
	if path == "" {
		return nil
	}
	return &cleanStore{path: path}
}

func (c *cleanStore) ensureLocked() error {
	if c.loaded {
		return nil
	}
	m, err := loadCleanMap(c.path)
	if err != nil {
		return err
	}
	c.m = m
	c.loaded = true
	return nil
}

func cleanPathIsJSON(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".json")
}

// loadCleanMap reads path into a normalized-key map. Missing file → empty.
// Accepts newline URL lists or a JSON object map (auto-detected).
func loadCleanMap(path string) (map[string]cleanEntry, error) {
	out := map[string]cleanEntry{}
	if path == "" {
		return out, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return out, nil
	}
	trim := strings.TrimSpace(string(b))
	if len(trim) > 0 && trim[0] == '{' {
		raw := map[string]cleanEntry{}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("clean-file %s: %w", path, err)
		}
		for k, e := range raw {
			url := e.URL
			if url == "" {
				url = k
			}
			key := normalizeRepoURL(url)
			if key == "" {
				continue
			}
			e.URL = url
			out[key] = e
		}
		return out, nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := normalizeRepoURL(line)
		if key == "" {
			continue
		}
		url := line
		if !strings.Contains(line, "://") {
			url = "https://" + key
		}
		out[key] = cleanEntry{URL: url}
	}
	return out, nil
}

// unionCleanIntoSkip adds every clean-map key into skip.
func unionCleanIntoSkip(skip map[string]struct{}, cleanPath string) (int, error) {
	m, err := loadCleanMap(cleanPath)
	if err != nil {
		return 0, err
	}
	n := 0
	for k := range m {
		if _, ok := skip[k]; ok {
			continue
		}
		skip[k] = struct{}{}
		n++
	}
	return n, nil
}

func writeCleanMap(path string, m map[string]cleanEntry) error {
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	var b []byte
	if cleanPathIsJSON(path) {
		// Compact JSON object (no indent) — smaller than pretty-printed.
		var err error
		b, err = json.Marshal(m)
		if err != nil {
			return err
		}
		b = append(b, '\n')
	} else {
		// One URL per line, sorted — smallest + fastest to load for skip sets.
		urls := make([]string, 0, len(m))
		for _, e := range m {
			u := e.URL
			if u == "" {
				continue
			}
			urls = append(urls, u)
		}
		sort.Strings(urls)
		var sb strings.Builder
		for _, u := range urls {
			sb.WriteString(u)
			sb.WriteByte('\n')
		}
		b = []byte(sb.String())
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MarkClean records url as scanned with no verified secrets.
func (c *cleanStore) MarkClean(url string) error {
	if c == nil {
		return nil
	}
	key := normalizeRepoURL(url)
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return err
	}
	c.m[key] = cleanEntry{URL: url, At: time.Now().UTC().Format(time.RFC3339)}
	return writeCleanMap(c.path, c.m)
}

// MarkDirty removes url from the clean map (repo had at least one verified secret).
func (c *cleanStore) MarkDirty(url string) error {
	if c == nil {
		return nil
	}
	key := normalizeRepoURL(url)
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return err
	}
	if _, ok := c.m[key]; !ok {
		return nil
	}
	delete(c.m, key)
	return writeCleanMap(c.path, c.m)
}

// mergeCleanFiles unions delta JSON clean maps into master.
func mergeCleanFiles(master string, deltas []string) (added int, err error) {
	if master == "" {
		return 0, nil
	}
	m, err := loadCleanMap(master)
	if err != nil {
		return 0, err
	}
	before := len(m)
	for _, d := range deltas {
		dm, err := loadCleanMap(d)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return added, err
		}
		for k, e := range dm {
			if _, ok := m[k]; ok {
				continue
			}
			if e.URL == "" {
				e.URL = "https://" + k
			}
			m[k] = e
		}
	}
	if err := writeCleanMap(master, m); err != nil {
		return 0, err
	}
	added = len(m) - before
	if added > 0 {
		CoreLogCleanMerge(master, added, "cluster-collect")
	}
	return added, nil
}

// seedCleanReposFromScanned marks every scanned URL not in verifiedRepos as clean.
func seedCleanReposFromScanned(cleanPath, scannedPath string, verifiedRepos map[string]struct{}) (int, error) {
	if cleanPath == "" || scannedPath == "" {
		return 0, nil
	}
	// Prefer original URL lines from the scanned file for the stored url field.
	urls, err := readURLs(scannedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	m, err := loadCleanMap(cleanPath)
	if err != nil {
		return 0, err
	}
	before := len(m)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, u := range urls {
		key := normalizeRepoURL(u)
		if key == "" {
			continue
		}
		if _, hit := verifiedRepos[key]; hit {
			continue
		}
		if _, ok := m[key]; ok {
			continue
		}
		m[key] = cleanEntry{URL: u, At: now}
	}
	if err := writeCleanMap(cleanPath, m); err != nil {
		return 0, err
	}
	return len(m) - before, nil
}

// verifiedReposFromCSV returns normalized repository_url keys marked verified=true
// (or all rows if the verified column is absent — treat whole file as verified-only).
func verifiedReposFromCSV(path string) (map[string]struct{}, error) {
	rows, header, err := readResultsCSV(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	hasVerified := false
	for _, h := range header {
		if h == "verified" {
			hasVerified = true
			break
		}
	}
	out := map[string]struct{}{}
	for _, row := range rows {
		if hasVerified {
			v := strings.ToLower(strings.TrimSpace(row["verified"]))
			if v != "true" && v != "1" && v != "yes" {
				continue
			}
		}
		u := githubRepoURL(row["repository_url"])
		if u == "" {
			continue
		}
		key := normalizeRepoURL(u)
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out, nil
}
