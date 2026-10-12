package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewJobID(t *testing.T) {
	ts := time.Date(2026, 10, 11, 16, 21, 45, 0, time.UTC)
	got := newJobID(ts)
	want := "j-20261011-162145"
	if got != want {
		t.Fatalf("newJobID = %q want %q", got, want)
	}
}

func TestSaveLoadJobMeta(t *testing.T) {
	root := t.TempDir()
	m := &jobMeta{
		ID:         "j-20261011-162145",
		Playbook:   "remote-playbook.yaml",
		PrefixFile: "examples/prefixes-openai.txt",
		Mode:       "split",
		PID:        12345,
		PGID:       12345,
		StartedAt:  "2026-10-11T16:21:45Z",
		Status:     jobStatusRunning,
		Limit:      500,
		SleepSecs:  60,
	}
	if err := saveJobMeta(root, m); err != nil {
		t.Fatal(err)
	}
	got, err := loadJobMeta(root, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Playbook != m.Playbook || got.PID != m.PID || got.Status != jobStatusRunning {
		t.Fatalf("got %+v", got)
	}
	if got.LogPath != jobLogPath(root, m.ID) {
		t.Fatalf("log path = %q", got.LogPath)
	}
	if _, err := os.Stat(jobMetaPath(root, m.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshJobStatusDeadPID(t *testing.T) {
	root := t.TempDir()
	m := &jobMeta{
		ID:        "j-dead",
		Playbook:  "p.yaml",
		PID:       1 << 30, // almost certainly not a live pid
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Status:    jobStatusRunning,
	}
	if err := saveJobMeta(root, m); err != nil {
		t.Fatal(err)
	}
	got, err := refreshJobStatus(root, m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != jobStatusExited {
		t.Fatalf("status = %q want exited", got.Status)
	}
	reloaded, err := loadJobMeta(root, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != jobStatusExited {
		t.Fatalf("persisted status = %q", reloaded.Status)
	}
}

func TestRefreshJobStatusAliveSelf(t *testing.T) {
	root := t.TempDir()
	m := &jobMeta{
		ID:        "j-alive",
		Playbook:  "p.yaml",
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Status:    jobStatusRunning,
	}
	if err := saveJobMeta(root, m); err != nil {
		t.Fatal(err)
	}
	got, err := refreshJobStatus(root, m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != jobStatusRunning {
		t.Fatalf("status = %q want running", got.Status)
	}
}

func TestListJobIDs(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"j-20261011-010000", "j-20261011-020000", "skip-me"} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if id == "skip-me" {
			continue
		}
		m := &jobMeta{ID: id, Playbook: "p.yaml", Status: jobStatusExited, PID: 1}
		if err := saveJobMeta(root, m); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := listJobIDs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "j-20261011-010000" || ids[1] != "j-20261011-020000" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatal("self should be alive")
	}
	if pidAlive(0) || pidAlive(-1) {
		t.Fatal("invalid pids should be dead")
	}
}

func TestParseJobIDArgs(t *testing.T) {
	id, dir, err := parseJobIDArgs([]string{"j-1", "--jobs-dir", "/tmp/x"}, true)
	if err != nil || id != "j-1" || dir != "/tmp/x" {
		t.Fatalf("got id=%q dir=%q err=%v", id, dir, err)
	}
	id, dir, err = parseJobIDArgs([]string{"--jobs-dir", "/tmp/y", "j-2"}, true)
	if err != nil || id != "j-2" || dir != "/tmp/y" {
		t.Fatalf("got id=%q dir=%q err=%v", id, dir, err)
	}
}
