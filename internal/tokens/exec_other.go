//go:build !(darwin || linux)

package tokens

import "os/exec"

// Other systems are refused before a tool is run, see systemTool.
func prepare(cmd *exec.Cmd) {}
