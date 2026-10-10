package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildPlaybookRunTiming(t *testing.T) {
	start := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	searchDur := 2*time.Minute + 500*time.Millisecond
	scanDur := 7 * time.Minute

	rec := buildPlaybookRunTiming("/data/run-1", "remote", start, true, searchDur, scanDur)
	if rec.Search == nil || rec.Search.Skipped || rec.Search.Duration != "2m0.5s" {
		t.Fatalf("search: %+v", rec.Search)
	}
	if rec.Scan == nil || rec.Scan.Duration != "7m0s" {
		t.Fatalf("scan: %+v", rec.Scan)
	}

	skipped := buildPlaybookRunTiming("/data/run-1", "remote", start, false, 0, scanDur)
	if skipped.Search == nil || !skipped.Search.Skipped {
		t.Fatalf("expected skipped search, got %+v", skipped.Search)
	}
}

func TestWritePlaybookTiming(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-time.Minute)
	rec := buildPlaybookRunTiming(dir, "test", start, true, 10*time.Second, 20*time.Second)
	if err := writePlaybookTiming(dir, rec); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, playbookTimingBasename)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded playbookRunTiming
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Playbook != "test" || decoded.Scan == nil || decoded.Search == nil {
		t.Fatalf("unexpected decode: %+v", decoded)
	}
}
