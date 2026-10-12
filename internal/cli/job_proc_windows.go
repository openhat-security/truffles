//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

func setJobSysProcAttr(cmd *exec.Cmd) {
	// Windows has no setsid; detach via CREATE_NEW_PROCESS_GROUP.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func terminateJobProcessGroup(pgid int) error {
	p, err := os.FindProcess(pgid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func killJobProcessGroup(pgid int) error {
	return terminateJobProcessGroup(pgid)
}
