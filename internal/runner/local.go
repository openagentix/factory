package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/harness"
	"github.com/openagentix/factory/internal/repo"
)

// Local runs the agent as a process on this machine.
type Local struct{}

// NewLocal returns the local runner.
func NewLocal() *Local { return &Local{} }

var _ Runner = (*Local)(nil)

type run struct {
	spec Spec
	lim  Limits
	dir  string
	ws   string
	in   string
	out  string
	sha  string

	cmd        *exec.Cmd
	evFile     *os.File
	errFile    *os.File
	evBytes    int64
	forced     atomic.Bool
	stderrHead []byte
	stdout     *os.File
	stderr     *os.File
	events     chan api.Event

	cancelOnce sync.Once
	cancelCh   chan struct{}
	done       chan struct{}
	outcome    Outcome

	notes []string
}

// Start implements Runner.
func (*Local) Start(ctx context.Context, spec Spec) (Handle, error) {
	if err := checkSpec(spec); err != nil {
		return nil, err
	}
	r := &run{spec: spec, lim: spec.Limits.withDefaults(), events: make(chan api.Event, 1024),
		cancelCh: make(chan struct{}), done: make(chan struct{})}
	if err := r.prepare(ctx); err != nil {
		r.removeDir()
		return nil, err
	}
	if err := r.launch(); err != nil {
		r.removeDir()
		return nil, err
	}
	go r.supervise(ctx)
	return r, nil
}

func checkSpec(s Spec) error {
	switch {
	case s.Repo == nil:
		return errors.New("runner: Repo is required")
	case s.Adapter == nil:
		return errors.New("runner: Adapter is required")
	case s.DataDir == "":
		return errors.New("runner: DataDir is required")
	case s.Deadline <= 0:
		return errors.New("runner: Deadline is required")
	case s.Commit == "":
		return errors.New("runner: Commit is required")
	case strings.TrimSpace(s.Author.Name) == "" || strings.TrimSpace(s.Author.Email) == "":
		return errors.New("runner: Author needs a name and an address")
	}
	if len(s.TriggerJSON) > 0 && !json.Valid(s.TriggerJSON) {
		return errors.New("runner: TriggerJSON is not valid JSON")
	}
	return nil
}

func (r *run) note(format string, a ...any) { r.notes = append(r.notes, fmt.Sprintf(format, a...)) }

// prepare makes the directories and writes the workspace and the inputs.
func (r *run) prepare(ctx context.Context) error {
	data, err := filepath.Abs(r.spec.DataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(data, 0o700); err != nil {
		return err
	}
	if r.dir, err = os.MkdirTemp(data, "run-"); err != nil {
		return err
	}
	r.ws, r.in, r.out = filepath.Join(r.dir, "workspace"), filepath.Join(r.dir, "in"), filepath.Join(r.dir, "out")
	for _, d := range []string{r.in, r.out, filepath.Join(r.dir, "tmp")} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return err
		}
	}
	if r.sha, err = r.spec.Repo.Resolve(ctx, r.spec.Commit); err != nil {
		return fmt.Errorf("runner: resolve commit: %w", err)
	}
	notes, err := r.spec.Repo.Export(ctx, r.sha, r.ws, r.spec.RepoLimits)
	if err != nil {
		return fmt.Errorf("runner: export: %w", err)
	}
	for _, n := range notes {
		r.note("export: %s", n)
	}
	return r.writeInputs()
}

func (r *run) writeInputs() error {
	task, err := json.MarshalIndent(r.spec.Task, "", "  ")
	if err != nil {
		return err
	}
	trigger := r.spec.TriggerJSON
	if len(trigger) == 0 {
		trigger = []byte("{}")
	}
	parts := r.spec.Prompt
	parts.OutDir = r.out
	parts.Commands = r.spec.Harness.Commands
	files := map[string][]byte{
		"task.json":    append(task, '\n'),
		"prompt.md":    []byte(harness.RenderPrompt(parts)),
		"trigger.json": trigger,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(r.in, name), data, 0o400); err != nil {
			return err
		}
	}
	return os.Chmod(r.in, 0o500)
}

