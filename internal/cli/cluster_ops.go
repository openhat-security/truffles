package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func clusterStop(rest []string) error {
	fs := flag.NewFlagSet("cluster stop", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster or playbook YAML")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print("truffles cluster stop -c <cluster|playbook.yaml>\n")
			return nil
		}
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	stopLocalPlaybook(*cfg)
	return clusterStopOn(c)
}

func clusterReset(rest []string) error {
	fs := flag.NewFlagSet("cluster reset", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster or playbook YAML")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print("truffles cluster reset -c <cluster|playbook.yaml>\n")
			return nil
		}
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	return clusterResetOn(c)
}

func clusterDeploy(rest []string) error {
	fs := flag.NewFlagSet("cluster deploy", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster or playbook YAML")
	local := fs.String("bin", "", "local binary to upload (default: cross-compile linux)")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print("truffles cluster deploy -c <cluster|playbook.yaml> [-bin path]\n")
			return nil
		}
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}
	return clusterDeployOn(c, *local)
}

// clusterHeal stops local/remote scans, resets GCE workers, waits for SSH, deploys.
func clusterHeal(rest []string) error {
	fs := flag.NewFlagSet("cluster heal", flag.ContinueOnError)
	cfg := fs.String("c", "cluster.yaml", "cluster or playbook YAML")
	skipReset := fs.Bool("no-reset", false, "Skip gcloud instance reset")
	skipDeploy := fs.Bool("no-deploy", false, "Skip binary deploy")
	wait := fs.Duration("wait", 3*time.Minute, "How long to wait for SSH after reset")
	local := fs.String("bin", "", "local binary to upload (default: cross-compile linux)")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(`truffles cluster heal -c <cluster|playbook.yaml>

Stop local playbook + remote scans, gcloud reset workers (gce_*),
wait for SSH, cross-compile linux/amd64 and deploy.

flags:
  -c string        cluster or playbook YAML
  -no-reset        skip gcloud reset
  -no-deploy       skip binary deploy
  -wait duration   SSH wait after reset (default 3m)
  -bin path        upload this binary instead of building

alias: truffles playbook heal -f <playbook.yaml>
`)
			return nil
		}
		return err
	}
	c, err := loadCluster(*cfg)
	if err != nil {
		return err
	}

	fmt.Println("[*] stopping local playbook / remote scans")
	stopLocalPlaybook(*cfg)
	if err := clusterHealOn(c, healOpts{
		Wait:       *wait,
		SkipReset:  *skipReset,
		SkipDeploy: *skipDeploy,
		Bin:        *local,
		StopRemote: true,
	}); err != nil {
		return err
	}
	fmt.Printf("    re-run: truffles playbook -f %s\n", *cfg)
	return nil
}

// healOpts controls clusterHealOn. Auto-heal must leave StopLocal alone (the
// running playbook is the caller); explicit `cluster heal` kills it first.
type healOpts struct {
	Wait       time.Duration
	SkipReset  bool
	SkipDeploy bool
	Bin        string
	StopRemote bool
}

func clusterHealOn(c *Cluster, opt healOpts) error {
	if opt.Wait <= 0 {
		opt.Wait = 3 * time.Minute
	}
	if opt.StopRemote {
		fmt.Println("[*] stopping remote scans")
		_ = clusterStopOn(c)
	}
	if !opt.SkipReset {
		fmt.Println("[*] resetting GCE workers")
		if err := clusterResetOn(c); err != nil {
			return err
		}
	}
	fmt.Printf("[*] waiting for SSH (up to %s)\n", opt.Wait.Round(time.Second))
	if err := clusterWaitSSH(c, opt.Wait); err != nil {
		return err
	}
	if !opt.SkipDeploy {
		fmt.Println("[*] deploying truffles binary")
		if err := clusterDeployOn(c, opt.Bin); err != nil {
			return err
		}
	}
	fmt.Println("[+] heal done")
	return nil
}

