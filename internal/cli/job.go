package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	jobStatusRunning = "running"
	jobStatusExited  = "exited"
	jobStatusStopped = "stopped"
	defaultJobsDir   = "data/jobs"
)

// jobMeta is persisted under data/jobs/<id>/meta.json.
type jobMeta struct {
	ID         string `json:"id"`
	Playbook   string `json:"playbook"`
	PrefixFile string `json:"prefix_file,omitempty"`
	TermsFile  string `json:"terms_file,omitempty"`
	DataDir    string `json:"data_dir,omitempty"`
	Workers    int    `json:"workers,omitempty"`
	Mode       string `json:"mode"` // split | playbook
	PID        int    `json:"pid"`
	PGID       int    `json:"pgid,omitempty"`
	StartedAt  string `json:"started_at"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	LogPath    string `json:"log_path"`
	JobsDir    string `json:"jobs_dir,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	SleepSecs  int    `json:"sleep_secs,omitempty"`
	Once       bool   `json:"once,omitempty"`
	Cmd        string `json:"cmd,omitempty"`
}

func runJob(args []string) error {
	if len(args) == 0 {
		return jobUsage()
	}
	switch args[0] {
	case "start":
		return jobStart(args[1:])
	case "run":
		return jobRun(args[1:])
	case "list", "ls":
		return jobList(args[1:])
	case "status":
		return jobStatus(args[1:])
	case "logs", "log", "tail":
		return jobLogs(args[1:])
	case "stop", "kill":
		return jobStop(args[1:])
	case "help", "-h", "--help", "-help":
		return jobUsage()
	default:
		return fmt.Errorf("unknown job command %q — try `truffles job help`", args[0])
	}
}

func jobUsage() error {
	fmt.Fprint(os.Stderr, `truffles job — manage continuous playbook loops

usage:
  truffles job start -c <playbook.yaml> [flags]
  truffles job list
  truffles job status [id] [--full]
  truffles job logs <id> [-f] [-n N]
  truffles job stop <id>

start flags:
  -c, --playbook-file <yaml>   Playbook (required)
  --prefix-file <file>         Code-search prefixes (default examples/prefixes.txt)
  --terms-file <file>          Search terms file
  --seed <n>                   Shuffle seed (0 = random each cycle)
  --limit <n>                  New repos per cycle (default 500)
  --sleep <secs>               Pause between cycles (default 60)
  --full-playbook              Run truffles playbook each cycle (not search+cluster)
  --once                       Single cycle then exit
  --jobs-dir <dir>             Registry root (default data/jobs)
  --data-dir <dir>             Data directory (default data)

status flags:
  --full                       Per-worker CPU/mem/disk + GCE machine type when set
  --color auto|always|never    Colour (default auto)

Jobs run until stop. The loop is in-process (job run); no scripts/ checkout needed.
On macOS, start wraps with caffeinate -dims when available.
`)
	return nil
}

func jobsRoot(override string) string {
	if override != "" {
		return override
	}
	return defaultJobsDir
}

func newJobID(t time.Time) string {
	return "j-" + t.Format("20060102-150405")
}

func jobDir(root, id string) string {
	return filepath.Join(root, id)
}

func jobMetaPath(root, id string) string {
	return filepath.Join(jobDir(root, id), "meta.json")
}

func jobLogPath(root, id string) string {
	return filepath.Join(jobDir(root, id), "job.log")
}

func jobPIDPath(root, id string) string {
	return filepath.Join(jobDir(root, id), "pid")
}

func saveJobMeta(root string, m *jobMeta) error {
	dir := jobDir(root, m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m.JobsDir = root
	m.LogPath = jobLogPath(root, m.ID)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := jobMetaPath(root, m.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, jobMetaPath(root, m.ID))
}

// resolveJobID maps a job id (j-…) or numeric pid to a registry id.
func resolveJobID(root, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("job id required")
	}
	if strings.HasPrefix(id, "j-") {
		return id, nil
	}
	if pid, err := strconv.Atoi(id); err == nil && pid > 0 {
		ids, err := listJobIDs(root)
		if err != nil {
			return "", err
		}
		for _, jid := range ids {
			m, err := loadJobMeta(root, jid)
			if err != nil {
				continue
			}
			if m.PID == pid {
				return jid, nil
			}
		}
		return "", fmt.Errorf("no job with pid %d — run: truffles job list", pid)
	}
	return id, nil
}

