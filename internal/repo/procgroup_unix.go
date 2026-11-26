//go:build unix

package repo

import (
	"os/exec"
	"syscall"
	"time"
)

// ownProcessGroup puts the command in its own process group and kills the
// whole group when the context ends, so a child of git cannot outlive it.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
