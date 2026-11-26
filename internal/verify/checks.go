package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// RanByOrchestrator is the value of CheckResult.RanBy for checks run here.
const RanByOrchestrator = "orchestrator"

// DefaultCheckDeadline is the deadline of one check when none is given.
const DefaultCheckDeadline = 10 * time.Minute

// maxCheckOutput is how much output of one check is kept.
const maxCheckOutput = 64 * 1024

// CheckOptions tune RunChecks.
type CheckOptions struct {
	// Deadline is the time limit of each check. Zero means
	// DefaultCheckDeadline.
	Deadline time.Duration
	// Environ is the environment to scrub, in NAME=value form. Nil means the
	// environment of this process. Tests pass their own.
	Environ []string
}

// CheckOutcome is what one check did. Result goes into the evidence bundle.
// The bundle has no field for the other values yet.
type CheckOutcome struct {
	Result api.CheckResult
	// TimedOut is true when the deadline stopped the check. The exit code is
	// then -1.
	TimedOut bool
	// Output is the start of the combined output, kept for the engineering
	// view. It is untrusted text.
	Output string
	// Err is set when the check could not be started.
	Err string
}

// Passed reports whether the check ran to the end and exited with 0.
func (o CheckOutcome) Passed() bool {
	return o.Err == "" && !o.TimedOut && o.Result.ExitCode == 0
}

// AllPassed reports whether every outcome passed. No outcomes is a pass.
func AllPassed(outcomes []CheckOutcome) bool {
	for _, o := range outcomes {
		if !o.Passed() {
			return false
		}
	}
	return true
}

// RunChecks runs each check on a clean checkout of commit.
//
// The checkout is a detached git worktree in a new temporary directory. It
// is removed before RunChecks returns, also when a check fails, times out or
// the context is cancelled. Each check runs with its own deadline, with the
// scrubbed environment (see ScrubEnv) and in its own process group, and the
// whole group is killed at the deadline. Checks run one after the other, and
// a failing check does not stop the next one.
//
// The command line is run with "sh -c", because it is the repository's own
// configured command. That is the only place in this package that uses a
// shell. If ctx ends, RunChecks returns the outcomes so far and the error.
func RunChecks(ctx context.Context, repoDir, commit string, checks []api.Check, opts CheckOptions) ([]CheckOutcome, error) {
	sha, err := resolveCommit(ctx, repoDir, commit)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "oxf-verify-")
	if err != nil {
		return nil, fmt.Errorf("make a temporary directory: %w", err)
	}
	checkout := filepath.Join(tmp, "checkout")
	defer removeWorktree(repoDir, checkout, tmp)
	if _, err := runGit(ctx, repoDir, "-c", "core.hooksPath=/dev/null", "worktree", "add", "--detach", "--force", checkout, sha); err != nil {
		return nil, err
	}

	deadline := opts.Deadline
	if deadline <= 0 {
		deadline = DefaultCheckDeadline
	}
	environ := opts.Environ
	if environ == nil {
		environ = os.Environ()
	}
	env := ScrubEnv(environ)

	var outcomes []CheckOutcome
	for _, c := range checks {
		if err := ctx.Err(); err != nil {
			return outcomes, err
		}
		o := runOne(ctx, checkout, c, deadline, env)
		if err := ctx.Err(); err != nil {
			return append(outcomes, o), err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

func runOne(parent context.Context, dir string, c api.Check, deadline time.Duration, env []string) CheckOutcome {
	ctx, cancel := context.WithTimeout(parent, deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Run)
	cmd.Dir = dir
	cmd.Env = env
	ownProcessGroup(cmd)
	cmd.Cancel = func() error { killGroup(cmd); return nil }
	// A child that keeps the output pipe open must not hold us for ever.
	cmd.WaitDelay = 2 * time.Second
	out := &cappedBuffer{max: maxCheckOutput}
	cmd.Stdout, cmd.Stderr = out, out

	start := time.Now()
	runErr := cmd.Run()
	killGroup(cmd) // anything the check left running
	elapsed := time.Since(start)

	o := CheckOutcome{
		Result: api.CheckResult{
			Name:            c.Name,
			DurationSeconds: int(math.Round(elapsed.Seconds())),
			RanBy:           RanByOrchestrator,
		},
		Output:   out.String(),
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil,
	}
	if cmd.ProcessState != nil {
		o.Result.ExitCode = cmd.ProcessState.ExitCode()
	} else {
		o.Result.ExitCode = -1
		if runErr != nil {
			o.Err = runErr.Error()
		}
	}
	if o.TimedOut {
		o.Result.ExitCode = -1
	}
	return o
}

// removeWorktree removes the checkout and its git bookkeeping. It uses its
// own context, because the caller's may be cancelled.
func removeWorktree(repoDir, checkout, tmp string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = runGit(ctx, repoDir, "worktree", "remove", "--force", checkout)
	_ = os.RemoveAll(tmp)
	_, _ = runGit(ctx, repoDir, "worktree", "prune")
}

// cappedBuffer keeps the first max bytes written and drops the rest.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string { return b.buf.String() }
