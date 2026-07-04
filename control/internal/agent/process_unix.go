//go:build darwin || linux

package agent

import (
	"os"
	"os/exec"
	"syscall"
)

func configureOwnedProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func processGroupID(pid int) (int, error) {
	return syscall.Getpgid(pid)
}

func processOwnsGroup(pid, pgid int) bool {
	return pid > 0 && pid == pgid
}

func signalProcessGroup(pgid int, signal os.Signal) error {
	sysSignal, ok := signal.(syscall.Signal)
	if !ok {
		return syscall.EINVAL
	}
	return syscall.Kill(-pgid, sysSignal)
}

func killProcessGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
