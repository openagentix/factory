package tokens

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

type result struct {
	stdout      []byte
	stderrBytes int
	code        int
}

// capWriter keeps at most max bytes. At the first byte over the limit it
// stops the tool.
type capWriter struct {
	mu     sync.Mutex
	buf    []byte
	max    int
	over   bool
	cancel context.CancelFunc
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.over || len(w.buf)+len(p) > w.max {
		if !w.over {
			w.over = true
			w.cancel()
		}
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// countWriter counts bytes and keeps none: standard error is never stored, so
// it can never be copied into an error.
type countWriter struct {
	mu sync.Mutex
	n  int
}

func (w *countWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.n += len(p)
	w.mu.Unlock()
	return len(p), nil
}

// run starts the tool with a deadline, a scrubbed environment and no
// terminal, and waits for it. The error never holds the output of the tool.
func (s *system) run(ctx context.Context, args []string) (result, error) {
	return execTool(ctx, s.tool, s.env, s.timeout, s.waitDelay, args)
}

// execTool is the one place that starts a program (see guard_test.go). It
// checks the path first.
func execTool(ctx context.Context, path string, env []string, timeout, waitDelay time.Duration, args []string) (result, error) {
	tool, err := checkTool(path)
	if err != nil {
		return result{}, err
	}
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx2, tool, args...)
	cmd.Env = append([]string{}, env...)
	cmd.Dir = "/"
	cmd.WaitDelay = waitDelay
	out := &capWriter{max: maxOutput, cancel: cancel}
	errc := &countWriter{}
	cmd.Stdout = out
	cmd.Stderr = errc
	prepare(cmd) // new session: no controlling terminal; the whole group is stopped at the deadline
	runErr := cmd.Run()

	out.mu.Lock()
	over, buf := out.over, out.buf
	out.mu.Unlock()
	errc.mu.Lock()
	n := errc.n
	errc.mu.Unlock()

	switch {
	case over:
		return result{}, errors.New("the tool printed too much")
	case ctx.Err() != nil:
		return result{}, errors.New("cancelled")
	case ctx2.Err() != nil:
		return result{}, errors.New("the tool did not finish in time")
	}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
		return result{stdout: buf, stderrBytes: n}, nil
	case errors.As(runErr, &ee):
		if ee.ExitCode() < 0 {
			return result{}, errors.New("the tool was stopped by a signal")
		}
		return result{stdout: buf, stderrBytes: n, code: ee.ExitCode()}, nil
	case errors.Is(runErr, exec.ErrWaitDelay):
		return result{}, errors.New("the tool left a process running")
	}
	return result{}, errors.New("the tool could not be started")
}
