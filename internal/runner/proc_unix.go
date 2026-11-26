//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// setGroup puts the process in a new process group of its own.
func setGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// signalProcess sends the terminating signal, or the kill signal.
func signalProcess(cmd *exec.Cmd, kill bool) {
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	cmd.Process.Signal(sig)
}
