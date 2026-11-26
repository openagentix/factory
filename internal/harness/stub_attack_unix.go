//go:build unix

package harness

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

func makeFifo(p string) error { return syscall.Mkfifo(p, 0o644) }

// ignoreSignals makes the stub ignore the terminating signals. The returned
// function restores them.
func ignoreSignals() func() {
	sigs := []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}
	signal.Ignore(sigs...)
	return func() { signal.Reset(sigs...) }
}

// startChild starts a background sleep that outlives the stub. It keeps the
// stub's standard output open when that is a file, as a real child of an
// agent would, so a reader waiting for the end of the stream is tested.
func startChild(c StubChild, stdout io.Writer) (int, error) {
	cmd := exec.Command("sleep", strconv.Itoa(c.SleepSeconds))
	if f, ok := stdout.(*os.File); ok {
		cmd.Stdout = f
	}
	if c.NewSession {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// Release the process so it is not ours to wait for.
	return pid, cmd.Process.Release()
}
