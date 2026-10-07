package cli

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Cluster struct {
	Name    string  `yaml:"name"`
	Master  string  `yaml:"master"` // defaults to current user@hostname if empty
	Slaves  []Slave `yaml:"slaves"`
	EnvFile string  `yaml:"env_file"` // optional dotenv loaded before ${VAR} expansion
	// DataDir is the parent directory for run output (default: data).
	DataDir string `yaml:"data_dir"`
	// CollectOut is the scan name under DataDir for `cluster run` / collect.
	CollectOut string `yaml:"collect_out"`
	// AutoHeal: when true (default if any slave has gce_instance), unreachable
	// workers trigger stop→reset→deploy before / during remote scan.
	AutoHeal *bool `yaml:"auto_heal"`
}

type Slave struct {
	Name         string `yaml:"name"`
	Host         string `yaml:"host"` // hostname or IP
	User         string `yaml:"user"`
	Port         int    `yaml:"port"`
	Key          string `yaml:"key"` // identity file path
	WorkDir      string `yaml:"workdir"`
	TrufflesPath string `yaml:"truffles_path"` // path to truffles binary on remote (or "truffles" in PATH)
	// Optional GCE identity for `cluster reset` / `cluster heal`.
	GCEInstance string `yaml:"gce_instance"`
	GCEZone     string `yaml:"gce_zone"`
	GCEProject  string `yaml:"gce_project"`
	// GOARCH for cross-compile deploy (default amd64).
	Arch    string   `yaml:"arch"`
	Tags    []string `yaml:"tags"`
	Enabled bool     `yaml:"enabled"`
}

func defaultClusterName() string {
	hn, _ := os.Hostname()
	if hn == "" {
		hn = "localhost"
	}
	return fmt.Sprintf("cluster-%s", hn)
}

func genCluster(name string) *Cluster {
	if name == "" {
		name = defaultClusterName()
	}
	return &Cluster{
		Name:   name,
		Master: currentUserHost(),
		Slaves: []Slave{},
	}
}

func currentUserHost() string {
	u := os.Getenv("USER")
	if u == "" {
		u = os.Getenv("LOGNAME")
	}
	if u == "" {
		u = "user"
	}
	hn, _ := os.Hostname()
	if hn == "" {
		hn = "localhost"
	}
	return fmt.Sprintf("%s@%s", u, hn)
}

func writeCluster(c *Cluster, path string) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, b, 0644)
}

func loadCluster(path string) (*Cluster, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Cluster
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if ef := resolveEnvFile(path, c.EnvFile); ef != "" {
		if err := loadEnvFile(ef); err != nil {
			return nil, fmt.Errorf("env_file %s: %w", ef, err)
		}
	}
	expandCluster(&c)
	if c.Master == "" {
		c.Master = currentUserHost()
	}
	return &c, nil
}

func sshCmd(sl Slave, cmd string) *exec.Cmd {
	return sshCmdConnect(sl, cmd, 30)
}

func sshCmdConnect(sl Slave, cmd string, connectTimeoutSec int) *exec.Cmd {
	if connectTimeoutSec < 1 {
		connectTimeoutSec = 30
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", connectTimeoutSec),
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=no",
	}
	if sl.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", sl.Port))
	}
	if sl.Key != "" {
		args = append(args, "-i", sl.Key)
	}
	target := sl.Host
	if sl.User != "" {
		target = sl.User + "@" + sl.Host
	}
	args = append(args, target, cmd)
	return exec.Command("ssh", args...)
}

func slaveWorkDir(sl Slave) string {
	if sl.WorkDir != "" {
		return sl.WorkDir
	}
	return "~/truffles-work"
}

// ensureRemoteWorkDir creates the slave workdir over SSH (scp cannot mkdir parents).
func ensureRemoteWorkDir(sl Slave) error {
	wd := slaveWorkDir(sl)
	cmd := sshCmdConnect(sl, "mkdir -p "+shellSingleQuote(wd), 15)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("mkdir workdir %s: %w\n%s", wd, err, msg)
		}
		return fmt.Errorf("mkdir workdir %s: %w", wd, err)
	}
	return nil
}

func runScpTo(sl Slave, local, remote string) error {
	cmd := scpTo(sl, local, remote)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w\n%s", err, msg)
		}
		return err
	}
	return nil
}

func scpTo(sl Slave, local, remote string) *exec.Cmd {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "-o", "StrictHostKeyChecking=no"}
	if sl.Port > 0 {
		args = append(args, "-P", fmt.Sprintf("%d", sl.Port))
	}
	if sl.Key != "" {
		args = append(args, "-i", sl.Key)
	}
	args = append(args, local)
	target := sl.Host
	if sl.User != "" {
		target = sl.User + "@" + sl.Host
	}
	args = append(args, target+":"+remote)
	return exec.Command("scp", args...)
}

func scpFrom(sl Slave, remote, local string) *exec.Cmd {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "-o", "StrictHostKeyChecking=no"}
	if sl.Port > 0 {
		args = append(args, "-P", fmt.Sprintf("%d", sl.Port))
	}
	if sl.Key != "" {
		args = append(args, "-i", sl.Key)
	}
	target := sl.Host
	if sl.User != "" {
		target = sl.User + "@" + sl.Host
	}
	args = append(args, target+":"+remote, local)
	return exec.Command("scp", args...)
}

func splitLines(path string, n int) ([][]string, error) {
	if n <= 0 {
		n = 1
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	var nonempty []string
	for _, l := range lines {
		lt := strings.TrimSpace(l)
		if lt == "" || strings.HasPrefix(lt, "#") {
			continue
		}
		nonempty = append(nonempty, lt)
	}
	if len(nonempty) == 0 {
		return [][]string{nonempty}, nil
	}
	chunks := make([][]string, n)
	for i, l := range nonempty {
		chunks[i%n] = append(chunks[i%n], l)
	}
	return chunks, nil
}

func writeLines(dir, name string, lines []string) (string, error) {
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	w.Flush()
	return p, nil
}
