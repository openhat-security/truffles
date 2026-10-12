package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebuildCleanRepos(t *testing.T) {
	data := t.TempDir()
	run := filepath.Join(data, "remote-results-1")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	csvPath := filepath.Join(run, "results-worker1.csv")
	if err := os.WriteFile(csvPath, []byte(
		"repository_url,verified,secret\n"+
			"https://github.com/clean/repo,false,sk-x\n"+
			"https://github.com/dirty/repo,true,sk-y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(data, "clean-repos.txt")
	n, added, err := rebuildCleanRepos(cleanRebuildOpts{
		Inputs:  []string{run},
		Out:     out,
		DataDir: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 || added < 1 {
		t.Fatalf("n=%d added=%d", n, added)
	}
	body, _ := os.ReadFile(out)
	s := string(body)
	if !strings.Contains(s, "https://github.com/clean/repo") {
		t.Fatalf("missing clean repo: %s", s)
	}
	if strings.Contains(s, "dirty/repo") {
		t.Fatalf("dirty repo should be excluded: %s", s)
	}
}

func TestParseGitHubURL(t *testing.T) {
	if g := parseGitHubURL("https://github.com/o/r.git"); g != "https://github.com/o/r" {
		t.Fatalf("got %q", g)
	}
	if g := parseGitHubURL("file:///tmp/x"); g != "" {
		t.Fatalf("file url should be empty: %q", g)
	}
}
