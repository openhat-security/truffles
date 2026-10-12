package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultCoreLogPath = "data/truffles.log"

var (
	coreLogPath string
	coreLogOnce sync.Once
	coreLogMu   sync.Mutex
)

// CoreLogPath returns the active core log file path.
func CoreLogPath() string {
	if coreLogPath != "" {
		return coreLogPath
	}
	return defaultCoreLogPath
}

// SetCoreLogPath overrides the log destination (tests).
func SetCoreLogPath(path string) {
	coreLogPath = path
}

func absPathForLog(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// CoreLog appends one human-readable line to data/truffles.log (never verbose proxy noise).
func CoreLog(category, message string) {
	if category == "" {
		category = "truffles"
	}
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("%s  %-14s  %s\n", ts, category, message)

	coreLogMu.Lock()
	defer coreLogMu.Unlock()

	path := CoreLogPath()
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Sync()
	_ = f.Close()
}

func countURLLines(path string) int {
	if path == "" {
		return 0
	}
	set, err := loadSkipSet(path)
	if err != nil {
		return 0
	}
	return len(set)
}

func countCleanEntries(path string) int {
	m, err := loadCleanMap(path)
	if err != nil {
		return 0
	}
	return len(m)
}

// CoreLogScannedMerge logs a bulk update to scanned-repos (or append_scanned master).
func CoreLogScannedMerge(master string, added int, source string) {
	if master == "" {
		return
	}
	src := source
	if src == "" {
		src = "merge"
	}
	CoreLog("scanned-repos", fmt.Sprintf("%s  +%d urls  total≈%d  %s",
		src, added, countURLLines(master), absPathForLog(master)))
}

// CoreLogCleanMerge logs a bulk update to clean-repos.
func CoreLogCleanMerge(master string, added int, source string) {
	if master == "" {
		return
	}
	src := source
	if src == "" {
		src = "merge"
	}
	CoreLog("clean-repos", fmt.Sprintf("%s  +%d repos  total=%d  %s",
		src, added, countCleanEntries(master), absPathForLog(master)))
}
