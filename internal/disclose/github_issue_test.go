package disclose

import "testing"

func TestParseIssueURL(t *testing.T) {
	owner, repo, n, ok := ParseIssueURL("https://github.com/seth-stitik/live.Weather/issues/2")
	if !ok || owner != "seth-stitik" || repo != "live.Weather" || n != 2 {
		t.Fatalf("got %q %q %d ok=%v", owner, repo, n, ok)
	}
	if _, _, _, ok := ParseIssueURL("https://github.com/o/r/pull/1"); ok {
		t.Fatal("expected false for pull URL")
	}
}