// clusterAutoHealEnabled is true when auto_heal: true, or when unset and at
// least one enabled slave has gce_instance (so reset is possible).
func clusterAutoHealEnabled(c *Cluster) bool {
	if c.AutoHeal != nil {
		return *c.AutoHeal
	}
	for _, sl := range enabledSlaves(c) {
		if sl.GCEInstance != "" {
			return true
		}
	}
	return false
}

func slaveSSHReady(sl Slave) bool {
	cmd := sshCmdConnect(sl, "echo ok", 8)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), "ok")
}

func clusterUnreachable(c *Cluster) []Slave {
	var down []Slave
	for _, sl := range enabledSlaves(c) {
		if !slaveSSHReady(sl) {
			down = append(down, sl)
		}
	}
	return down
}

func clusterHasGCE(c *Cluster) bool {
	for _, sl := range enabledSlaves(c) {
		if sl.GCEInstance != "" {
			return true
		}
	}
	return false
}

// clusterEnsureReady probes SSH; if workers are down and auto-heal is on,
// resets/deploys once. Also deploys when the remote binary is missing modern
// scan flags (stale install). Does not kill the local playbook process.
func clusterEnsureReady(c *Cluster) error {
	down := clusterUnreachable(c)
	if len(down) > 0 {
		names := make([]string, len(down))
		for i, sl := range down {
			names[i] = sl.Name
		}
		if !clusterAutoHealEnabled(c) {
			return fmt.Errorf("worker SSH down (%s); enable auto_heal or run: truffles cluster heal -c …",
				strings.Join(names, ", "))
		}
		if !clusterHasGCE(c) {
			return fmt.Errorf("worker SSH down (%s) and no gce_instance set for auto-heal",
				strings.Join(names, ", "))
		}
		fmt.Printf("[!] worker unreachable (%s) — auto-healing\n", strings.Join(names, ", "))
		return clusterHealOn(c, healOpts{StopRemote: true, Wait: 3 * time.Minute})
	}
	return clusterEnsureFreshBinary(c)
}

// clusterEnsureFreshBinary deploys when a reachable worker's scan help lacks
// -skip-file (pre-dual-report / skip-list builds).
func clusterEnsureFreshBinary(c *Cluster) error {
	if !clusterAutoHealEnabled(c) {
		return nil
	}
	var stale []Slave
	for _, sl := range enabledSlaves(c) {
		if !remoteScanHasFlag(sl, "-skip-file") {
			stale = append(stale, sl)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	names := make([]string, len(stale))
	for i, sl := range stale {
		names[i] = sl.Name
	}
	fmt.Printf("[!] remote binary outdated (%s) — deploying\n", strings.Join(names, ", "))
	return clusterDeployOn(c, "")
}

func remoteScanHasFlag(sl Slave, flagName string) bool {
	bin := binOrPath(sl.TrufflesPath)
	cmd := sshCmdConnect(sl, bin+" scan -h 2>&1", 15)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Missing binary or broken install → treat as stale.
		return false
	}
	return strings.Contains(string(out), flagName)
}

func isSSHTransportErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{
		"timeout",
		"timed out",
		"not responding",
		"connection refused",
		"connection reset",
		"connection closed",
		"broken pipe",
		"no route to host",
		"network is unreachable",
		"connection timed out",
		"ssh: handshake",
		"i/o timeout",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func isStaleBinaryErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "flag provided but not defined") ||
		strings.Contains(s, "unknown flag")
}

// clusterShouldAutoRecover is true for errors that a deploy/reset can fix.
func clusterShouldAutoRecover(err error) bool {
	return isSSHTransportErr(err) || isStaleBinaryErr(err)
}

func stopLocalPlaybook(cfg string) {
	base := filepath.Base(cfg)
	patterns := []string{
		"truffles playbook -f " + cfg,
		"truffles playbook -f " + base,
		"./truffles playbook -f " + cfg,
		"./truffles playbook -f " + base,
	}
	for _, p := range patterns {
		cmd := exec.Command("pkill", "-INT", "-f", p)
		_ = cmd.Run()
	}
}

