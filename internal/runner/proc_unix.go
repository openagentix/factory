//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// setGroup puts the process in a new process group of its own.
func setGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// signalGroup sends the terminating signal, or the kill signal, to every
// process in the group of the command. The group id is the process id of
// the leader, because the leader made the group.
func signalGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	syscall.Kill(-cmd.Process.Pid, sig)
}
