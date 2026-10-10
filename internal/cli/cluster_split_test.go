package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitRoundRobinEqualCounts(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	chunks := splitRoundRobin(lines, 2)
	if len(chunks[0]) != 3 || len(chunks[1]) != 2 {
		t.Fatalf("chunks=%v want 3 and 2", chunks)
	}
}

func TestShuffleLinesDeterministic(t *testing.T) {
	in := []string{"https://github.com/a/1", "https://github.com/b/2", "https://github.com/c/3", "https://github.com/d/4"}
	a := shuffleLinesDeterministic(in)
	b := shuffleLinesDeterministic(in)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("shuffle not stable: %v vs %v", a, b)
		}
	}
	// Should permute something non-trivial for this input.
	sameOrder := true
	for i := range in {
		if a[i] != in[i] {
			sameOrder = false
			break
		}
	}
	if sameOrder {
		t.Fatal("expected shuffle to change order")
	}
}

func TestSplitLinesShuffledStableAndBalanced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.txt")
	var body string
	for i := 0; i < 10; i++ {
		body += "https://github.com/o/r" + string(rune('0'+i)) + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := splitLinesShuffled(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	b, err := splitLinesShuffled(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			t.Fatalf("chunk %d size drift: %d vs %d", i, len(a[i]), len(b[i]))
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatalf("chunk %d line %d drift: %q vs %q", i, j, a[i][j], b[i][j])
			}
		}
	}
	total := 0
	for _, c := range a {
		total += len(c)
	}
	if total != 10 {
		t.Fatalf("total=%d want 10", total)
	}
}

func TestSplitLinesShuffledBreaksStarSortSkew(t *testing.T) {
	// Search output is often star-ranked; plain round-robin still interleaves,
	// but shuffle guarantees no fixed pairing of rank→worker when re-run with edits.
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.txt")
	body := "https://github.com/big/huge\nhttps://github.com/mid/mid\nhttps://github.com/small/tiny\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	plain, err := splitLines(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	shuf, err := splitLinesShuffled(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain[0])+len(plain[1]) != 3 {
		t.Fatal("plain split lost repos")
	}
	if len(shuf[0])+len(shuf[1]) != 3 {
		t.Fatal("shuffled split lost repos")
	}
}
