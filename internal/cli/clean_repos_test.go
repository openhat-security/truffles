package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanStoreMarkCleanAndDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean-repos.txt")
	store := newCleanStore(path)
	if err := store.MarkClean("https://github.com/A/B"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkClean("https://github.com/c/d.git"); err != nil {
		t.Fatal(err)
	}
	m, err := loadCleanMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("len=%d want 2: %v", len(m), m)
	}
	if _, ok := m[normalizeRepoURL("https://github.com/a/b")]; !ok {
		t.Fatalf("missing a/b: %v", m)
	}
	if err := store.MarkDirty("http://github.com/A/B/"); err != nil {
		t.Fatal(err)
	}
	m, err = loadCleanMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Fatalf("after dirty len=%d want 1", len(m))
	}
}

func TestUnionCleanIntoSkip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.json")
	raw := map[string]cleanEntry{
		"github.com/x/y": {URL: "https://github.com/x/y", At: "2026-01-01T00:00:00Z"},
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	skip := map[string]struct{}{
		normalizeRepoURL("https://github.com/a/b"): {},
	}
	n, err := unionCleanIntoSkip(skip, path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("added=%d want 1", n)
	}
	if _, ok := skip[normalizeRepoURL("https://github.com/x/y")]; !ok {
		t.Fatalf("clean key missing: %v", skip)
	}
}

func TestMergeCleanFiles(t *testing.T) {
	dir := t.TempDir()
	master := filepath.Join(dir, "clean.json")
	d1 := filepath.Join(dir, "clean-w1.json")
	d2 := filepath.Join(dir, "clean-w2.json")
	if err := writeCleanMap(d1, map[string]cleanEntry{
		"github.com/a/b": {URL: "https://github.com/a/b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCleanMap(d2, map[string]cleanEntry{
		"github.com/c/d": {URL: "https://github.com/c/d"},
		"github.com/a/b": {URL: "https://github.com/a/b"},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := mergeCleanFiles(master, []string{d1, d2})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("added=%d want 2", n)
	}
	n, err = mergeCleanFiles(master, []string{d1, d2})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second merge added %d", n)
	}
}

func TestSeedCleanReposFromScanned(t *testing.T) {
	dir := t.TempDir()
	scanned := filepath.Join(dir, "scanned.txt")
	clean := filepath.Join(dir, "clean.json")
	if err := os.WriteFile(scanned, []byte(
		"https://github.com/a/clean\nhttps://github.com/b/hit\nhttps://github.com/c/also-clean\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	verified := map[string]struct{}{
		normalizeRepoURL("https://github.com/b/hit"): {},
	}
	n, err := seedCleanReposFromScanned(clean, scanned, verified)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("seeded=%d want 2", n)
	}
	m, err := loadCleanMap(clean)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m[normalizeRepoURL("https://github.com/b/hit")]; ok {
		t.Fatal("verified repo should not be clean")
	}
}

func TestWriteCombinedSkipFile(t *testing.T) {
	dir := t.TempDir()
	skip := filepath.Join(dir, "scanned.txt")
	clean := filepath.Join(dir, "clean.json")
	out := filepath.Join(dir, "combined.txt")
	if err := os.WriteFile(skip, []byte("https://github.com/a/b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCleanMap(clean, map[string]cleanEntry{
		"github.com/c/d": {URL: "https://github.com/c/d"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCombinedSkipFile(out, skip, clean); err != nil {
		t.Fatal(err)
	}
	set, err := loadSkipSet(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 2 {
		t.Fatalf("combined size %d want 2: %v", len(set), set)
	}
}

func TestVerifiedReposFromCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.csv")
	if err := os.WriteFile(p, []byte(
		"repository_url,secret,verified\n"+
			"https://github.com/a/b,sec1,true\n"+
			"https://github.com/c/d,sec2,false\n"+
			"file:///tmp/x,sec3,true\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := verifiedReposFromCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Fatalf("got %v want 1 github repo", m)
	}
}
