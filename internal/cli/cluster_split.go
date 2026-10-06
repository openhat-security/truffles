package cli

import (
	"io/ioutil"
	"strings"
)

func splitByOwner(lines []string, n int) [][]string {
	chunks := make([][]string, n)
	for _, l := range lines {
		lt := strings.TrimSpace(l)
		if lt == "" || strings.HasPrefix(lt, "#") {
			continue
		}
		owner := ""
		for _, prefix := range []string{"https://github.com/", "http://github.com/"} {
			if strings.HasPrefix(lt, prefix) {
				rest := strings.TrimPrefix(lt, prefix)
				parts := strings.Split(rest, "/")
				if len(parts) > 0 {
					owner = parts[0]
				}
				break
			}
		}
		if owner == "" {
			owner = lt
		}
		h := 0
		for _, r := range owner {
			h += int(r)
		}
		idx := h % n
		if idx < 0 {
			idx = -idx
		}
		chunks[idx] = append(chunks[idx], lt)
	}
	return chunks
}

func splitLinesHash(path string, n int) ([][]string, error) {
	if n <= 0 {
		n = 1
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	var nonempty []string
	for _, l := range lines {
		lt := strings.TrimSpace(l)
		if lt == "" || strings.HasPrefix(lt, "#") {
			continue
		}
		nonempty = append(nonempty, lt)
	}
	if len(nonempty) == 0 {
		return [][]string{nonempty}, nil
	}
	chunks := make([][]string, n)
	for i, l := range nonempty {
		_ = i
		h := 0
		for _, r := range l {
			h += int(r)
			h *= 131542391
		}
		idx := h % n
		if idx < 0 {
			idx = -idx
		}
		chunks[idx] = append(chunks[idx], l)
	}
	return chunks, nil
}