func (r *run) launch() error {
	hs := r.spec.Harness
	hs.Workspace, hs.InDir, hs.OutDir = r.ws, r.in, r.out
	hs.PromptFile = filepath.Join(r.in, "prompt.md")
	hs.Deadline = r.spec.Deadline
	if r.spec.Metered {
		hs.Billing = api.BillingMetered
	}
	c, err := r.spec.Adapter.Command(hs)
	if err != nil {
		return fmt.Errorf("runner: adapter: %w", err)
	}
	env, err := buildEnv(r.spec, filepath.Join(r.dir, "tmp"), c.Env)
	if err != nil {
		return err
	}
	cmd := exec.Command(c.Program, c.Args...)
	cmd.Dir, cmd.Env = r.ws, env
	setGroup(cmd)
	if c.StdinFile != "" {
		f, err := os.Open(c.StdinFile)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd.Stdin = f
	}
	// The runner's own events file and error log live in the run directory,
	// not in out: the agent controls everything under out. Each is made new,
	// so a link in its place is never followed.
	ef, err := os.OpenFile(r.eventsPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	r.evFile = ef
	sf, err := os.OpenFile(r.stderrPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		ef.Close()
		return err
	}
	r.errFile = sf
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	er, ew, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stderr = ew
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		er.Close()
		ew.Close()
		return fmt.Errorf("runner: start: %w", err)
	}
	pw.Close()
	ew.Close()
	r.cmd, r.stdout, r.stderr = cmd, pr, er
	return nil
}

// Events implements Handle.
func (r *run) Events() <-chan api.Event { return r.events }

// Wait implements Handle.
func (r *run) Wait(ctx context.Context) (Outcome, error) {
	select {
	case <-r.done:
		return r.outcome, nil
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
}

// Cancel implements Handle.
func (r *run) Cancel(ctx context.Context) error {
	r.cancelOnce.Do(func() { close(r.cancelCh) })
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *run) eventsPath() string { return filepath.Join(r.dir, "events.jsonl") }
func (r *run) stderrPath() string { return filepath.Join(r.dir, "stderr.log") }

func (r *run) supervise(ctx context.Context) {
	defer close(r.done)
	o := &r.outcome
	waited := make(chan error, 1)
	go func() { waited <- r.cmd.Wait() }()
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); r.readStdout(o) }()
	go func() { defer readers.Done(); r.readStderr() }()
	readersDone := make(chan struct{})
	go func() { readers.Wait(); close(readersDone) }()

	timer := time.NewTimer(r.spec.Deadline)
	defer timer.Stop()
	var werr error
	select {
	case werr = <-waited:
	case <-timer.C:
		o.TimedOut = true
		werr = r.stop(waited)
	case <-r.cancelCh:
		o.Cancelled = true
		werr = r.stop(waited)
	case <-ctx.Done():
		o.Cancelled = true
		werr = r.stop(waited)
	}
	o.ExitCode = exitCode(werr)
	// Whatever the agent started in its group must not outlive the run.
	signalGroup(r.cmd, true)
	select {
	case <-readersDone:
	case <-time.After(r.lim.PipeWait):
		// Something outside the group still holds the output pipes open.
		r.forced.Store(true)
		r.stdout.Close()
		r.stderr.Close()
		<-readersDone
		r.note("a process outside the process group still held the output pipes after the run; it may still be running. The runner cannot reach it without an operating system sandbox")
	}
	r.evFile.Close()
	r.errFile.Close()
	r.finish(ctx, o)
}

