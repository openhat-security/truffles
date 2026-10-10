package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// remoteWorkerArtifactNames are result files written per slave during cluster scan.
// Collect and incremental mirror both use this list.
func remoteWorkerArtifactNames(sl Slave) []string {
	return []string{
		fmt.Sprintf("results-%s.csv", sl.Name),
		fmt.Sprintf("results-%s.txt", sl.Name),
		fmt.Sprintf("results-%s.jsonl", sl.Name),
		fmt.Sprintf("scanned-%s.txt", sl.Name),
	}
}

// mirrorPullInterval aligns with scan progress when set; otherwise 30s.
func mirrorPullInterval(sc ScanConfig) time.Duration {
	if sc.Progress != "" {
		if d, err := time.ParseDuration(sc.Progress); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

func pullRemoteWorkerFiles(sl Slave, wd, localDir string) {
	for _, name := range remoteWorkerArtifactNames(sl) {
		remote := filepath.Join(wd, name)
		local := filepath.Join(localDir, name)
		_ = scpFrom(sl, remote, local).Run()
	}
}

func workerScanLogPath(localDir, slaveName string) string {
	return filepath.Join(localDir, fmt.Sprintf("scan-%s.log", slaveName))
}

// remoteScanCommand is the shell command run on a slave via SSH.
func remoteScanCommand(wd string, sl Slave, workers int, format, ext string, sc ScanConfig, useSkip bool) string {
	scanCmd := fmt.Sprintf("cd %s && %s scan -f repos.txt -workers %d -format %s -out results-%s.%s",
		wd, binOrPath(sl.TrufflesPath), workers, format, sl.Name, ext)
	scanCmd += " " + scanNoProxyFlag(sc.NoProxy)
	if sc.PoolSize > 0 {
		scanCmd += fmt.Sprintf(" -pool-size %d", sc.PoolSize)
	}
	if sc.PoolWait != "" {
		scanCmd += " -pool-wait " + sc.PoolWait
	}
	if sc.UseDirect != nil && !*sc.UseDirect {
		scanCmd += " -no-direct"
	}
	if len(sc.ExcludePaths) > 0 {
		scanCmd += " -exclude-paths " + shellSingleQuote(strings.Join(sc.ExcludePaths, ","))
	}
	if toks := scanAuthTokens(sc); len(toks) > 0 {
		scanCmd += " -token " + shellSingleQuote(strings.Join(toks, ","))
	}
	if sc.NoVerification {
		scanCmd += " -no-verification"
	}
	if sc.MaxDepth > 0 {
		scanCmd += fmt.Sprintf(" -max-depth %d", sc.MaxDepth)
	}
	if useSkip {
		scanCmd += " -skip-file skip-repos.txt"
	}
	if sc.AppendScanned != "" {
		scanCmd += fmt.Sprintf(" -append-scanned scanned-%s.txt", sl.Name)
	}
	if sc.Progress != "" {
		scanCmd += " -progress " + shellSingleQuote(sc.Progress)
	}
	return scanCmd
}

type workerStage struct {
	sl Slave
	wd string
}

// mirrorWorkerResultsDuringRun SCP-pulls growing worker CSVs (and related artifacts)
// into localDir until ctx is cancelled.
func mirrorWorkerResultsDuringRun(ctx context.Context, workers []workerStage, localDir string, sc ScanConfig) {
	if localDir == "" || len(workers) == 0 {
		return
	}
	interval := mirrorPullInterval(sc)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pull := func() {
		for _, w := range workers {
			pullRemoteWorkerFiles(w.sl, w.wd, localDir)
		}
	}
	pull()
	for {
		select {
		case <-ctx.Done():
			pull()
			return
		case <-ticker.C:
			pull()
		}
	}
}
