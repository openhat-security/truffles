package proxy

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadProxyFile reads host:port lines (# comments and blanks skipped).
// Optional prefix "trusted:" on a line skips the GitHub probe and admits
// the proxy directly into the live set (use for paid/static proxies you control).
func LoadProxyFile(path string) (candidates []string, trusted []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		trust := false
		if strings.HasPrefix(strings.ToLower(line), "trusted:") {
			trust = true
			line = strings.TrimSpace(line[len("trusted:"):])
		}
		if !validAddr(line) {
			return nil, nil, fmt.Errorf("invalid proxy address %q in %s", line, path)
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		if trust {
			trusted = append(trusted, line)
		} else {
			candidates = append(candidates, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if len(candidates) == 0 && len(trusted) == 0 {
		return nil, nil, fmt.Errorf("proxy file %q has no addresses", path)
	}
	return candidates, trusted, nil
}
