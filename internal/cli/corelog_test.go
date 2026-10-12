package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreLogWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "truffles.log")
	SetCoreLogPath(path)
	CoreLog("test", "hello")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "test") || !strings.Contains(string(b), "hello") {
		t.Fatalf("log: %q", b)
	}
}
