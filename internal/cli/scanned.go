package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// normalizeRepoURL collapses common URL variants so skip-list matches are stable
// across http/https, trailing slashes, and .git suffixes.
func normalizeRepoURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.ToLower(u)
}

// loadSkipSet reads a newline-delimited repo list. Missing files are empty sets
// so a fresh master file does not fail the first run.
func loadSkipSet(path string) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	if path == "" {
		return set, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return set, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[normalizeRepoURL(line)] = struct{}{}
	}
	return set, sc.Err()
}

// filterSkipped drops URLs present in skip (by normalized form).
func filterSkipped(urls []string, skip map[string]struct{}) (keep, skipped []string) {
	if len(skip) == 0 {
		return urls, nil
	}
	for _, u := range urls {
		if _, hit := skip[normalizeRepoURL(u)]; hit {
			skipped = append(skipped, u)
			continue
		}
		keep = append(keep, u)
	}
	return keep, skipped
}

// scannedAppender appends successfully scanned repo URLs to a master file.
type scannedAppender struct {
	path string
	mu   sync.Mutex
	seen map[string]struct{}
}

func newScannedAppender(path string) *scannedAppender {
	if path == "" {
		return nil
	}
	return &scannedAppender{path: path, seen: make(map[string]struct{})}
}

// Seed marks URLs already in the skip set so Add will not rewrite them.
func (a *scannedAppender) Seed(skip map[string]struct{}) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range skip {
		a.seen[k] = struct{}{}
	}
}

// Add writes one URL line. Failed scans must not call this.
func (a *scannedAppender) Add(url string) error {
	if a == nil {
		return nil
	}
	key := normalizeRepoURL(url)
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.seen[key]; ok {
		return nil
	}
	if dir := filepath.Dir(a.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintln(f, url)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	a.seen[key] = struct{}{}
	return nil
}

// mergeScannedFiles appends unique URLs from deltas into master (creating it).
func mergeScannedFiles(master string, deltas []string) (added int, err error) {
	if master == "" {
		return 0, nil
	}
	skip, err := loadSkipSet(master)
	if err != nil {
		return 0, err
	}
	app := newScannedAppender(master)
	app.Seed(skip)
	for _, d := range deltas {
		urls, err := readURLs(d)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return added, err
		}
		for _, u := range urls {
			before := len(app.seen)
			if err := app.Add(u); err != nil {
				return added, err
			}
			if len(app.seen) > before {
				added++
			}
		}
	}
	return added, nil
}
