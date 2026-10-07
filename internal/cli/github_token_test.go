package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistAndLoadGitHubToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// Darwin uses $HOME/Library/Application Support; Linux uses XDG or ~/.config.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GH_TOKEN")

	tok := "ghp_test_token_value_not_real"
	if err := persistGitHubToken(tok); err != nil {
		t.Fatal(err)
	}
	path := githubTokenPath()
	if !strings.HasPrefix(path, dir) {
		t.Fatalf("token path %q not under temp HOME %q", path, dir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	got, err := loadPersistedGitHubToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != tok {
		t.Fatalf("got %q", got)
	}

	resolved, err := resolveGitHubToken("", false)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != tok {
		t.Fatalf("resolve = %q", resolved)
	}
	if os.Getenv("GITHUB_TOKEN") != tok {
		t.Fatal("GITHUB_TOKEN not set in env")
	}
}

func TestResolveDiscloseGitHubTokenSubmitRequiresBot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
	os.Unsetenv("OPENHAT_BOT_GH_TOKEN")
	t.Setenv("GITHUB_TOKEN", "ghp_personal_not_real")

	if _, _, err := resolveDiscloseGitHubToken("", true); err == nil {
		t.Fatal("expected error when submit without bot token")
	}
	t.Setenv("OPENHAT_BOT_GH_TOKEN", "ghp_bot_not_real")
	tok, src, err := resolveDiscloseGitHubToken("", true)
	if err != nil || tok == "" || src != "OPENHAT_BOT_GH_TOKEN" {
		t.Fatalf("tok=%q src=%q err=%v", tok, src, err)
	}
}

func TestResolveGitHubTokenPrefersOpenHatBot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("OPENHAT_BOT_GH_TOKEN")

	bot := "ghp_bot_token_not_real"
	personal := "ghp_personal_not_real"
	t.Setenv("OPENHAT_BOT_GH_TOKEN", bot)
	t.Setenv("GITHUB_TOKEN", personal)

	got, err := resolveGitHubToken("", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != bot {
		t.Fatalf("resolve = %q want bot token", got)
	}
}
