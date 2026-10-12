package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// defaultSince is the scan window when -since / scan.since is omitted.
// Override with any supported value, or all/full/0 for entire history.
const defaultSince = "2y"

// sinceMeansFullHistory reports values that scan every commit (no --since-commit).
func sinceMeansFullHistory(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all", "full", "0", "none", "off":
		return true
	default:
		return false
	}
}

// resolveSinceFlag picks the effective -since string for a scan invocation.
// empty (unset) → defaultSince; all/full/0/none/off → "" (full history).
func resolveSinceFlag(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultSince
	}
	if sinceMeansFullHistory(s) {
		return ""
	}
	return s
}

// parseSince turns playbook/CLI -since values into a UTC cutoff.
// Accepts YYYY-MM-DD, relative forms like 2y / 24m / 730d, or all/full/0 for
// no cutoff (full history). Empty is treated as full history — callers that
// want the 2y default should run resolveSinceFlag first.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if sinceMeansFullHistory(s) {
		return time.Time{}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.UTC); err == nil {
		return t, nil
	}
	re := regexp.MustCompile(`(?i)^(\d+)\s*([ymd])$`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid -since %q (want YYYY-MM-DD, 2y/24m/730d, or all)", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("invalid -since %q", s)
	}
	now := time.Now().UTC()
	switch strings.ToLower(m[2]) {
	case "y":
		return now.AddDate(-n, 0, 0), nil
	case "m":
		return now.AddDate(0, -n, 0), nil
	case "d":
		return now.AddDate(0, 0, -n), nil
	default:
		return time.Time{}, fmt.Errorf("invalid -since %q", s)
	}
}

// fileURI turns a local path into a file:// URI trufflehog accepts.
// Bare paths like /tmp/foo fail with "unsupported Git URI".
func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	// url.URL with Path="/tmp/x" → "file:///tmp/x"
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

// fullClone clones the full default-branch history into dest (no shallow/depth).
func fullClone(ctx context.Context, repoURL, dest string, env []string) error {
	return gitClone(ctx, env, dest, repoURL, "--single-branch", "--no-tags")
}

// sinceCommitSHA returns the newest commit strictly before since, suitable for
// trufflehog --since-commit (scan from that point toward HEAD). Empty string
// means the whole repo is newer than since — scan everything.
func sinceCommitSHA(ctx context.Context, repoDir string, since time.Time) (string, error) {
	if since.IsZero() {
		return "", nil
	}
	sinceStr := since.UTC().Format("2006-01-02")
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-list", "-n", "1", "--before="+sinceStr, "HEAD")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rev-list --before: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func gitClone(ctx context.Context, env []string, dest, repoURL string, extraArgs ...string) error {
	_ = os.RemoveAll(dest)
	args := []string{"-c", "advice.detachedHead=false", "clone"}
	args = append(args, extraArgs...)
	args = append(args, repoURL, dest)
	cmd := exec.CommandContext(ctx, "git", args...)
	if env != nil {
		cmd.Env = append(append([]string{}, env...), "GIT_TERMINAL_PROMPT=0")
	} else {
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	}
	var errBuf bytes.Buffer
	cmd.Stdout = &errBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return nil
}
