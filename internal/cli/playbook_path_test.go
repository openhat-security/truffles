package cli

import "testing"

func TestPlaybookHasSearch(t *testing.T) {
	if playbookHasSearch(&Playbook{}) {
		t.Fatal("empty playbook should not have search")
	}
	if !playbookHasSearch(&Playbook{Search: SearchConfig{Owner: "acme"}}) {
		t.Fatal("owner should count as search")
	}
	if !playbookHasSearch(&Playbook{Search: SearchConfig{Queries: []string{"llm"}}}) {
		t.Fatal("queries should count as search")
	}
}

func TestReposListBasename(t *testing.T) {
	if got := reposListBasename(nil); got != "repos.txt" {
		t.Fatalf("nil = %q", got)
	}
	if got := reposListBasename(&Playbook{Search: SearchConfig{Out: "data/foo/list.txt"}}); got != "list.txt" {
		t.Fatalf("search.out = %q", got)
	}
	if got := reposListBasename(&Playbook{Scan: ScanConfig{File: "/tmp/x/repos.txt"}}); got != "repos.txt" {
		t.Fatalf("scan.file = %q", got)
	}
}