func loadJobMeta(root, id string) (*jobMeta, error) {
	b, err := os.ReadFile(jobMetaPath(root, id))
	if err != nil {
		return nil, err
	}
	var m jobMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.ID == "" {
		m.ID = id
	}
	return &m, nil
}

func listJobIDs(root string) ([]string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !strings.HasPrefix(id, "j-") {
			continue
		}
		if _, err := os.Stat(jobMetaPath(root, id)); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// pidAlive reports whether pid is a live process (signal 0).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

// refreshJobStatus updates meta if a "running" job's pid is dead.
func refreshJobStatus(root string, m *jobMeta) (*jobMeta, error) {
	if m.Status != jobStatusRunning {
		return m, nil
	}
	if pidAlive(m.PID) {
		return m, nil
	}
	m.Status = jobStatusExited
	code := -1
	m.ExitCode = &code
	if err := saveJobMeta(root, m); err != nil {
		return m, err
	}
	return m, nil
}

func jobStart(args []string) error {
	fs := flag.NewFlagSet("job start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	playbook := fs.String("c", "", "playbook YAML")
	fs.StringVar(playbook, "playbook-file", "", "playbook YAML")
	prefixFile := fs.String("prefix-file", "examples/prefixes.txt", "prefix file for search mode")
	termsFile := fs.String("terms-file", "", "search terms file (see truffles search -terms-file)")
	searchSeed := fs.Int64("seed", 0, "search shuffle seed (0 = random each cycle)")
	limit := fs.Int("limit", 500, "repos per cycle")
	sleepSecs := fs.Int("sleep", 60, "seconds between cycles")
	fullPlaybook := fs.Bool("full-playbook", false, "use truffles playbook each cycle")
	once := fs.Bool("once", false, "single cycle then exit")
	jobsDir := fs.String("jobs-dir", defaultJobsDir, "job registry directory")
	skipFile := fs.String("skip-file", "", "override skip file")
	cleanFile := fs.String("clean-file", "", "override clean file")
	workers := fs.Int("workers", 8, "search workers")
	dataDir := fs.String("data-dir", "data", "data directory")
	bin := fs.String("bin", "", "truffles binary (default: this executable)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	// Positional playbook if -c omitted.
	if *playbook == "" && fs.NArg() > 0 {
		*playbook = fs.Arg(0)
	}
	if strings.TrimSpace(*playbook) == "" {
		return fmt.Errorf("playbook required: truffles job start -c remote-playbook.yaml")
	}
	if _, err := os.Stat(*playbook); err != nil {
		return fmt.Errorf("playbook %s: %w", *playbook, err)
	}

	root, err := filepath.Abs(*jobsDir)
	if err != nil {
		return err
	}
	now := time.Now()
	id := newJobID(now)
	// Collision: bump seconds.
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(jobDir(root, id)); os.IsNotExist(err) {
			break
		}
		now = now.Add(time.Second)
		id = newJobID(now)
	}
	if err := os.MkdirAll(jobDir(root, id), 0o755); err != nil {
		return err
	}

	mode := "split"
	if *fullPlaybook {
		mode = "playbook"
	}

	trufflesBin := strings.TrimSpace(*bin)
	if trufflesBin == "" {
		trufflesBin, err = selfExecutable()
		if err != nil {
			return fmt.Errorf("resolve self binary: %w", err)
		}
	} else if p, err := filepath.Abs(trufflesBin); err == nil {
		if _, err := os.Stat(p); err == nil {
			trufflesBin = p
		}
	}

	// Detach: this binary runs `job run` (in-process loop; no scripts/run-loop.sh).
	loopArgs := []string{trufflesBin, "job", "run", "-c", *playbook,
		"--limit", strconv.Itoa(*limit),
		"--sleep", strconv.Itoa(*sleepSecs),
		"--workers", strconv.Itoa(*workers),
		"--data-dir", *dataDir,
	}
	if strings.TrimSpace(*prefixFile) != "" {
		loopArgs = append(loopArgs, "--prefix-file", *prefixFile)
	}
	if strings.TrimSpace(*termsFile) != "" {
		loopArgs = append(loopArgs, "--terms-file", *termsFile)
	}
	if *searchSeed != 0 {
		loopArgs = append(loopArgs, "--seed", strconv.FormatInt(*searchSeed, 10))
	}
	if *fullPlaybook {
		loopArgs = append(loopArgs, "--full-playbook")
	}
	if *once {
		loopArgs = append(loopArgs, "--once")
	}
	if *skipFile != "" {
		loopArgs = append(loopArgs, "--skip-file", *skipFile)
	}
	if *cleanFile != "" {
		loopArgs = append(loopArgs, "--clean-file", *cleanFile)
	}

	logPath := jobLogPath(root, id)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	cmd, cmdLine, err := buildJobCommand(loopArgs)
	if err != nil {
		logf.Close()
		return err
	}
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Dir, _ = os.Getwd()
	cmd.Env = append(os.Environ(),
		"JOB_ID="+id,
	)
	setJobSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		logf.Close()
		return fmt.Errorf("start job: %w", err)
	}
	pid := cmd.Process.Pid
	pgid := pid
	if err := os.WriteFile(jobPIDPath(root, id), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		logf.Close()
		return err
	}
	// Parent must not wait; release so we don't zombie when child exits.
	go func() {
		_, _ = cmd.Process.Wait()
		logf.Close()
	}()

	meta := &jobMeta{
		ID:         id,
		Playbook:   *playbook,
		PrefixFile: *prefixFile,
		TermsFile:  *termsFile,
		DataDir:    *dataDir,
		Workers:    *workers,
		Mode:       mode,
		PID:        pid,
		PGID:       pgid,
		StartedAt:  now.UTC().Format(time.RFC3339),
		Status:     jobStatusRunning,
		LogPath:    logPath,
		Limit:      *limit,
		SleepSecs:  *sleepSecs,
		Once:       *once,
		Cmd:        cmdLine,
	}
	if err := saveJobMeta(root, meta); err != nil {
		return err
	}

	fmt.Printf("started job %s (pid=%d)\n", id, pid)
	CoreLog("job", fmt.Sprintf("start  id=%s pid=%d playbook=%s prefix=%s", id, pid, *playbook, *prefixFile))
	fmt.Printf("playbook: %s\n", *playbook)
	fmt.Printf("log:      %s\n", logPath)
	fmt.Printf("status:   truffles job status %s\n", id)
	fmt.Printf("logs:     truffles job logs %s -f\n", id)
	fmt.Printf("stop:     truffles job stop %s\n", id)
	return nil
}

// buildJobCommand wraps run-loop with caffeinate on macOS when available.
func buildJobCommand(loopArgs []string) (*exec.Cmd, string, error) {
	if len(loopArgs) == 0 {
		return nil, "", fmt.Errorf("empty command")
	}
	if runtime.GOOS == "darwin" {
		if caff, err := exec.LookPath("caffeinate"); err == nil {
			args := append([]string{"-dims"}, loopArgs...)
			cmd := exec.Command(caff, args...)
			return cmd, strings.Join(append([]string{caff, "-dims"}, loopArgs...), " "), nil
		}
	}
	cmd := exec.Command(loopArgs[0], loopArgs[1:]...)
	return cmd, strings.Join(loopArgs, " "), nil
}

// parseJobIDArgs extracts optional --jobs-dir and a required job id from mixed argv.
func parseJobIDArgs(args []string, needID bool) (id, jobsDir string, err error) {
	jobsDir = defaultJobsDir
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "-help":
			return "", "", flag.ErrHelp
		case a == "--jobs-dir" || a == "-jobs-dir":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s requires a value", a)
			}
			jobsDir = args[i+1]
			i++
		case strings.HasPrefix(a, "--jobs-dir="):
			jobsDir = strings.TrimPrefix(a, "--jobs-dir=")
		case strings.HasPrefix(a, "-"):
			return "", "", fmt.Errorf("unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	if needID {
		if len(pos) < 1 {
			return "", "", fmt.Errorf("job id required")
		}
		id = pos[0]
	}
	return id, jobsDir, nil
}

