package cli

import (
	"strings"
	"testing"
	"time"
)

func TestMirrorPullInterval(t *testing.T) {
	if got := mirrorPullInterval(ScanConfig{}); got != 30*time.Second {
		t.Fatalf("default = %v", got)
	}
	if got := mirrorPullInterval(ScanConfig{Progress: "15s"}); got != 15*time.Second {
		t.Fatalf("progress = %v", got)
	}
	if got := mirrorPullInterval(ScanConfig{Progress: "nope"}); got != 30*time.Second {
		t.Fatalf("bad progress = %v", got)
	}
	if got := mirrorPullInterval(ScanConfig{Progress: "0"}); got != 30*time.Second {
		t.Fatalf("zero progress = %v", got)
	}
}

func TestWorkerScanLogPath(t *testing.T) {
	got := workerScanLogPath("data/run-1", "worker1")
	want := "data/run-1/scan-worker1.log"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRemoteScanCommandProgress(t *testing.T) {
	sl := Slave{Name: "worker1", TrufflesPath: "/usr/local/bin/truffles"}
	cmd := remoteScanCommand("/home/u/work", sl, 4, "csv", "csv", ScanConfig{Progress: "20s"}, false)
	if !strings.Contains(cmd, "-progress '20s'") {
		t.Fatalf("missing progress flag: %q", cmd)
	}
	if !strings.Contains(cmd, "results-worker1.csv") {
		t.Fatalf("missing out file: %q", cmd)
	}
}

func TestRemoteWorkerArtifactNames(t *testing.T) {
	names := remoteWorkerArtifactNames(Slave{Name: "worker1"})
	want := []string{
		"results-worker1.csv",
		"results-worker1.txt",
		"results-worker1.jsonl",
		"scanned-worker1.txt",
	}
	if len(names) != len(want) {
		t.Fatalf("got %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("[%d] = %q want %q", i, names[i], want[i])
		}
	}
}
