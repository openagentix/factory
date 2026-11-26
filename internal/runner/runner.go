package runner

import (
	"context"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/harness"
	"github.com/openagentix/factory/internal/repo"
)

// Runner starts the work stage of one run.
type Runner interface {
	// Start prepares a run and starts the agent. It returns when the agent
	// is running. An error means nothing was started and nothing is left on
	// disk. Ending ctx cancels the run, as Handle.Cancel does.
	Start(ctx context.Context, spec Spec) (Handle, error)
}

// Handle follows one run.
type Handle interface {
	// Events carries the events of the run as they arrive. It is closed
	// when the output of the agent ends. The channel has a buffer, and an
	// event that does not fit is dropped from the channel, not from the
	// recorded events file. Spec.OnEvent never drops.
	Events() <-chan api.Event
	// Wait blocks until the run is over and returns its outcome. When ctx
	// ends first it returns ctx.Err() and the run goes on.
	Wait(ctx context.Context) (Outcome, error)
	// Cancel stops the run as a deadline does, and waits until it is over
	// or ctx ends.
	Cancel(ctx context.Context) error
}

// Limits bound what the runner reads and keeps. A zero field means the
// default.
type Limits struct {
	// MaxResultBytes is the largest result.json the runner reads.
	MaxResultBytes int64
	// MaxStdoutBytes is how much standard output is parsed. The rest is
	// read and thrown away, so the agent never blocks on a full pipe.
	MaxStdoutBytes int64
	// MaxEventsBytes is the size of the runner's events file.
	MaxEventsBytes int64
	// MaxStderrBytes is the size of the standard error log.
	MaxStderrBytes int64
	// PipeWait is how long the runner waits for the output pipes to end
	// after the process group is gone.
	PipeWait time.Duration
}

// Defaults of Limits and of the time settings.
const (
	DefaultMaxResultBytes = 1 << 20
	DefaultMaxStdoutBytes = 256 << 20
	DefaultMaxEventsBytes = 16 << 20
	DefaultMaxStderrBytes = 4 << 20
	DefaultPipeWait       = 2 * time.Second
	DefaultGrace          = 5 * time.Second
)

func (l Limits) withDefaults() Limits {
	if l.MaxResultBytes <= 0 {
		l.MaxResultBytes = DefaultMaxResultBytes
	}
	if l.MaxStdoutBytes <= 0 {
		l.MaxStdoutBytes = DefaultMaxStdoutBytes
	}
	if l.MaxEventsBytes <= 0 {
		l.MaxEventsBytes = DefaultMaxEventsBytes
	}
	if l.MaxStderrBytes <= 0 {
		l.MaxStderrBytes = DefaultMaxStderrBytes
	}
	if l.PipeWait <= 0 {
		l.PipeWait = DefaultPipeWait
	}
	return l
}

// Spec says what one run is.
type Spec struct {
	// Repo is the factory's own repository. Commit is the revision to work
	// on. The runner resolves it once and uses that commit for the export
	// and as the parent of the new commit.
	Repo   *repo.Repo
	Commit string
	// DataDir is where run directories are made. It is made when absent.
	DataDir string

	// Task is the resolved task, written to in/task.json. TriggerJSON is
	// written to in/trigger.json and must be valid JSON. Empty means {}.
	Task        api.Task
	TriggerJSON []byte
	// Prompt is rendered to in/prompt.md. The runner fills in OutDir and
	// Commands (from Harness.Commands).
	Prompt harness.PromptParts

	// Adapter builds the command and reads the stream. Harness holds the
	// run settings that are not paths (commands, method, plugins, model,
	// billing, cost limit, stub script). The runner sets Workspace, InDir,
	// OutDir, PromptFile and Deadline.
	Adapter harness.Adapter
	Harness harness.Spec

	// Metered says that the run is paid by an API key. Only then are the
	// variables named in KeyEnv passed, with the values they have in the
	// factory's environment.
	Metered bool
	KeyEnv  []string
	// Environ is the factory's environment. Nil means os.Environ().
	Environ []string

	// Author is the factory's author name for the new commit. Message is
	// the commit message; empty means a short default.
	Author  repo.Author
	Message string
	// RepoLimits bound the export and the import. ImportExclude lists path
	// patterns the import leaves out.
	RepoLimits    repo.Limits
	ImportExclude []string

	// Deadline is how long the agent may run. It is required. Grace is how
	// long after the terminating signal before the group is killed.
	Deadline time.Duration
	Grace    time.Duration
	Limits   Limits

	// KeepFailed keeps the run directory of a failed run for inspection.
	KeepFailed bool
	// OnEvent gets every event, in order, from one goroutine. It must not
	// block for long.
	OnEvent func(api.Event)
}

// Outcome is everything the runner learned about one run.
type Outcome struct {
	// Failed is true when the run produced no usable result or no commit.
	// Reason says why, in plain words.
	Failed bool
	Reason string
	// Result is the parsed result.json. Nil when there was none.
	Result *api.Result
	// Summary is what the harness stream said at the end.
	Summary harness.Summary
	// Commit is the new commit, or the parent when nothing changed. Empty
	// when the run failed before the import. Changed says whether the
	// files differ from the parent.
	Commit  string
	Changed bool
	// ExitCode is the exit code of the agent, or -1 when it was killed by a
	// signal.
	ExitCode  int
	TimedOut  bool
	Cancelled bool
	// Notes are plain statements about what happened and what the runner
	// could not do.
	Notes []string
	// EventCount counts the events handed out. EventsDropped counts those
	// that did not fit in the channel or the events file.
	EventCount    int
	EventsDropped int
	// StderrHead is the start of standard error, made printable.
	StderrHead string
	// RunDir, EventsPath and StderrPath are set only when the run
	// directory was kept.
	RunDir     string
	EventsPath string
	StderrPath string
}
