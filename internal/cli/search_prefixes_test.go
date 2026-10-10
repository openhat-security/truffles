package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodeSearchQuery(t *testing.T) {
	got := codeSearchQuery(`ghp_`)
	want := `"ghp_" in:file`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadPrefixFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefixes.txt")
	if err := os.WriteFile(path, []byte("# comment\nghp_\n\nsk-\nghp_\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := loadPrefixFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "ghp_" || got[1] != "sk-" {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadPrefixFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrefixFile(path); err == nil {
		t.Fatal("expected error for empty prefix file")
	}
}

func TestMergePrefixQueriesDedup(t *testing.T) {
	prefixes := []string{"ghp_", "sk-"}
	existing := []string{codeSearchQuery("ghp_"), "llm"}
	got := mergePrefixQueries(existing, prefixes)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %#v", len(got), got)
	}
	set := map[string]bool{}
	for _, q := range got {
		if set[q] {
			t.Fatalf("duplicate query %q", q)
		}
		set[q] = true
	}
}

func TestPlaybookHasSearchPrefixFile(t *testing.T) {
	if !playbookHasSearch(&Playbook{Search: SearchConfig{PrefixFile: "examples/prefixes.txt"}}) {
		t.Fatal("prefix_file should count as search")
	}
}

func TestBuildSearchArgsPrefixFile(t *testing.T) {
	args := buildSearchArgs(Playbook{Search: SearchConfig{PrefixFile: "/tmp/prefixes.txt", Out: "repos.txt"}})
	var found bool
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-prefix-file" && args[i+1] == "/tmp/prefixes.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing -prefix-file: %v", args)
	}
}