func jobList(args []string) error {
	_, jobsDir, err := parseJobIDArgs(args, false)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return jobUsage()
		}
		return err
	}
	root, err := filepath.Abs(jobsDir)
	if err != nil {
		return err
	}
	ids, err := listJobIDs(root)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("no jobs (start with: truffles job start -c remote-playbook.yaml)")
		return nil
	}
	fmt.Printf("%-20s %-8s %7s  %-28s  %s\n", "ID", "STATUS", "PID", "PLAYBOOK", "PREFIX")
	for _, id := range ids {
		m, err := loadJobMeta(root, id)
		if err != nil {
			continue
		}
		m, _ = refreshJobStatus(root, m)
		pb := filepath.Base(m.Playbook)
		pfx := filepath.Base(m.PrefixFile)
		if pfx == "." || pfx == "" {
			pfx = "-"
		}
		fmt.Printf("%-20s %-8s %7d  %-28s  %s\n", m.ID, m.Status, m.PID, pb, pfx)
	}
	return nil
}

func jobLogs(args []string) error {
	jobsDir := defaultJobsDir
	follow := false
	lines := 50
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			return jobUsage()
		case a == "-f" || a == "--follow":
			follow = true
		case a == "-n" || a == "--lines":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return err
			}
			lines = n
			i++
		case strings.HasPrefix(a, "-n="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "-n="))
			if err != nil {
				return err
			}
			lines = n
		case a == "--jobs-dir" || a == "-jobs-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			jobsDir = args[i+1]
			i++
		case strings.HasPrefix(a, "--jobs-dir="):
			jobsDir = strings.TrimPrefix(a, "--jobs-dir=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) < 1 {
		return fmt.Errorf("usage: truffles job logs <id> [-f] [-n N]")
	}
	id := pos[0]
	root, err := filepath.Abs(jobsDir)
	if err != nil {
		return err
	}
	id, err = resolveJobID(root, id)
	if err != nil {
		return err
	}
	m, err := loadJobMeta(root, id)
	if err != nil {
		return fmt.Errorf("job %s: %w", id, err)
	}
	if !follow {
		return printLogTail(m.LogPath, lines)
	}
	if err := printLogTail(m.LogPath, lines); err != nil && !os.IsNotExist(err) {
		return err
	}
	return followLog(m.LogPath)
}