func clusterStopOn(c *Cluster) error {
	var last error
	for _, sl := range enabledSlaves(c) {
		// Bracket letters so pkill does not match its own command line.
		script := "pkill -INT -f '[t]ruffles scan' 2>/dev/null || true; " +
			"pkill -TERM -f '[t]rufflehog' 2>/dev/null || true; " +
			"sleep 1; " +
			"pkill -KILL -f '[t]ruffles scan' 2>/dev/null || true; " +
			"pkill -KILL -f '[t]rufflehog' 2>/dev/null || true; " +
			"echo stopped"
		cmd := sshCmdConnect(sl, script, 10)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[!] stop %s: %v (host may be wedged — use reset)\n", sl.Name, err)
			last = err
			continue
		}
		fmt.Printf("[+] stopped scans on %s\n", sl.Name)
	}
	return last
}

func clusterResetOn(c *Cluster) error {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return fmt.Errorf("gcloud not on PATH (needed for reset): %w", err)
	}
	n := 0
	for _, sl := range enabledSlaves(c) {
		if sl.GCEInstance == "" {
			continue
		}
		n++
		if err := gceReset(sl); err != nil {
			return fmt.Errorf("reset %s: %w", sl.Name, err)
		}
		fmt.Printf("[+] reset %s (%s)\n", sl.Name, sl.GCEInstance)
	}
	if n == 0 {
		return fmt.Errorf("no slaves have gce_instance set — add gce_instance/gce_zone/gce_project (or env) to the YAML")
	}
	return nil
}

func gceReset(sl Slave) error {
	zone := sl.GCEZone
	if zone == "" {
		return fmt.Errorf("gce_zone required for instance %s", sl.GCEInstance)
	}
	args := []string{"compute", "instances", "reset", sl.GCEInstance, "--zone=" + zone, "--quiet"}
	if sl.GCEProject != "" {
		args = append(args, "--project="+sl.GCEProject)
	}
	cmd := exec.Command("gcloud", args...)
	cmd.Env = gcloudUserCLIEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n(hint: gcloud auth login  # user creds for gcloud CLI, not application-default)", err)
	}
	return nil
}

// gcloudUserCLIEnv is the process env for gcloud CLI calls with ADC stripped so
// a stale GOOGLE_APPLICATION_CREDENTIALS file cannot override `gcloud auth login`.
func gcloudUserCLIEnv() []string {
	base := os.Environ()
	out := make([]string, 0, len(base))
	for _, e := range base {
		key, _, _ := strings.Cut(e, "=")
		switch key {
		case "GOOGLE_APPLICATION_CREDENTIALS",
			"CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE",
			"CLOUDSDK_AUTH_ACCESS_TOKEN":
			continue
		}
		out = append(out, e)
	}
	return out
}

func clusterWaitSSH(c *Cluster, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for _, sl := range enabledSlaves(c) {
		fmt.Printf("[*] waiting for %s (%s)\n", sl.Name, sl.Host)
		for {
			cmd := sshCmdConnect(sl, "echo ok", 8)
			if out, err := cmd.CombinedOutput(); err == nil && strings.Contains(string(out), "ok") {
				fmt.Printf("[+] ssh ready: %s\n", sl.Name)
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("ssh to %s still down after %s", sl.Name, timeout.Round(time.Second))
			}
			time.Sleep(5 * time.Second)
		}
	}
	return nil
}

