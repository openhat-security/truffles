//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

func setJobSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func terminateJobProcessGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGTERM)
}

func killJobProcessGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
