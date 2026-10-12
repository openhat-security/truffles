package cli

import (
	"fmt"
	"strings"
)

// loadTermsFile reads one search term per line (# comments and blanks skipped).
// Each term becomes a GitHub query (see termToQuery).
func loadTermsFile(path string) ([]string, error) {
	lines, err := loadPrefixFile(path)
	if err != nil {
		return nil, err
	}
	var queries []string
	for _, line := range lines {
		q, _ := termToQuery(line)
		if q != "" {
			queries = append(queries, q)
		}
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("terms file %q has no usable terms", path)
	}
	return queries, nil
}

// termToQuery maps a user term to a GitHub search query and whether to use code search.
// Lines that already look like GitHub search syntax are passed through.
func termToQuery(term string) (query string, code bool) {
	term = strings.TrimSpace(term)
	if term == "" {
		return "", false
	}
	lower := strings.ToLower(term)
	if strings.Contains(lower, " in:") || strings.HasPrefix(lower, "language:") ||
		strings.Contains(lower, " repo:") || strings.Contains(lower, " user:") {
		code = strings.Contains(lower, "in:file")
		return term, code
	}
	return codeSearchQuery(term), true
}

// mergeTermQueries appends queries from terms, marking code-search ones in codeQueries.
func mergeTermQueries(queries []string, terms []string, codeQueries map[string]bool) []string {
	if codeQueries == nil {
		codeQueries = map[string]bool{}
	}
	seen := map[string]bool{}
	for _, q := range queries {
		seen[q] = true
	}
	for _, t := range terms {
		q, isCode := termToQuery(t)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		if isCode {
			codeQueries[q] = true
		}
		queries = append(queries, q)
	}
	return queries
}
