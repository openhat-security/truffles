package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultDataRoot = "data"
const defaultScanName = "remote-results"

// uniqueCollectDir returns a free directory under root for one scan run.
// requested is a logical name (e.g. "remote-results"). If that directory
// already exists, the next free suffix is used: name-1, name-2, …
func uniqueCollectDir(root, requested string) (string, error) {
	base := normalizeDataDir(root, requested)
	for n := 0; n < 10000; n++ {
		candidate := base
		if n > 0 {
			candidate = fmt.Sprintf("%s-%d", base, n)
		}
		_, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(candidate, 0755); err != nil {
				return "", err
			}
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no free collect dir under %s (tried up to %s-9999)", base, base)
}

// normalizeDataDir joins root + scan name, avoiding nested data/data/….
func normalizeDataDir(root, requested string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = defaultDataRoot
	}
	root = filepath.Clean(root)

	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = defaultScanName
	}
	requested = filepath.Clean(requested)

	// Absolute scan paths: keep basename under root.
	if filepath.IsAbs(requested) {
		return filepath.Join(root, filepath.Base(requested))
	}

	// If caller passed "data/foo" and root is "data", strip the root prefix.
	rel := requested
	rootPrefix := root + string(os.PathSeparator)
	if rel == root {
		rel = defaultScanName
	} else if strings.HasPrefix(rel, rootPrefix) {
		rel = strings.TrimPrefix(rel, rootPrefix)
	} else if root == defaultDataRoot && strings.HasPrefix(rel, defaultDataRoot+string(os.PathSeparator)) {
		rel = strings.TrimPrefix(rel, defaultDataRoot+string(os.PathSeparator))
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		rel = defaultScanName
	}
	return filepath.Join(root, rel)
}

type runLayout struct {
	DataDir    string
	ScanName   string
	CollectDir string
}

type runLayoutOpts struct {
	DataDirFlag string
	OutFlag     string
	YAMLDataDir string
	YAMLOut     string
	Prompt      bool // ask on TTY when true
}

func resolveRunLayout(opts runLayoutOpts) (runLayout, error) {
	dataDir := firstNonEmpty(opts.DataDirFlag, opts.YAMLDataDir, defaultDataRoot)
	scanName := firstNonEmpty(opts.OutFlag, opts.YAMLOut, defaultScanName)

	if opts.Prompt && isInteractiveTerminal() {
		var err error
		dataDir, err = promptLine("Data directory", dataDir)
		if err != nil {
			return runLayout{}, err
		}
		scanName, err = promptLine("Scan name", scanName)
		if err != nil {
			return runLayout{}, err
		}
		dataDir = firstNonEmpty(strings.TrimSpace(dataDir), defaultDataRoot)
		scanName = firstNonEmpty(strings.TrimSpace(scanName), defaultScanName)
	}

	collectDir, err := uniqueCollectDir(dataDir, scanName)
	if err != nil {
		return runLayout{}, err
	}
	return runLayout{DataDir: dataDir, ScanName: scanName, CollectDir: collectDir}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func isInteractiveTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func promptLine(label, def string) (string, error) {
	fmt.Printf("%s [%s]: ", label, def)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && len(strings.TrimSpace(line)) == 0 {
		return def, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}
