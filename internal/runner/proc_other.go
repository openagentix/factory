//go:build !unix

package runner

import "os/exec"

func setGroup(*exec.Cmd) {}

func signalProcess(cmd *exec.Cmd, kill bool) { cmd.Process.Kill() }
