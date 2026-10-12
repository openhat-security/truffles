package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildPerformanceDocPhases(t *testing.T) {
	start := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	searchDur := 2*time.Minute + 500*time.Millisecond
	scanDur := 7 * time.Minute
	collectDur := 30 * time.Second

	doc := buildPerformanceDoc(RunPerformanceInput{
		CollectDir:  "/data/run-1",
		Playbook:    "remote",
		Command:     "playbook",
		StartedAt:   start,
		SearchRan:   true,
		SearchDur:   searchDur,
		SearchMeta:  &SearchPerformanceMeta{Matched: 42, Calls: 100},
		ScanDur:     scanDur,
		ScanMeta:    &ScanPerformanceMeta{Repos: 42, Findings: 3, Verified: 1},
		CollectDur:  collectDur,
		CollectMeta: &CollectPerformanceMeta{FilesCopied: 14},
	}, nil)

	if doc.Metadata.SchemaVersion != performanceSchemaVersion {
		t.Fatalf("schema: %d", doc.Metadata.SchemaVersion)
	}
	if doc.Summary.Search == nil || doc.Summary.Search.Skipped || doc.Summary.Search.Matched != 42 {
		t.Fatalf("search: %+v", doc.Summary.Search)
	}
	if doc.Summary.Scan == nil || doc.Summary.Scan.Duration != "7m0s" {
		t.Fatalf("scan: %+v", doc.Summary.Scan)
	}
	if doc.Summary.Collect == nil || doc.Summary.Collect.FilesCopied != 14 {
		t.Fatalf("collect: %+v", doc.Summary.Collect)
	}

	skipped := buildPerformanceDoc(RunPerformanceInput{
		CollectDir: "/data/run-1",
		Command:    "cluster run",
		StartedAt:  start,
		SearchRan:  false,
		ScanDur:    scanDur,
	}, nil)
	if skipped.Summary.Search == nil || !skipped.Summary.Search.Skipped {
		t.Fatalf("expected skipped search, got %+v", skipped.Summary.Search)
	}
}

func TestWritePerformanceFile(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-time.Minute)
	rec := &perfRecorder{dir: dir, start: start, meta: PerformanceMetadata{Command: "search"}}
	rec.record(PerformanceEvent{Phase: "search", Kind: "page", Name: "q=openai page 1", DurationSeconds: 1.2, OK: true})

	doc := buildPerformanceDoc(RunPerformanceInput{
		CollectDir: dir,
		Command:    "search",
		StartedAt:  start,
		SearchRan:  true,
		SearchDur:  10 * time.Second,
		SearchMeta: &SearchPerformanceMeta{Matched: 5},
	}, rec)
	if err := writePerformanceFile(dir, doc); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, performanceBasename)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded performanceDocument
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata.Command != "search" || len(decoded.Events) != 1 {
		t.Fatalf("unexpected decode: %+v", decoded)
	}
	if decoded.Summary.Search == nil || decoded.Summary.Search.Matched != 5 {
		t.Fatalf("summary: %+v", decoded.Summary.Search)
	}
}

func TestPercentileSeconds(t *testing.T) {
	p50 := percentileSeconds([]float64{1, 2, 3, 4, 100}, 0.50)
	if p50 != 3 {
		t.Fatalf("p50 = %v", p50)
	}
}
