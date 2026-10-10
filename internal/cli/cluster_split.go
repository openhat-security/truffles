package cli

import (
	"hash/fnv"
	"io/ioutil"
	"math/rand"
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

// readRepoLines loads non-empty, non-comment lines from a repo list file.
func readRepoLines(path string) ([]string, error) {
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
	return nonempty, nil
}

// splitRoundRobin assigns lines to n chunks in interleaved order (equal counts ±1).
func splitRoundRobin(lines []string, n int) [][]string {
	if n <= 0 {
		n = 1
	}
	if len(lines) == 0 {
		return [][]string{lines}
	}
	chunks := make([][]string, n)
	for i, l := range lines {
		chunks[i%n] = append(chunks[i%n], l)
	}
	return chunks
}

// shuffleLinesDeterministic permutes lines with a seed derived from their content
// so the same list always maps to the same shuffle (stable across re-runs).
func shuffleLinesDeterministic(lines []string) []string {
	if len(lines) <= 1 {
		return append([]string(nil), lines...)
	}
	h := fnv.New64a()
	for _, l := range lines {
		_, _ = h.Write([]byte(l))
		_, _ = h.Write([]byte{'\n'})
	}
	r := rand.New(rand.NewSource(int64(h.Sum64())))
	out := append([]string(nil), lines...)
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// splitLinesShuffled shuffles the repo list (deterministic), then round-robins
// across workers so star-sorted search output does not stick one worker with
// every heavy clone.
func splitLinesShuffled(path string, n int) ([][]string, error) {
	lines, err := readRepoLines(path)
	if err != nil {
		return nil, err
	}
	lines = shuffleLinesDeterministic(lines)
	return splitRoundRobin(lines, n), nil
}

func splitLinesHash(path string, n int) ([][]string, error) {
	if n <= 0 {
		n = 1
	}
	lines, err := readRepoLines(path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return [][]string{lines}, nil
	}
	chunks := make([][]string, n)
	for _, l := range lines {
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