// stop ends the process group: the terminating signal, then, after the
// grace time, the kill.
func (r *run) stop(waited chan error) error {
	signalGroup(r.cmd, false)
	grace := r.spec.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	select {
	case err := <-waited:
		return err
	case <-time.After(grace):
	}
	signalGroup(r.cmd, true)
	return <-waited
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// readStdout hands the stream to the adapter, bounded. What is past the
// limit is read and thrown away, so the agent never blocks on a full pipe.
func (r *run) readStdout(o *Outcome) {
	defer close(r.events)
	lr := &io.LimitedReader{R: r.stdout, N: r.lim.MaxStdoutBytes}
	sum, err := r.spec.Adapter.Read(lr, func(e api.Event) { r.emit(o, e) })
	if err != nil && !r.forced.Load() {
		r.note("reading standard output stopped: %v", err)
	}
	o.Summary = sum
	if lr.N <= 0 {
		n, _ := io.Copy(io.Discard, r.stdout)
		r.note("standard output went over %d bytes. The rest (%d bytes or more) was thrown away", r.lim.MaxStdoutBytes, n)
	}
}

func (r *run) emit(o *Outcome, e api.Event) {
	o.EventCount++
	b, err := json.Marshal(e)
	if err == nil {
		b = append(b, '\n')
		if r.evBytes+int64(len(b)) <= r.lim.MaxEventsBytes {
			if _, err := r.evFile.Write(b); err == nil {
				r.evBytes += int64(len(b))
			}
		} else {
			o.EventsDropped++
		}
	}
	if r.spec.OnEvent != nil {
		r.spec.OnEvent(e)
	}
	select {
	case r.events <- e:
	default:
	}
}

const stderrHeadBytes = 4 << 10

// readStderr keeps standard error in the log file up to its size limit,
// keeps the first bytes in memory, and throws the rest away.
func (r *run) readStderr() {
	buf := make([]byte, 32<<10)
	var written int64
	for {
		n, err := r.stderr.Read(buf)
		if n > 0 {
			if len(r.stderrHead) < stderrHeadBytes {
				r.stderrHead = append(r.stderrHead, buf[:min(n, stderrHeadBytes-len(r.stderrHead))]...)
			}
			if room := r.lim.MaxStderrBytes - written; room > 0 {
				w, _ := r.errFile.Write(buf[:min(int64(n), room)])
				written += int64(w)
			}
		}
		if err != nil {
			return
		}
	}
}

func (r *run) finish(ctx context.Context, o *Outcome) {
	o.StderrHead = repo.Visible(string(r.stderrHead), 2000)
	defer func() {
		o.Notes = append(o.Notes, r.notes...)
		if o.Failed && r.spec.KeepFailed {
			o.RunDir, o.EventsPath, o.StderrPath = r.dir, r.eventsPath(), r.stderrPath()
		} else {
			r.removeDir()
		}
	}()
	fail := func(format string, a ...any) { o.Failed, o.Reason = true, fmt.Sprintf(format, a...) }
	switch {
	case o.TimedOut:
		fail("the deadline of %v passed", r.spec.Deadline)
		return
	case o.Cancelled:
		fail("the run was cancelled")
		return
	case o.ExitCode != 0:
		fail("the agent exited with code %d", o.ExitCode)
		return
	}
	data, err := readResult(r.dir, r.lim.MaxResultBytes)
	if err != nil {
		fail("no usable result.json: %v", err)
		return
	}
	res, err := api.ParseResult(data)
	if err != nil {
		fail("result.json is not valid: %v", err)
		return
	}
	msg := r.spec.Message
	if msg == "" {
		msg = "factory: work stage of a run"
	}
	commit, changed, err := r.spec.Repo.Import(ctx, r.ws, r.sha, msg, r.spec.Author,
		repo.ImportOptions{Limits: r.spec.RepoLimits, Exclude: r.spec.ImportExclude})
	if err != nil {
		fail("import of the workspace refused: %v", err)
		return
	}
	o.Result, o.Commit, o.Changed = res, commit, changed
}

// removeDir removes the run directory. The agent may have made directories
// it cannot enter, and the in directory is read-only, so modes are opened
// first. A link is removed, never followed.
func (r *run) removeDir() {
	if r.dir == "" {
		return
	}
	filepath.WalkDir(r.dir, func(p string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() {
			os.Chmod(p, 0o700)
		}
		return nil
	})
	if err := os.RemoveAll(r.dir); err != nil {
		r.outcome.Notes = append(r.outcome.Notes, fmt.Sprintf("the run directory could not be removed: %v", err))
	}
}
