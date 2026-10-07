package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCombineResultCSVs(t *testing.T) {
	dir := t.TempDir()
	h := "detector,verified,secret"
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("results-worker1.csv", h+"\na,true,s1\nb,false,s2\n")
	write("results-worker2.csv", h+"\nc,true,s3\n")

	path, rows, err := combineResultCSVs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "results.csv") {
		t.Fatalf("path = %q", path)
	}
	if rows != 3 {
		t.Fatalf("rows = %d", rows)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, h+"\n") {
		t.Fatalf("missing header: %q", got)
	}
	if strings.Count(got, h) != 1 {
		t.Fatalf("header duplicated: %q", got)
	}
	for _, want := range []string{"a,true,s1", "b,false,s2", "c,true,s3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	// Individuals kept.
	if _, err := os.Stat(filepath.Join(dir, "results-worker1.csv")); err != nil {
		t.Fatal(err)
	}
}
