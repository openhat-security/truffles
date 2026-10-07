package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRepoURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/A/B", "github.com/a/b"},
		{"http://github.com/a/b/", "github.com/a/b"},
		{"https://github.com/a/b.git", "github.com/a/b"},
	}
	for _, tc := range cases {
		if got := normalizeRepoURL(tc.in); got != tc.want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFilterSkipped(t *testing.T) {
	skip := map[string]struct{}{
		normalizeRepoURL("https://github.com/a/b"): {},
	}
	keep, skipped := filterSkipped([]string{
		"https://github.com/a/b.git",
		"https://github.com/c/d",
	}, skip)
	if len(keep) != 1 || keep[0] != "https://github.com/c/d" {
		t.Fatalf("keep = %v", keep)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestLoadSkipSetMissingIsEmpty(t *testing.T) {
	set, err := loadSkipSet(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 0 {
		t.Fatalf("want empty set, got %v", set)
	}
}

func TestScannedAppenderAndMerge(t *testing.T) {
	dir := t.TempDir()
	master := filepath.Join(dir, "master.txt")
	delta := filepath.Join(dir, "delta.txt")
	if err := os.WriteFile(delta, []byte("https://github.com/a/b\nhttps://github.com/c/d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := mergeScannedFiles(master, []string{delta})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("added = %d, want 2", n)
	}
	// Second merge should add nothing.
	n, err = mergeScannedFiles(master, []string{delta})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second merge added %d, want 0", n)
	}
}

func TestDualReportPaths(t *testing.T) {
	p, c, j := dualReportPaths("out/results-worker.csv", "csv")
	if p != "out/results-worker.txt" || c != "out/results-worker.csv" || j != "" {
		t.Fatalf("csv dual = %q %q %q", p, c, j)
	}
	p, c, j = dualReportPaths("out/report.txt", "pretty")
	if p != "out/report.txt" || c != "out/report.csv" || j != "" {
		t.Fatalf("pretty dual = %q %q %q", p, c, j)
	}
}
