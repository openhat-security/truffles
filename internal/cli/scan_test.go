package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyChildEnvStripsInherited(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://stale:1")
	t.Setenv("https_proxy", "http://stale:2")
	env := proxyChildEnv(nil)
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		switch strings.ToLower(key) {
		case "http_proxy", "https_proxy", "all_proxy":
			t.Fatalf("direct env still has %s", key)
		}
	}
}

func TestKilledBySignalAndMessage(t *testing.T) {
	if killedBySignal(nil) || killedBySignal(exec.ErrNotFound) {
		t.Fatal("nil/ErrNotFound should not be killedBySignal")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Process.Kill()
	err := cmd.Wait()
	if !killedBySignal(err) {
		t.Fatalf("killedBySignal(%v) = false", err)
	}
	got := scanError(err, nil)
	if got == nil || !strings.Contains(got.Error(), "killed") {
		t.Fatalf("scanError = %v, want it to mention killed", got)
	}
}

func TestPermanentScanError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"repo not found", `{"msg":"git clone failed","error":"remote: Repository not found."}`, true},
		{"auth", `{"error":"could not read Username for 'https://github.com': terminal prompts disabled"}`, true},
		{"transient", `{"error":"net/http: TLS handshake timeout"}`, false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := permanentScanError([]byte(tc.stderr)); got != tc.want {
				t.Errorf("permanentScanError(%q) = %v, want %v", tc.stderr, got, tc.want)
			}
		})
	}
}

func TestScanErrorExtractsReasonFromJSONLogs(t *testing.T) {
	// trufflehog logs JSON to stderr; the useful message is buried in the last
	// record carrying an "error", and dumping the raw log is useless in a report.
	stderr := `{"level":"info-0","msg":"git clone failed","error":"error executing git clone: exit status 128"}
{"level":"error","msg":"error running scan","error":"failed to scan Git: could not clone repo: https://github.com/o/r"}`
	err := scanError(exec.ErrNotFound, []byte(stderr))
	if err == nil {
		t.Fatal("scanError returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not clone repo") {
		t.Errorf("scanError = %q, want it to mention the clone failure", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("scanError = %q, want a single line", msg)
	}
}

func TestScanErrorFallsBackToExitCode(t *testing.T) {
	err := scanError(&exec.ExitError{}, nil)
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Errorf("scanError = %v, want an exit-code message", err)
	}
}

func TestScanErrorTruncatesLongMessages(t *testing.T) {
	long := strings.Repeat("x", 500)
	err := scanError(nil, []byte(`{"error":"`+long+`"}`))
	if len(err.Error()) > 200 {
		t.Errorf("scanError message is %d chars, want it truncated", len(err.Error()))
	}
}

func TestReadURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repos.txt")
	content := "https://github.com/a/b\n\n  https://github.com/c/d  \n# a comment\nhttps://github.com/e/f\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readURLs(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://github.com/a/b", "https://github.com/c/d", "https://github.com/e/f"}
	if len(got) != len(want) {
		t.Fatalf("got %d urls, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("url %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadURLsMissingFile(t *testing.T) {
	if _, err := readURLs(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Error("readURLs on a missing file returned nil error")
	}
}

func TestScanRejectsUnknownFormat(t *testing.T) {
	err := runScan([]string{"-f", "x.txt", "-format", "bogus"})
	if err == nil {
		t.Fatal("runScan accepted an unknown -format")
	}
	if !strings.Contains(err.Error(), "format") {
		t.Errorf("error = %v, want it to mention the format", err)
	}
}

func TestScanRejectsMachineFormatWithoutJSON(t *testing.T) {
	// trufflehog's text output cannot be parsed, so a machine format without
	// -json would silently produce an empty report.
	err := runScan([]string{"-f", "x.txt", "-format", "csv", "-json=false"})
	if err == nil || !strings.Contains(err.Error(), "-json") {
		t.Errorf("error = %v, want it to explain the -json requirement", err)
	}
}
