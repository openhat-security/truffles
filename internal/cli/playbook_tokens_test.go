package cli

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSplitAuthTokens(t *testing.T) {
	got := splitAuthTokens("a", "b,c", " b ", "", "a")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSearchAuthTokensTokenOnly(t *testing.T) {
	got := searchAuthTokens(SearchConfig{Token: "one"})
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("got %v", got)
	}
}

func TestScanAuthTokensMerged(t *testing.T) {
	got := scanAuthTokens(ScanConfig{
		Token:  "a,b",
		Tokens: []string{"c"},
	})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSearchAuthTokensListAndLegacy(t *testing.T) {
	got := searchAuthTokens(SearchConfig{
		Token:  "first,second",
		Tokens: []string{"${GITHUB_TOKEN}", "third"},
	})
	want := []string{"first", "second", "${GITHUB_TOKEN}", "third"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestTokenRingRoundRobin(t *testing.T) {
	r := newTokenRing([]string{"a", "b", "c"})
	seq := []string{r.next(), r.next(), r.next(), r.next(), r.next()}
	want := []string{"a", "b", "c", "a", "b"}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("step %d: got %q want %q (seq=%v)", i, seq[i], want[i], seq)
		}
	}
}

func TestTokenRingEmpty(t *testing.T) {
	if newTokenRing(nil).next() != "" {
		t.Fatal("expected empty")
	}
}

func TestPlaybookYAMLSearchTokens(t *testing.T) {
	t.Setenv("TRUFFLES_PB_TOKEN_A", "from-env")
	t.Setenv("TRUFFLES_PB_TOKEN_B", "second")
	dir := t.TempDir()
	envPath := filepath.Join(dir, "tokens.env")
	if err := os.WriteFile(envPath, []byte("# env_file only fills unset vars\nTRUFFLES_PB_TOKEN_A=file-ignored\n"), 0644); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "pb.yaml")
	const body = `name: t
env_file: tokens.env
search:
  token: ${TRUFFLES_PB_TOKEN_A}
  tokens:
    - ${TRUFFLES_PB_TOKEN_B}
    - extra,${TRUFFLES_PB_TOKEN_A}
`
	if err := os.WriteFile(yamlPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	pb, err := loadPlaybook(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	got := searchAuthTokens(pb.Search)
	want := []string{"from-env", "second", "extra"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSearchConfigTokensYAMLList(t *testing.T) {
	var sc SearchConfig
	if err := yaml.Unmarshal([]byte("tokens:\n  - a\n  - b,c\n"), &sc); err != nil {
		t.Fatal(err)
	}
	got := searchAuthTokens(sc)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
