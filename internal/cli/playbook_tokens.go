package cli

import (
	"strings"
	"sync"
)

// searchAuthTokens merges search.token and search.tokens after env expansion.
// Each value may be comma-separated. Empty entries are dropped; order is kept.
func searchAuthTokens(sc SearchConfig) []string {
	var parts []string
	if sc.Token != "" {
		parts = append(parts, sc.Token)
	}
	parts = append(parts, sc.Tokens...)
	return splitAuthTokens(parts...)
}

// scanAuthTokens merges scan.token and scan.tokens after env expansion.
func scanAuthTokens(sc ScanConfig) []string {
	var parts []string
	if sc.Token != "" {
		parts = append(parts, sc.Token)
	}
	parts = append(parts, sc.Tokens...)
	return splitAuthTokens(parts...)
}

// splitAuthTokens flattens comma-separated token strings into a de-duplicated list.
func splitAuthTokens(parts ...string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, part := range parts {
		for _, t := range strings.Split(part, ",") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// tokenRing hands out GitHub tokens round-robin for concurrent page fetches.
type tokenRing struct {
	mu   sync.Mutex
	toks []string
	i    int
}

func newTokenRing(toks []string) *tokenRing {
	cp := append([]string(nil), toks...)
	return &tokenRing{toks: cp}
}

func (r *tokenRing) next() string {
	if r == nil || len(r.toks) == 0 {
		return ""
	}
	r.mu.Lock()
	t := r.toks[r.i%len(r.toks)]
	r.i++
	r.mu.Unlock()
	return t
}
