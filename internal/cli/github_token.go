package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// githubTokenEnvVars is the disclose/bot token order after -token.
var githubTokenEnvVars = []string{"OPENHAT_BOT_GH_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"}

// resolveDiscloseGitHubToken picks auth for disclose. -submit accepts only -token
// or OPENHAT_BOT_GH_TOKEN so personal GITHUB_TOKEN / saved PAT are not used by mistake.
func resolveDiscloseGitHubToken(flagToken string, submit bool) (tok, source string, err error) {
	tok = strings.TrimSpace(flagToken)
	if tok != "" {
		return tok, "-token", nil
	}
	tok = strings.TrimSpace(os.Getenv("OPENHAT_BOT_GH_TOKEN"))
	if tok != "" {
		return tok, "OPENHAT_BOT_GH_TOKEN", nil
	}
	if submit {
		return "", "", fmt.Errorf(`-submit requires the OpenHat bot token: set OPENHAT_BOT_GH_TOKEN (e.g. truffles disclose -env-file remote.env …) or pass -token with the bot PAT; personal GITHUB_TOKEN and ~/.config/truffles/github_token are ignored for disclose`)
	}
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		tok = strings.TrimSpace(os.Getenv(key))
		if tok != "" {
			return tok, key, nil
		}
	}
	if saved, err := loadPersistedGitHubToken(); err != nil {
		return "", "", err
	} else if saved != "" {
		return saved, githubTokenPath(), nil
	}
	return "", "", nil
}

// resolveGitHubToken returns a token from (in order): flag, process env
// (OPENHAT_BOT_GH_TOKEN, GITHUB_TOKEN, GH_TOKEN), persisted config file.
// If still empty and prompt is true on a TTY, asks once and saves to the
// config file + GITHUB_TOKEN in the process env.
func resolveGitHubToken(flagToken string, prompt bool) (string, error) {
	tok := strings.TrimSpace(flagToken)
	if tok == "" {
		for _, key := range githubTokenEnvVars {
			tok = strings.TrimSpace(os.Getenv(key))
			if tok != "" {
				break
			}
		}
	}
	if tok == "" {
		if saved, err := loadPersistedGitHubToken(); err != nil {
			return "", err
		} else {
			tok = saved
		}
	}
	if tok != "" {
		_ = os.Setenv("GITHUB_TOKEN", tok)
		// Remember for later shells (flag, env, or file).
		if saved, _ := loadPersistedGitHubToken(); saved != tok {
			if err := persistGitHubToken(tok); err != nil {
				fmt.Fprintf(os.Stderr, "[!] could not save token: %v\n", err)
			}
		}
		return tok, nil
	}
	if !prompt || !isInteractiveTerminal() {
		return "", nil
	}
	fmt.Fprint(os.Stderr, "GitHub token (saved for later runs): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("read token: %w", err)
	}
	tok = strings.TrimSpace(line)
	if tok == "" {
		return "", fmt.Errorf("empty GitHub token")
	}
	if err := persistGitHubToken(tok); err != nil {
		return "", fmt.Errorf("save token: %w", err)
	}
	_ = os.Setenv("GITHUB_TOKEN", tok)
	fmt.Fprintf(os.Stderr, "[*] saved GitHub token to %s and set GITHUB_TOKEN\n", githubTokenPath())
	return tok, nil
}

func githubTokenPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "truffles", "github_token")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "truffles", "github_token")
}

func loadPersistedGitHubToken() (string, error) {
	path := githubTokenPath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	// Allow "GITHUB_TOKEN=…" form if the user edited the file.
	if strings.HasPrefix(tok, "GITHUB_TOKEN=") {
		tok = strings.TrimSpace(strings.TrimPrefix(tok, "GITHUB_TOKEN="))
		tok = strings.Trim(tok, "\"'")
	}
	return tok, nil
}

func persistGitHubToken(tok string) error {
	path := githubTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tok+"\n"), 0600)
}
