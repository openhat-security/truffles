package cli

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleFinding is a trimmed but structurally faithful trufflehog JSON line,
// including the nested SourceMetadata envelope this tool has to flatten.
const sampleFinding = `{"Verified":true,"VerificationError":"","DetectorName":"Airtable","DetectorDescription":"cloud db","DecoderName":"PLAIN","Raw":"patABC123","RawV2":"","Redacted":"","SourceMetadata":{"Data":{"Git":{"repository":"https://github.com/o/r","file":"a/b.py","line":6,"commit":"8f79e2038f79e203","email":"dev@example.com","timestamp":"2024-05-27T12:00:00Z"}}}}`

func TestParseFindingsJSON(t *testing.T) {
	got, lines := parseFindings(sampleFinding+"\n"+sampleFinding+"\n", true)
	if lines != 2 {
		t.Errorf("lines = %d, want 2", lines)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	f := got[0]
	if !f.Verified {
		t.Error("Verified = false, want true")
	}
	if f.Repo() != "https://github.com/o/r" {
		t.Errorf("Repo() = %q", f.Repo())
	}
	if f.Location() != "a/b.py:6" {
		t.Errorf("Location() = %q, want a/b.py:6", f.Location())
	}
}

func TestParseFindingsCountsUnparseableLines(t *testing.T) {
	// trufflehog interleaves log noise; it must be counted, never dropped
	// silently and never treated as a finding.
	in := sampleFinding + "\nnot json at all\n" + sampleFinding + "\n"
	got, lines := parseFindings(in, true)
	if lines != 3 {
		t.Errorf("lines = %d, want 3 (every non-empty line counted)", lines)
	}
	if len(got) != 2 {
		t.Errorf("got %d findings, want 2", len(got))
	}
}

func TestParseFindingsPlainTextHasNoStructuredFindings(t *testing.T) {
	// trufflehog's text output has no machine-readable fields, so without -json
	// there is nothing to flatten: lines are still counted for progress, but
	// no Finding is fabricated. This is why csv/jsonl require -json.
	got, lines := parseFindings("some finding text\n", false)
	if lines != 1 {
		t.Errorf("lines = %d, want 1", lines)
	}
	if len(got) != 0 {
		t.Errorf("got %d structured findings from text output, want 0", len(got))
	}
}

func TestLocationWithoutLine(t *testing.T) {
	f := Finding{}
	f.SourceMetadata.Data.Git.File = "only/file.txt"
	if got := f.Location(); got != "only/file.txt" {
		t.Errorf("Location() = %q, want only/file.txt", got)
	}
	f.SourceMetadata.Data.Git.File = ""
	if got := f.Location(); got != "" {
		t.Errorf("Location() = %q, want empty", got)
	}
}

func TestOpenReportSinkDualWrite(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "findings.csv")
	s, err := openReportSink(out, "csv", "never", false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.writeRepo("https://github.com/a/b", Result{Repo: "https://github.com/a/b"})
	s.Close()
	if _, err := os.Stat(filepath.Join(dir, "findings.csv")); err != nil {
		t.Fatalf("csv missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "findings.txt")); err != nil {
		t.Fatalf("pretty sibling missing: %v", err)
	}
}

func TestDefaultReportName(t *testing.T) {
	tests := []struct {
		input, format, suffix string
	}{
		{"ai-100.txt", "pretty", ".txt"},
		{"ai-100.txt", "csv", ".csv"},
		{"ai-100.txt", "jsonl", ".jsonl"},
		{"/tmp/nested/list.txt", "pretty", ".txt"},
		{"noext", "csv", ".csv"},
	}
	for _, tc := range tests {
		got := defaultReportName(tc.input, tc.format)
		if !strings.HasSuffix(got, tc.suffix) {
			t.Errorf("defaultReportName(%q, %q) = %q, want suffix %q", tc.input, tc.format, got, tc.suffix)
		}
		// The timestamp keeps concurrent runs from overwriting each other.
		stem := strings.TrimSuffix(got, tc.suffix)
		ts := strings.TrimPrefix(filepath.Base(stem), strings.TrimSuffix(filepath.Base(tc.input), filepath.Ext(tc.input))+"-")
		if ts == "" || strings.ContainsAny(ts, "abcdefghijklmnopqrstuvwxyz") {
			t.Errorf("defaultReportName(%q, %q) = %q, want a numeric suffix", tc.input, tc.format, got)
		}
	}
}

func TestSecretNeverUsesRedacted(t *testing.T) {
	f := Finding{
		Raw:      "https://user:hunter2@host/path",
		Redacted: "https://user:********@host/path",
	}
	if got := f.Secret(); got != f.Raw {
		t.Fatalf("Secret() = %q, want plaintext Raw", got)
	}
	f2 := Finding{RawV2: "only-v2", Redacted: "********"}
	if got := f2.Secret(); got != "only-v2" {
		t.Fatalf("Secret() = %q, want RawV2", got)
	}
	f3 := Finding{Redacted: "fallback-only"}
	if got := f3.Secret(); got != "" {
		t.Fatalf("Secret() = %q, want empty when only Redacted is set", got)
	}
}

func TestWriteCSVQuotesEmbeddedSeparators(t *testing.T) {
	// Real secrets and verification errors contain commas, quotes and
	// newlines, so quoting is the whole point of using encoding/csv.
	f := Finding{Raw: "a,b\"c\nd"}
	f.SourceMetadata.Data.Git.Repository = "https://github.com/o/r"
	r := Result{Repo: "https://github.com/o/r", Findings: []Finding{f}}

	var sb strings.Builder
	writeCSVHeader(&sb)
	writeCSV(&sb, r)

	rows, err := csv.NewReader(strings.NewReader(sb.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv did not round-trip: %v\n%s", err, sb.String())
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want header + 1", len(rows))
	}
	if len(rows[0]) != 10 {
		t.Errorf("header has %d columns, want 11", len(rows[0]))
	}
	for i, row := range rows {
		if len(row) != 10 {
			t.Errorf("row %d has %d columns, want 11", i, len(row))
		}
	}
	if rows[1][3] != "a,b\"c\nd" {
		t.Errorf("secret column = %q, want the raw value preserved", rows[1][9])
	}
}

func TestWriteCSVHeaderIsWrittenOnceAndDataAppended(t *testing.T) {
	// The header is emitted up front, not per repo, so a report spanning many
	// repos has exactly one header row.
	var sb strings.Builder
	writeCSVHeader(&sb)
	writeCSV(&sb, Result{Repo: "https://github.com/o/r", Findings: []Finding{{DetectorName: "A"}}})
	writeCSV(&sb, Result{Repo: "https://github.com/o/r2", Findings: []Finding{{DetectorName: "B"}}})

	rows, err := csv.NewReader(strings.NewReader(sb.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 1 header + 2 findings", len(rows))
	}
	if rows[0][0] != "detector" || rows[0][9] != "verification_error" {
		t.Errorf("unexpected header: %v", rows[0])
	}
}

func TestWriteCSVNoFindingsWritesNoRows(t *testing.T) {
	var sb strings.Builder
	writeCSV(&sb, Result{Repo: "https://github.com/o/r"})
	if sb.String() != "" {
		t.Errorf("a clean repo wrote %q, want nothing", sb.String())
	}
}

func TestWriteJSONLEachLineIsAnObject(t *testing.T) {
	f := Finding{DetectorName: "Postgres"}
	r := Result{Repo: "https://github.com/o/r", Findings: []Finding{f, f}}

	var sb strings.Builder
	writeJSONL(&sb, r)

	lines := strings.Split(strings.TrimSpace(sb.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per finding", len(lines))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, "{") || !strings.HasSuffix(l, "}") {
			t.Errorf("line %d is not a standalone JSON object: %q", i, l)
		}
	}
}

func TestResolveColorAutoFollowsTTY(t *testing.T) {
	// A regular file is never a terminal, so auto must resolve to off and
	// keep redirected reports free of escape codes.
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if resolveColor("auto", f) {
		t.Error("resolveColor(auto, file) = true, want false")
	}
	if resolveColor("never", f) {
		t.Error("resolveColor(never, file) = true, want false")
	}
	if !resolveColor("always", f) {
		t.Error("resolveColor(always, file) = false, want true")
	}
}