func printLogTail(path string, n int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var ring []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		ring = append(ring, sc.Text())
		if len(ring) > n {
			ring = ring[len(ring)-n:]
		}
	}
	for _, line := range ring {
		fmt.Println(line)
	}
	return sc.Err()
}

func followLog(path string) error {
	var offset int64
	if st, err := os.Stat(path); err == nil {
		// Start from end after initial tail already printed.
		offset = st.Size()
	}
	for {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		size := st.Size()
		if size < offset {
			offset = 0 // truncated
		}
		if size > offset {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				f.Close()
				return err
			}
			n, err := io.Copy(os.Stdout, f)
			offset += n
			if err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
		time.Sleep(400 * time.Millisecond)
	}
}

func jobStop(args []string) error {
	id, jobsDir, err := parseJobIDArgs(args, true)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return jobUsage()
		}
		return fmt.Errorf("usage: truffles job stop <id>: %w", err)
	}
	root, err := filepath.Abs(jobsDir)
	if err != nil {
		return err
	}
	id, err = resolveJobID(root, id)
	if err != nil {
		return err
	}
	m, err := loadJobMeta(root, id)
	if err != nil {
		return fmt.Errorf("job %s: %w (try: truffles job list)", id, err)
	}
	m, _ = refreshJobStatus(root, m)
	if m.Status != jobStatusRunning || !pidAlive(m.PID) {
		m.Status = jobStatusStopped
		_ = saveJobMeta(root, m)
		fmt.Printf("job %s already not running (status=%s)\n", id, m.Status)
		return nil
	}

	pgid := m.PGID
	if pgid <= 0 {
		pgid = m.PID
	}
	fmt.Printf("stopping job %s (pid=%d pgid=%d)…\n", id, m.PID, pgid)
	_ = terminateJobProcessGroup(pgid)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(m.PID) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if pidAlive(m.PID) {
		_ = killJobProcessGroup(pgid)
		time.Sleep(300 * time.Millisecond)
	}
	m.Status = jobStatusStopped
	code := -1
	if !pidAlive(m.PID) {
		code = 0
	}
	m.ExitCode = &code
	if err := saveJobMeta(root, m); err != nil {
		return err
	}
	fmt.Printf("stopped job %s\n", id)
	CoreLog("job", fmt.Sprintf("stop   id=%s pid=%d", id, m.PID))
	return nil
}
