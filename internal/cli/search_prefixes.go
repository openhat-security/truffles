package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadPrefixFile reads one prefix per line. Blank lines and # comments are skipped.
func loadPrefixFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("prefix file %q has no prefixes", path)
	}
	return out, nil
}

// codeSearchQuery turns a raw prefix into a GitHub code search query.
func codeSearchQuery(prefix string) string {
	return fmt.Sprintf("%q in:file", prefix)
}

// mergePrefixQueries appends code-search queries for each prefix, skipping duplicates.
func mergePrefixQueries(queries []string, prefixes []string) []string {
	seen := map[string]bool{}
	for _, q := range queries {
		seen[q] = true
	}
	for _, p := range prefixes {
		q := codeSearchQuery(p)
		if seen[q] {
			continue
		}
		seen[q] = true
		queries = append(queries, q)
	}
	return queries
}

// codeQuerySet builds a set of queries that should use the code search API.
func codeQuerySet(prefixes []string) map[string]bool {
	set := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		set[codeSearchQuery(p)] = true
	}
	return set
}
