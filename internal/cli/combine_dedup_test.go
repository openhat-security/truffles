package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCombineResultsDedupPrefersVerified(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "results-worker1.csv")
	b := filepath.Join(dir, "results-worker2.csv")
	header := "repository_url,verified,detector,secret\n"
	if err := os.WriteFile(a, []byte(header+
		"https://github.com/o/r,false,OpenAI,sk-aaa\n"+
		"file:///tmp/x,false,OpenAI,sk-bbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(header+
		"https://github.com/o/r,true,OpenAI,sk-aaa\n"+
		"https://github.com/o/r2,false,OpenAI,sk-bbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.csv")
	_, total, unique, _, err := combineResultsDedup([]string{a, b}, out)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || unique != 2 {
		t.Fatalf("total=%d unique=%d", total, unique)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "sk-aaa") || !strings.Contains(s, "true") {
		t.Fatalf("expected verified sk-aaa kept: %s", s)
	}
	if strings.Count(s, "sk-aaa") != 1 {
		t.Fatalf("sk-aaa should appear once: %s", s)
	}
	// sk-bbb: prefer github URL over file://
	if !strings.Contains(s, "https://github.com/o/r2") {
		t.Fatalf("expected github URL for sk-bbb: %s", s)
	}
}

func TestFilterVerifiedCSV(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "all.csv")
	dst := filepath.Join(dir, "ver.csv")
	if err := os.WriteFile(src, []byte(
		"repository_url,verified,secret\n"+
			"https://github.com/a/b,true,sk-1\n"+
			"https://github.com/c/d,false,sk-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := filterVerifiedCSV(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
}
