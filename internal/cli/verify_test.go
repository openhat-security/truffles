package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedCSVPath(t *testing.T) {
	got := verifiedCSVPath("/tmp/results-combined.csv")
	want := "/tmp/results-combined-verified.csv"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGithubRepoURL(t *testing.T) {
	if githubRepoURL("file:///tmp/x") != "" {
		t.Fatal("file:// should be empty")
	}
	if got := githubRepoURL("https://github.com/o/r.git"); got != "https://github.com/o/r" {
		t.Fatalf("got %q", got)
	}
	if got := githubRepoURL("o/r"); got != "https://github.com/o/r" {
		t.Fatalf("short %q", got)
	}
}

func TestLoadSecretRepoMap(t *testing.T) {
	dir := t.TempDir()
	body := `
[++] acme/widget  2 finding(s) in 1s  all unverified

  UNVERIFIED OpenAI
      secret     sk-proj-TESTSECRET123
      file       .env:1
`
	if err := os.WriteFile(filepath.Join(dir, "results-worker1.txt"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	m := loadSecretRepoMap(dir)
	if got := m["sk-proj-TESTSECRET123"]; got != "https://github.com/acme/widget" {
		t.Fatalf("map=%v got %q", m, got)
	}
}
