package disclose

import (
	"strings"
	"testing"
)

func TestIssueBodyHasNoSensitiveDetail(t *testing.T) {
	body := IssueBody(DefaultContact)
	for _, bad := range []string{
		"********",
		"hunter2",
		"file_line",
		".env:1",
		"OpenWeather",
		"sk-test",
		"abc123def",
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("issue body must not contain %q:\n%s", bad, body)
		}
	}
	if !strings.Contains(body, DefaultContact) {
		t.Fatal("missing contact email")
	}
	for _, want := range []string{TrufflesRepoURL, OpenHatOrgURL, TrufflesSiteURL, "star", "follow"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Fatalf("issue body missing %q", want)
		}
	}
}

func TestPrivateDescriptionCreditsOpenHat(t *testing.T) {
	r := RepoReport{
		Owner:   "acme",
		Repo:    "app",
		RepoURL: "https://github.com/acme/app",
		Findings: []Finding{{
			Detector: "Resend",
			Verified: true,
			Secret:   "re_test",
			FileLine: "x.csv:7",
			Commit:   "84b1ac0ddeadbeef",
		}},
	}
	got := PrivateDescription(r, false)
	for _, want := range []string{
		"### Summary",
		"### Details",
		"### Proof of concept",
		"### Impact",
		TrufflesRepoURL,
		OpenHatOrgURL,
		TrufflesSiteURL,
		"star",
		"follow",
	} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Fatalf("private body missing %q:\n%s", want, got)
		}
	}
}

func TestPrivateDescriptionOmitsSecretByDefault(t *testing.T) {
	r := RepoReport{
		Owner:   "acme",
		Repo:    "app",
		RepoURL: "https://github.com/acme/app",
		Findings: []Finding{{
			Detector: "URI",
			Verified: true,
			Secret:   "https://user:hunter2@host/path",
			FileLine: "cfg.py:12",
			Commit:   "abc123",
		}},
	}
	got := PrivateDescription(r, false)
	if strings.Contains(got, "hunter2") {
		t.Fatalf("secret leaked without includeSecret:\n%s", got)
	}
	if !strings.Contains(got, "cfg.py:12") {
		t.Fatal("expected file location")
	}
	got2 := PrivateDescription(r, true)
	if !strings.Contains(got2, "hunter2") {
		t.Fatal("expected secret when includeSecret")
	}
}

func TestParseGitHubRepo(t *testing.T) {
	o, r, ok := ParseGitHubRepo("https://github.com/acme/app.git")
	if !ok || o != "acme" || r != "app" {
		t.Fatalf("got %s/%s ok=%v", o, r, ok)
	}
	if _, _, ok := ParseGitHubRepo("https://gitlab.com/acme/app"); ok {
		t.Fatal("gitlab should be rejected")
	}
}
