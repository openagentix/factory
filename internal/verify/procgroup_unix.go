//go:build unix

package verify

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts the command in a new process group, so that the
// whole tree it starts can be killed together.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup kills every process in the command's process group.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
