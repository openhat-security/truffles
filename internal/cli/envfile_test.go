package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileAndExpandSlave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.env")
	content := "# comment\n" +
		"TRUFFLES_WORKER_HOST=10.0.0.1\n" +
		"TRUFFLES_WORKER_USER=alice\n" +
		"TRUFFLES_WORKER_KEY=${HOME}/.ssh/id_ed25519\n" +
		"TRUFFLES_WORKER_WORKDIR=/home/alice/truffles-work\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("TRUFFLES_WORKER_HOST")
	os.Unsetenv("TRUFFLES_WORKER_USER")
	os.Unsetenv("TRUFFLES_WORKER_KEY")
	os.Unsetenv("TRUFFLES_WORKER_WORKDIR")

	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TRUFFLES_WORKER_HOST"); got != "10.0.0.1" {
		t.Fatalf("host = %q", got)
	}

	sl := Slave{
		Host:    "${TRUFFLES_WORKER_HOST}",
		User:    "${TRUFFLES_WORKER_USER}",
		Key:     "${TRUFFLES_WORKER_KEY}",
		WorkDir: "${TRUFFLES_WORKER_WORKDIR}",
	}
	expandSlave(&sl)
	if sl.Host != "10.0.0.1" || sl.User != "alice" {
		t.Fatalf("expanded slave = %+v", sl)
	}
	wantKey := os.Getenv("HOME") + "/.ssh/id_ed25519"
	if sl.Key != wantKey {
		t.Fatalf("key = %q, want %q", sl.Key, wantKey)
	}
	if sl.WorkDir != "/home/alice/truffles-work" {
		t.Fatalf("workdir = %q", sl.WorkDir)
	}
}

func TestLoadEnvFileDoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.env")
	if err := os.WriteFile(path, []byte("TRUFFLES_TEST_VAR=fromfile\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRUFFLES_TEST_VAR", "fromenv")
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TRUFFLES_TEST_VAR"); got != "fromenv" {
		t.Fatalf("got %q, want fromenv", got)
	}
}
