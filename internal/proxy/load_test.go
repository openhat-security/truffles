package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProxyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxies.txt")
	err := os.WriteFile(path, []byte("# c\n203.0.113.1:8080\ntrusted:203.0.113.2:3128\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cand, trust, err := LoadProxyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cand) != 1 || cand[0] != "203.0.113.1:8080" {
		t.Fatalf("candidates %#v", cand)
	}
	if len(trust) != 1 || trust[0] != "203.0.113.2:3128" {
		t.Fatalf("trusted %#v", trust)
	}
}
