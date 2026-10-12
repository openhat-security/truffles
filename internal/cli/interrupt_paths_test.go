package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintInterruptedLocalPaths(t *testing.T) {
	dir := t.TempDir()
	must := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("results-worker1.csv")
	must("scan-worker1.log")
	must("results.csv")

	var buf bytes.Buffer
	printInterruptedLocalPaths(&buf, dir)
	out := buf.String()
	abs, _ := filepath.Abs(dir)
	if !strings.Contains(out, abs) {
		t.Fatalf("missing dir abs path in %q", out)
	}
	if !strings.Contains(out, "results-worker1.csv") {
		t.Fatalf("missing csv in %q", out)
	}
	if !strings.Contains(out, "scan-worker1.log") {
		t.Fatalf("missing log in %q", out)
	}
	// Paths should be absolute (clickable in most terminals).
	if strings.Contains(out, "  results-worker1.csv\n") {
		t.Fatalf("expected absolute path, got relative listing: %q", out)
	}
}

func TestAbsPathOrSelf(t *testing.T) {
	if absPathOrSelf("") != "" {
		t.Fatal("empty")
	}
	got := absPathOrSelf(".")
	if !filepath.IsAbs(got) {
		t.Fatalf("got %q", got)
	}
}
