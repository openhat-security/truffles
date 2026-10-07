package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeDataDir(t *testing.T) {
	cases := []struct {
		root, in, want string
	}{
		{"data", "", filepath.Join("data", "remote-results")},
		{"data", "remote-results", filepath.Join("data", "remote-results")},
		{"data", "./remote-results", filepath.Join("data", "remote-results")},
		{"data", "data/remote-results", filepath.Join("data", "remote-results")},
		{"outdir", "scan-a", filepath.Join("outdir", "scan-a")},
		{"outdir", "outdir/scan-a", filepath.Join("outdir", "scan-a")},
		{"data", "/tmp/foo", filepath.Join("data", "foo")},
	}
	for _, c := range cases {
		if got := normalizeDataDir(c.root, c.in); got != c.want {
			t.Errorf("normalizeDataDir(%q,%q) = %q, want %q", c.root, c.in, got, c.want)
		}
	}
}

func TestUniqueCollectDirSuffixes(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	a, err := uniqueCollectDir("data", "remote-results")
	if err != nil {
		t.Fatal(err)
	}
	if a != filepath.Join("data", "remote-results") {
		t.Fatalf("first = %q", a)
	}
	b, err := uniqueCollectDir("data", "remote-results")
	if err != nil {
		t.Fatal(err)
	}
	if b != filepath.Join("data", "remote-results-1") {
		t.Fatalf("second = %q", b)
	}
	c, err := uniqueCollectDir("data", "remote-results")
	if err != nil {
		t.Fatal(err)
	}
	if c != filepath.Join("data", "remote-results-2") {
		t.Fatalf("third = %q", c)
	}
}

func TestResolveRunLayoutNonInteractive(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	lay, err := resolveRunLayout(runLayoutOpts{
		YAMLDataDir: "outdir",
		YAMLOut:     "my-scan",
		Prompt:      false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lay.CollectDir != filepath.Join("outdir", "my-scan") {
		t.Fatalf("got %q", lay.CollectDir)
	}
}
