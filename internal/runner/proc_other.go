//go:build !unix

package runner

import "os/exec"

func setGroup(*exec.Cmd) {}

// signalGroup reaches only the process itself here. This system has no
// process groups in the runner.
func signalGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}
