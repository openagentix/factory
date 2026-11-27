//go:build linux

package runner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// strayProcesses looks in /proc for processes whose working directory or
// open files are under the run directory. It runs no program.
func strayProcesses(dir string) ([]int, bool) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	under := func(p string) bool { return p == real || strings.HasPrefix(p, real+"/") }
	var pids []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		base := filepath.Join("/proc", e.Name())
		hit := false
		if p, err := os.Readlink(filepath.Join(base, "cwd")); err == nil && under(p) {
			hit = true
		}
		if fds, err := os.ReadDir(filepath.Join(base, "fd")); err == nil && !hit {
			for _, fd := range fds {
				if p, err := os.Readlink(filepath.Join(base, "fd", fd.Name())); err == nil && under(p) {
					hit = true
					break
				}
			}
		}
		if hit {
			pids = append(pids, pid)
		}
	}
	return pids, true
}
