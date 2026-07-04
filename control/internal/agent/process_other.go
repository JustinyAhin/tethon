//go:build !darwin && !linux

package agent

import (
	"os"
	"os/exec"
	"syscall"
)

func configureOwnedProcessGroup(_ *exec.Cmd) {}

func processGroupID(_ int) (int, error) {
	return 0, syscall.ENOSYS
}

func processOwnsGroup(_, _ int) bool {
	return false
}

func signalProcessGroup(_ int, _ os.Signal) error {
	return syscall.ENOSYS
}

func killProcessGroup(_ int) error {
	return syscall.ENOSYS
}
