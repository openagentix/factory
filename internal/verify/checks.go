package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// MaxTotalBytes, MaxFileBytes and MaxEntries bound the checkout. Zero
	// means the defaults: DefaultMaxTotalBytes, DefaultMaxFileBytes and
	// DefaultMaxEntries.
	MaxTotalBytes int64
	MaxFileBytes  int64
	MaxEntries    int
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
	// Notes say what was left out of the checkout and what the check is not
	// protected from. They are the same for every check of one run.
	Notes []string
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

func (o CheckOptions) limits() limits {
	l := limits{o.MaxTotalBytes, o.MaxFileBytes, o.MaxEntries}
	if l.maxTotal <= 0 {
		l.maxTotal = DefaultMaxTotalBytes
	}
	if l.maxFile <= 0 {
		l.maxFile = DefaultMaxFileBytes
	}
	if l.maxEntries <= 0 {
		l.maxEntries = DefaultMaxEntries
	}
	return l
}

// containmentNotes are put on every outcome. They say what the checks are
// not protected from. This package does not provide an operating system
// sandbox.
var containmentNotes = []string{
	"HOME and TMPDIR are a fresh empty directory for each check, so tool caches are cold",
	"writes outside the checkout are not prevented: there is no operating system sandbox here",
	"a check that leaves its process group, for example with setsid, is not contained or killed",
}

// RunChecks runs each check on a clean checkout of commit.
//
// The repository must pass the guard (see checkTrusted) before anything else
// happens. The checkout is built by reading git objects only (see materialize) into a
// new temporary directory outside the repository. It has no .git, so a check
// has no path back to the repository, and no git filter or hook runs. The
// directory is removed before RunChecks returns, also when a check fails,
// times out or the context is cancelled. Each check runs with its own
// deadline, with the scrubbed environment (see ScrubEnv), with a fresh empty
// directory as HOME and TMPDIR that is removed after the check, and in its own
// process group, and the whole group is killed at the deadline. Checks run one
// after the other, and a failing check does not stop the next one.
//
// Limits, also listed in the Notes of each outcome: tool caches are cold
// because HOME is empty; writes outside the checkout are not prevented, since
// that needs an operating system sandbox; and a check that leaves its process
// group with setsid is not contained.
//
// The command line is run with "sh -c", because it is the repository's own
// configured command. That is the only place in this package that uses a
// shell. If ctx ends, RunChecks returns the outcomes so far and the error.
func RunChecks(ctx context.Context, repoDir, commit string, checks []api.Check, opts CheckOptions) ([]CheckOutcome, error) {
	if err := checkTrusted(ctx, repoDir); err != nil {
		return nil, err
	}
	sha, err := resolveCommit(ctx, repoDir, commit)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "oxf-verify-")
	if err != nil {
		return nil, fmt.Errorf("make a temporary directory: %w", err)
	}
	defer removeAll(tmp)
	checkout := filepath.Join(tmp, "checkout")
	if err := os.Mkdir(checkout, 0o700); err != nil {
		return nil, err
	}
	notes, err := materialize(ctx, repoDir, sha, checkout, opts.limits())
	if err != nil {
		return nil, err
	}
	notes = append(notes, containmentNotes...)

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
		o := runOne(ctx, checkout, tmp, c, deadline, env)
		o.Notes = notes
		if err := ctx.Err(); err != nil {
			return append(outcomes, o), err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

func runOne(parent context.Context, dir, tmp string, c api.Check, deadline time.Duration, base []string) CheckOutcome {
	home, herr := os.MkdirTemp(tmp, "home-")
	if herr != nil {
		return CheckOutcome{Result: api.CheckResult{Name: c.Name, ExitCode: -1, RanBy: RanByOrchestrator}, Err: herr.Error()}
	}
	defer removeAll(home)
	// The scrubbed environment, with HOME and TMPDIR replaced. No real home
	// path is passed on.
	var env []string
	for _, kv := range base {
		if !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "TMPDIR=") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "TMPDIR="+home)

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
		o.Result.TimedOut = true
	}
	return o
}

// removeAll removes a directory tree, also when a check made parts of it
// read-only.
func removeAll(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
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
