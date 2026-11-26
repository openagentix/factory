//go:build !unix

package verify

import "os/exec"

func ownProcessGroup(cmd *exec.Cmd) {}

// killGroup kills only the process itself where process groups are not
// available. The checks of the first release run on Unix systems.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
