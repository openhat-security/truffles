package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseJobStatusArgs(t *testing.T) {
	opts, err := parseJobStatusArgs([]string{"j-1", "--full", "--jobs-dir", "/tmp/j"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ID != "j-1" || !opts.Full || opts.JobsDir != "/tmp/j" {
		t.Fatalf("got %+v", opts)
	}
	opts, err = parseJobStatusArgs([]string{"--full"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ID != "" || !opts.Full {
		t.Fatalf("got %+v", opts)
	}
}

func TestEnrichJobMetaFromCmd(t *testing.T) {
	m := &jobMeta{
		Cmd: "/usr/bin/caffeinate -dims ./truffles job run -c remote-playbook.yaml --limit 3000 --sleep 45 --workers 16 --data-dir data --prefix-file examples/prefixes-openai.txt --terms-file examples/search-ai-llm.txt",
	}
	enrichJobMetaFromCmd(m)
	if m.TermsFile != "examples/search-ai-llm.txt" {
		t.Fatalf("terms = %q", m.TermsFile)
	}
	if m.DataDir != "data" {
		t.Fatalf("data = %q", m.DataDir)
	}
	if m.Workers != 16 {
		t.Fatalf("workers = %d", m.Workers)
	}
	if m.PrefixFile != "examples/prefixes-openai.txt" {
		t.Fatalf("prefix = %q", m.PrefixFile)
	}
}

func TestJobStartStamp(t *testing.T) {
	m := &jobMeta{ID: "j-20261012-031620"}
	if got := jobStartStamp(m); got != "20261012-031620" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectJobSessionArtifacts(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "repos-loop-20261012-031620.txt"), "https://github.com/a/b\nhttps://github.com/c/d\n")
	mustWrite(t, filepath.Join(dir, "repos-loop-20261011-010000.txt"), "https://github.com/old/x\n")
	if err := os.MkdirAll(filepath.Join(dir, "remote-results-20261012-031620"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "remote-results-20261011-010000"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs, pulled, runs := collectJobSessionArtifacts(dir, "20261012-031620")
	if pulled != 2 {
		t.Fatalf("pulled = %d", pulled)
	}
	if runs != 1 {
		t.Fatalf("runs = %d", runs)
	}
	if len(dirs) != 1 || !strings.HasSuffix(dirs[0], "remote-results-20261012-031620") {
		t.Fatalf("dirs = %v", dirs)
	}
}

func TestAggregateCSVStats(t *testing.T) {
	dir := t.TempDir()
	run := filepath.Join(dir, "remote-results-1")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(run, "results.csv"),
		"detector,verified,repository_url,secret\n"+
			"OpenAI,true,https://github.com/a/b,sk-1\n"+
			"OpenAI,false,https://github.com/a/b,sk-2\n"+
			"Anthropic,true,https://github.com/c/d,sk-ant-1\n")
	find, ver, hit := aggregateCSVStats([]string{run})
	if find != 3 || ver != 2 || hit != 2 {
		t.Fatalf("find=%d ver=%d hit=%d", find, ver, hit)
	}
}

func TestParseJobLogProgress(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "job.log")
	mustWrite(t, log, strings.Join([]string{
		"[2026-10-12 03:16:20] [j-1] cycle 1 — search (prefix=x, limit=3000) → data/repos.txt",
		`[+] 2m50s — 12 repos found, 12 calls, pool 29/30 alive`,
		"[2026-10-12 03:20:00] [j-1] cycle 1 — cluster run 12 repos → data/out",
		"[*] 50/818 done, 4 failed, 6 in flight, 57 finding(s) (1 verified), 8m0s elapsed, ~2h2m53s left",
		"[*] 36/818 done, 0 failed, 6 in flight, 97 finding(s) (3 verified), 8m0s elapsed, ~2h53m47s left",
	}, "\n"))
	prog := parseJobLogProgress(log)
	if prog.Phase != "scanning" {
		t.Fatalf("phase = %q", prog.Phase)
	}
	if prog.Cycle != 1 {
		t.Fatalf("cycle = %d", prog.Cycle)
	}
	if prog.Repos != 12 {
		t.Fatalf("repos = %d", prog.Repos)
	}
	if prog.ScanDone != 86 || prog.ScanTotal != 1636 {
		t.Fatalf("scan progress = %d/%d", prog.ScanDone, prog.ScanTotal)
	}
	if prog.ScanFindings != 154 || prog.ScanVerified != 4 {
		t.Fatalf("findings=%d verified=%d", prog.ScanFindings, prog.ScanVerified)
	}
	if len(prog.Snippet) == 0 {
		t.Fatal("expected snippet")
	}
}

func TestFormatUptime(t *testing.T) {
	if got := formatUptime(90 * time.Second); got != "1m30s" {
		t.Fatalf("got %q", got)
	}
	if got := formatUptime(25*time.Hour + 3*time.Minute); got != "1d1h3m" {
		t.Fatalf("got %q", got)
	}
}

func TestParseWorkerProbeOutput(t *testing.T) {
	var row workerStatusRow
	parseWorkerProbeOutput("3661|0.15", &row, false)
	if row.Uptime != "1h1m1s" || row.Load != "0.15" {
		t.Fatalf("got %+v", row)
	}
	row = workerStatusRow{}
	parseWorkerProbeOutput("120|0.5 0.4 0.3|2|512/7841|34|12", &row, true)
	if row.CPUs != 2 || row.Mem != "512/7841" || row.Disk != "34%" || row.CPUPct != "12%" {
		t.Fatalf("got %+v", row)
	}
}

func TestInferMachineFromMem(t *testing.T) {
	if got := inferMachineFromMem("260/7939", 2); got != "~8GB/2vCPU" {
		t.Fatalf("got %q", got)
	}
	if got := inferMachineFromMem("228/1964", 2); got != "~2GB/2vCPU" {
		t.Fatalf("got %q", got)
	}
	if got := inferMachineFromMem("", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestPrintJobStatusReportSmoke(t *testing.T) {
	rep := &jobStatusReport{
		Meta: &jobMeta{
			ID:         "j-20261012-031620",
			Status:     jobStatusRunning,
			PID:        42,
			Playbook:   "remote-playbook.yaml",
			PrefixFile: "examples/prefixes-openai.txt",
			TermsFile:  "examples/search-ai-llm.txt",
			Mode:       "split",
			Limit:      3000,
			SleepSecs:  45,
			Workers:    16,
			StartedAt:  time.Now().UTC().Format(time.RFC3339),
			LogPath:    "data/jobs/j/job.log",
		},
		Alive:           true,
		Uptime:          5 * time.Minute,
		Phase:           "searching",
		Cycle:           1,
		Scanned:         5411,
		Clean:           5331,
		SessionPulled:   0,
		SessionFindings: 0,
		LifeFindings:    100,
		LifeVerified:    20,
		LifeReposHit:    15,
		Workers: []workerStatusRow{
			{Name: "worker1", Host: "1.2.3.4", Enabled: true, Workers: 6, SSH: "ok", Uptime: "2d4h", Load: "0.2"},
			{Name: "worker3", Host: "5.6.7.8", Enabled: true, Workers: 2, SSH: "down"},
		},
		LogSnippet: []string{"cycle 1 — search"},
	}
	var buf bytes.Buffer
	printJobStatusReport(&buf, palette{on: false}, rep)
	out := buf.String()
	for _, want := range []string{"j-20261012-031620", "This job", "Workers", "Findings", "worker1", "searching", "View Recent Logs", "job logs", "workers", "Running for"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Lifetime") {
		t.Fatalf("lifetime should be hidden without --full:\n%s", out)
	}
	if strings.Contains(out, "Recent log\n") || strings.Contains(out, "  Load") {
		t.Fatalf("should not dump log lines or Load column:\n%s", out)
	}
	rep.Full = true
	buf.Reset()
	printJobStatusReport(&buf, palette{on: false}, rep)
	if !strings.Contains(buf.String(), "Lifetime") {
		t.Fatal("expected Lifetime section with --full")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
