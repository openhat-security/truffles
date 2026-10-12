package cli

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	got, err := parseSince("2024-04-10")
	if err != nil {
		t.Fatal(err)
	}
	if got.UTC().Format("2006-01-02") != "2024-04-10" {
		t.Fatalf("got %v", got)
	}

	got, err = parseSince("2y")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().UTC().AddDate(-2, 0, 0)
	if got.After(want.Add(48*time.Hour)) || got.Before(want.Add(-48*time.Hour)) {
		t.Fatalf("2y = %v, want near %v", got, want)
	}

	if _, err := parseSince("nope"); err == nil {
		t.Fatal("expected error")
	}
	for _, full := range []string{"", "all", "full", "0", "none"} {
		got, err := parseSince(full)
		if err != nil || !got.IsZero() {
			t.Fatalf("parseSince(%q) = %v, %v", full, got, err)
		}
	}
}

func TestResolveSinceFlag(t *testing.T) {
	if got := resolveSinceFlag(""); got != defaultSince {
		t.Fatalf("default = %q", got)
	}
	if got := resolveSinceFlag("6m"); got != "6m" {
		t.Fatalf("override = %q", got)
	}
	if got := resolveSinceFlag("all"); got != "" {
		t.Fatalf("all = %q", got)
	}
}

func TestFileURI(t *testing.T) {
	got := fileURI("/tmp/truffles-since-123")
	if got != "file:///tmp/truffles-since-123" {
		t.Fatalf("got %q", got)
	}
}
