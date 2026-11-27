//go:build !linux

package runner

// strayProcesses cannot look cheaply on this system. Finding the processes
// that use a directory would need a program such as lsof, and the runner
// starts only the harness.
func strayProcesses(string) ([]int, bool) { return nil, false }