func clusterDeployOn(c *Cluster, localBin string) error {
	built := map[string]string{} // arch -> path
	cleanup := []string{}
	defer func() {
		for _, p := range cleanup {
			_ = os.Remove(p)
		}
	}()

	for _, sl := range enabledSlaves(c) {
		bin := localBin
		if bin == "" {
			arch := sl.Arch
			if arch == "" {
				arch = "amd64"
			}
			if p, ok := built[arch]; ok {
				bin = p
			} else {
				p, err := buildLinuxTruffles(arch)
				if err != nil {
					return err
				}
				built[arch] = p
				cleanup = append(cleanup, p)
				bin = p
			}
		}
		if err := deployBinary(sl, bin); err != nil {
			return fmt.Errorf("deploy %s: %w", sl.Name, err)
		}
		fmt.Printf("[+] deployed to %s (%s)\n", sl.Name, remoteTrufflesPath(sl))
		if err := ensureRemoteTrufflehog(sl); err != nil {
			return fmt.Errorf("trufflehog %s: %w", sl.Name, err)
		}
	}
	return nil
}

// ensureRemoteTrufflehog installs trufflehog into /usr/local/bin when missing.
func ensureRemoteTrufflehog(sl Slave) error {
	check := `if command -v trufflehog >/dev/null 2>&1; then trufflehog --version; exit 0; fi; exit 2`
	cmd := sshCmdConnect(sl, check, 20)
	out, err := cmd.CombinedOutput()
	if err == nil {
		ver := strings.TrimSpace(string(out))
		if ver != "" {
			fmt.Printf("[+] trufflehog on %s: %s\n", sl.Name, ver)
		}
		return nil
	}
	fmt.Printf("[*] installing trufflehog on %s…\n", sl.Name)
	install := `set -euo pipefail
curl -sSfL https://raw.githubusercontent.com/trufflesecurity/trufflehog/main/scripts/install.sh | sudo bash -s -- -b /usr/local/bin
trufflehog --version`
	cmd = sshCmdConnect(sl, install, 120)
	out, err = cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg != "" {
			return fmt.Errorf("%w\n%s", err, msg)
		}
		return err
	}
	if msg != "" {
		fmt.Printf("[+] trufflehog on %s: %s\n", sl.Name, msg)
	} else {
		fmt.Printf("[+] trufflehog installed on %s\n", sl.Name)
	}
	return nil
}

func remoteTrufflesPath(sl Slave) string {
	if sl.TrufflesPath != "" && sl.TrufflesPath != "truffles" {
		return sl.TrufflesPath
	}
	return "/usr/local/bin/truffles"
}

func moduleRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", fmt.Errorf("go list -m: %w (run from the truffles module)", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func buildLinuxTruffles(arch string) (string, error) {
	if arch == "" {
		arch = "amd64"
	}
	root, err := moduleRoot()
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "truffles-linux-"+arch+"-")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	_ = tmp.Close()

	cmd := exec.Command("go", "build", "-o", path, "./cmd/truffles")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH="+arch,
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("[*] building linux/%s (host %s/%s)\n", arch, runtime.GOOS, runtime.GOARCH)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func deployBinary(sl Slave, localBin string) error {
	if err := ensureRemoteWorkDir(sl); err != nil {
		return err
	}
	dest := remoteTrufflesPath(sl)
	tmp := "/tmp/truffles.new"
	scp := scpTo(sl, localBin, tmp)
	scp.Stdout = os.Stdout
	scp.Stderr = os.Stderr
	if err := scp.Run(); err != nil {
		return fmt.Errorf("scp: %w", err)
	}
	install := fmt.Sprintf("sudo mv %s %s && sudo chmod 755 %s && %s version", tmp, dest, dest, dest)
	if !needsSudoInstall(dest) {
		dir := filepath.Dir(dest)
		install = fmt.Sprintf("mkdir -p %s && mv %s %s && chmod 755 %s && %s version",
			shellSingleQuote(dir), tmp, shellSingleQuote(dest), shellSingleQuote(dest), shellSingleQuote(dest))
	}
	cmd := sshCmd(sl, install)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func needsSudoInstall(dest string) bool {
	return strings.HasPrefix(dest, "/usr/") || strings.HasPrefix(dest, "/opt/")
}
