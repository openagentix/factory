//go:build darwin || linux

package tokens

import (
	"os/exec"
	"syscall"
)

func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// The tool leads its own session and group, so a negative id reaches
		// every process it started.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
