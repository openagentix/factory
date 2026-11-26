//go:build !unix

package repo

import (
	"os/exec"
	"time"
)

func ownProcessGroup(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
