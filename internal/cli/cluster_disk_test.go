package cli

import (
	"strings"
	"testing"
	"time"
)

func TestParseDiskCleanOutput(t *testing.T) {
	freed, actions := parseDiskCleanOutput("FREED_MB=42|ACTIONS=rm truffles-since-abc (40M); rm results-w1.csv (2M)")
	if freed != 42 {
		t.Fatalf("freed = %d", freed)
	}
	if !strings.Contains(actions, "truffles-since") {
		t.Fatalf("actions = %q", actions)
	}
	_, actions = parseDiskCleanOutput("")
	if actions != "nothing to purge" {
		t.Fatalf("empty = %q", actions)
	}
}

func TestBuildDiskCleanScriptContainsSafety(t *testing.T) {
	script := buildDiskCleanScript("/home/u/truffles-work", diskCleanOpts{
		MinAge:      15 * time.Minute,
		OrphansOnly: true,
	})
	for _, want := range []string{"truffles-since-", "MIN_AGE=", "mmin", "FREED_MB=", "find "} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %q in script:\n%s", want, script)
		}
	}
	if strings.Contains(script, "du -sm") {
		t.Fatal("script must not du large clones (hangs at 99% disk)")
	}
	// Orphans-only should not purge workdir artifacts.
	if strings.Contains(script, "PURGE_ART=1") {
		t.Fatal("orphans-only should set PURGE_ART=0")
	}
	script2 := buildDiskCleanScript("/wd", diskCleanOpts{AfterCollect: true, MinAge: time.Minute})
	if !strings.Contains(script2, "PURGE_ART=1") {
		t.Fatal("after-collect should purge artifacts")
	}
}

func TestUtf8Trunc(t *testing.T) {
	if got := utf8Trunc("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := utf8Trunc("abcdefghij", 5); got != "abcde" {
		t.Fatalf("got %q", got)
	}
}
