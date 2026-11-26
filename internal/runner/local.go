package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// beforeImport is called just before the import. Tests use it. It is nil in
// production.
var beforeImport func(*run)

// named is a directory and what it was when the run began.
type named struct {
	path string
	info fs.FileInfo
}

type run struct {
	spec Spec
	lim  Limits

	// The run directory is held by handle, not by name. The agent runs as the
	// same user and can rename its run directory and put a link in its place,
	// so everything the runner reads, writes and removes goes through root.
	// dir is the name, used for the paths the agent and the repository see.
	dir      string
	root     *os.Root
	rootInfo fs.FileInfo
	runsRoot *os.Root
	parents  []named // the runs folder and the data directory
	ws       string
	in       string
	out      string
	sha      string

	cmd        *exec.Cmd
	evFile     *os.File
	errFile    *os.File
	evBytes    int64
	forced     atomic.Bool
	stderrHead []byte
	stdout     *os.File
	stderr     *os.File
	events     chan api.Event

	cb        chan api.Event
	cbDone    chan struct{}
	cbPanic   atomic.Value // string
	cancelOne sync.Once
	cancelCh  chan struct{}
	done      chan struct{}
	outcome   Outcome

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
		r.cleanup()
		return nil, err
	}
	if err := r.launch(); err != nil {
		r.cleanup()
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
	return checkKeyNames(s)
}

func (r *run) note(format string, a ...any) { r.notes = append(r.notes, fmt.Sprintf(format, a...)) }

// makeRunDir makes DataDir/runs/<random name> and opens it as a handle. The
// name is long and random, so nothing else can guess it. The runs folder is
// mode 0700. The same user can still list it: this keeps other users out,
// and does not stop the agent.
func (r *run) makeRunDir() error {
	data, err := filepath.Abs(r.spec.DataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(data, 0o700); err != nil {
		return err
	}
	runs := filepath.Join(data, "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(runs, 0o700); err != nil {
		return err
	}
	dataInfo, err := os.Stat(data)
	if err != nil {
		return err
	}
	runsInfo, err := os.Lstat(runs)
	if err != nil {
		return err
	}
	if !runsInfo.IsDir() || runsInfo.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("runner: %s is not a plain directory", runs)
	}
	r.parents = []named{{runs, runsInfo}, {data, dataInfo}}
	if r.runsRoot, err = os.OpenRoot(runs); err != nil {
		return err
	}
	for i := 0; i < 10; i++ {
		var b [24]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		name := hex.EncodeToString(b[:])
		if err := r.runsRoot.Mkdir(name, 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return err
		}
		r.dir = filepath.Join(runs, name)
		if r.root, err = r.runsRoot.OpenRoot(name); err != nil {
			return err
		}
		r.rootInfo, err = r.root.Stat(".")
		return err
	}
	return errors.New("runner: could not make a run directory")
}

// sameDir checks that the run directory's name still names the directory
// that was made, and that its parents are unchanged.
//
// This narrows the window and does not close it. repo.Export and repo.Import
// take a path, not an opened directory, so a swap between this check and the
// moment the repository package opens the path is still possible for the
// same user. What internal/repo should offer to close it: an Import (and
// Export) that takes an opened directory (an *os.Root or a file descriptor)
// and reads only through it.
func (r *run) sameDir() error {
	li, err := os.Lstat(r.dir)
	if err != nil || !li.IsDir() || li.Mode()&fs.ModeSymlink != 0 || !os.SameFile(li, r.rootInfo) {
		return errors.New("the run directory was moved or replaced")
	}
	for _, p := range r.parents {
		var fi fs.FileInfo
		if p.path == r.parents[0].path {
			fi, err = os.Lstat(p.path)
			if err == nil && (!fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0) {
				err = errors.New("not a plain directory")
			}
		} else {
			fi, err = os.Stat(p.path)
		}
		if err != nil || !os.SameFile(fi, p.info) {
			return fmt.Errorf("a parent of the run directory changed: %s", filepath.Base(p.path))
		}
	}
	return nil
}

// prepare makes the directories and writes the workspace and the inputs.
func (r *run) prepare(ctx context.Context) error {
	if err := r.makeRunDir(); err != nil {
		return err
	}
	r.ws, r.in, r.out = filepath.Join(r.dir, "workspace"), filepath.Join(r.dir, "in"), filepath.Join(r.dir, "out")
	for _, d := range []string{"in", "out", "tmp"} {
		if err := r.root.Mkdir(d, 0o700); err != nil {
			return err
		}
	}
	var err error
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
		f, err := r.root.OpenFile("in/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
		if err != nil {
			return err
		}
		_, werr := f.Write(data)
		if err := errors.Join(werr, f.Close()); err != nil {
			return err
		}
	}
	return r.root.Chmod("in", 0o500)
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
	// through the handle, so a link in its place is never followed.
	if r.evFile, err = r.root.OpenFile("events.jsonl", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err != nil {
		return err
	}
	if r.errFile, err = r.root.OpenFile("stderr.log", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err != nil {
		return err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	er, ew, err := os.Pipe()
	if err != nil {
		pr.Close()
		pw.Close()
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

func (r *run) eventsPath() string { return filepath.Join(r.dir, "events.jsonl") }
func (r *run) stderrPath() string { return filepath.Join(r.dir, "stderr.log") }

// Events implements Handle.
func (r *run) Events() <-chan api.Event { return r.events }

// Wait implements Handle.
func (r *run) Wait(ctx context.Context) (Outcome, error) {
	select {
	case <-r.done:
		return r.outcome, nil
	case <-ctx.Done():
		return Outcome{Status: StatusUnknown, Reason: "the run is still going"}, ctx.Err()
	}
}

func (r *run) requestCancel() { r.cancelOne.Do(func() { close(r.cancelCh) }) }

// Cancel implements Handle.
func (r *run) Cancel(ctx context.Context) error {
	r.requestCancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *run) cancelled(ctx context.Context) bool {
	select {
	case <-r.cancelCh:
		return true
	default:
	}
	return ctx.Err() != nil
}

func (r *run) supervise(ctx context.Context) {
	defer close(r.done)
	o := &r.outcome
	if r.spec.OnEvent != nil {
		r.cb = make(chan api.Event, callbackBuffer)
		r.cbDone = make(chan struct{})
		go r.deliver()
	}
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
		o.ExitCode = exitCode(werr)
	case <-timer.C:
		o.TimedOut = true
		werr = r.stop(waited)
		o.ExitCode = -1
	case <-r.cancelCh:
		o.Cancelled = true
		werr = r.stop(waited)
		o.ExitCode = -1
	case <-ctx.Done():
		o.Cancelled = true
		werr = r.stop(waited)
		o.ExitCode = -1
	}
	_ = werr
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
	r.finishCallback(o)
	r.lookForStrays(o)
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
	if b, err := json.Marshal(e); err == nil {
		b = append(b, '\n')
		if r.evBytes+int64(len(b)) <= r.lim.MaxEventsBytes {
			if _, err := r.evFile.Write(b); err == nil {
				r.evBytes += int64(len(b))
			}
		} else {
			o.EventsDropped++
		}
	}
	if r.cb != nil {
		select {
		case r.cb <- e:
		default:
			o.CallbackDropped++
		}
	}
	select {
	case r.events <- e:
	default:
	}
}

// deliver gives the events to the caller's callback, away from the reader.
// A panic in the callback fails the run and ends the agent's group.
func (r *run) deliver() {
	defer close(r.cbDone)
	for e := range r.cb {
		if r.cbPanic.Load() != nil {
			continue
		}
		func() {
			defer func() {
				if v := recover(); v != nil {
					r.cbPanic.Store(fmt.Sprint(v))
					signalGroup(r.cmd, true)
				}
			}()
			r.spec.OnEvent(e)
		}()
	}
}

// finishCallback waits, for a bounded time, for the callback to take what it
// was given.
func (r *run) finishCallback(o *Outcome) {
	if r.cb == nil {
		return
	}
	close(r.cb)
	select {
	case <-r.cbDone:
	case <-time.After(r.lim.CallbackWait):
		r.note("the OnEvent callback was too slow to finish: the run did not wait for it")
	}
	if o.CallbackDropped > 0 {
		r.note("the OnEvent callback was too slow: %d events were not delivered to it", o.CallbackDropped)
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

// lookForStrays reports processes that outlived the group, where the system
// lets the runner look cheaply, and says so where it does not.
func (r *run) lookForStrays(o *Outcome) {
	pids, ok := strayProcesses(r.dir)
	switch {
	case !ok:
		r.note("processes that left the process group, for example with a new session, are not detected on this system")
	case len(pids) > 0:
		o.Strays = pids
		r.note("processes %v still have the run directory as their working directory or an open file. They are outside the process group and were not stopped", pids)
	}
}

func (r *run) finish(ctx context.Context, o *Outcome) {
	o.StderrHead = repo.Visible(string(r.stderrHead), 2000)
	var res *api.Result
	in := verdictInput{TimedOut: o.TimedOut, Cancelled: o.Cancelled, ExitCode: o.ExitCode}
	var commit string
	var changed bool
	var early string
	switch {
	case r.cbPanic.Load() != nil:
		early = fmt.Sprintf("the OnEvent callback panicked: %v", r.cbPanic.Load())
	case o.TimedOut || o.Cancelled || o.ExitCode != 0:
	default:
		data, err := readResult(r.root, r.lim.MaxResultBytes)
		if err != nil {
			early = fmt.Sprintf("no usable result.json: %v", err)
			break
		}
		if res, err = api.ParseResult(data); err != nil {
			early = fmt.Sprintf("result.json is not valid: %v", err)
			break
		}
		in.Result = res
		if wantsImport(res) {
			commit, changed, early = r.importWorkspace(ctx, &in)
		}
	}
	if r.cancelled(ctx) && !o.TimedOut {
		in.Cancelled, o.Cancelled = true, true
	}
	o.Status, o.Reason = classify(in)
	if early != "" && o.Status != StatusCancelled && o.Status != StatusTimedOut {
		o.Status, o.Reason = StatusFailed, early
	}
	switch o.Status {
	case StatusSucceeded, StatusSucceededNoChange:
		o.Result, o.Commit, o.Changed = res, commit, changed
	case StatusNeedsHuman:
		o.Result = res
	case StatusFailed:
		if res != nil && res.Status == api.StatusFailed && o.Reason != "" {
			o.Reason += ": " + repo.Visible(res.Summary, 300)
		}
	}
	o.Notes = append(o.Notes, r.notes...)
	if o.Status == StatusFailed || o.Status == StatusTimedOut || o.Status == StatusCancelled {
		if r.spec.KeepFailed {
			o.RunDir, o.EventsPath, o.StderrPath = r.dir, r.eventsPath(), r.stderrPath()
			if err := r.sameDir(); err != nil {
				o.Notes = append(o.Notes, "the kept run directory may not be at its path: "+err.Error())
			}
			r.closeRoots()
			return
		}
	}
	o.Notes = append(o.Notes, r.cleanup()...)
}

// importWorkspace imports the workspace and checks, before and after, that
// the run directory's name still names the directory that was made.
func (r *run) importWorkspace(ctx context.Context, in *verdictInput) (commit string, changed bool, early string) {
	if err := r.sameDir(); err != nil {
		return "", false, err.Error()
	}
	if beforeImport != nil {
		beforeImport(r)
	}
	msg := r.spec.Message
	if msg == "" {
		msg = "factory: work stage of a run"
	}
	commit, changed, err := r.spec.Repo.Import(ctx, r.ws, r.sha, msg, r.spec.Author,
		repo.ImportOptions{Limits: r.spec.RepoLimits, Exclude: r.spec.ImportExclude})
	in.ImportErr, in.Changed = err, changed
	if err != nil {
		return "", false, ""
	}
	if err := r.sameDir(); err != nil {
		return "", false, err.Error()
	}
	return commit, changed, ""
}

func (r *run) closeRoots() {
	if r.root != nil {
		r.root.Close()
	}
	if r.runsRoot != nil {
		r.runsRoot.Close()
	}
}

// cleanup removes exactly the directory that was made, found by its handle.
// When the name no longer names it, the agent moved it. The contents are
// still removed through the handle. A moved copy that is still in the runs
// folder is removed, and a link left at the name is removed (not followed).
// Anything else is left, and reported: the runner deletes nothing it is not
// sure about.
func (r *run) cleanup() (notes []string) {
	defer r.closeRoots()
	if r.root == nil || r.runsRoot == nil || r.dir == "" {
		return nil
	}
	if err := clearDir(r.root); err != nil {
		notes = append(notes, fmt.Sprintf("the run directory could not be fully emptied: %v", err))
	}
	name := filepath.Base(r.dir)
	if fi, err := r.runsRoot.Lstat(name); err == nil && os.SameFile(fi, r.rootInfo) {
		if err := r.runsRoot.Remove(name); err != nil {
			notes = append(notes, fmt.Sprintf("the run directory could not be removed: %v", err))
		}
		return notes
	}
	notes = append(notes, "the agent moved or replaced the run directory")
	removed := false
	if fi, err := r.runsRoot.Lstat(name); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if r.runsRoot.Remove(name) == nil {
			removed = true
		}
	} else if err == nil {
		notes = append(notes, "something else is at the run directory's name. It was left alone")
	}
	if f, err := r.runsRoot.Open("."); err == nil {
		ents, _ := f.ReadDir(-1)
		f.Close()
		for _, e := range ents {
			if fi, err := r.runsRoot.Lstat(e.Name()); err == nil && os.SameFile(fi, r.rootInfo) {
				if r.runsRoot.Remove(e.Name()) == nil {
					removed = true
				}
			}
		}
	}
	if !removed {
		notes = append(notes, "the run directory was moved away from the runs folder. Its contents were removed, and an empty directory may remain where it was moved")
	}
	return notes
}

// clearDir removes everything inside dir, through the handle. A directory
// the agent made unreadable is opened up first. A link is removed, never
// followed.
func clearDir(dir *os.Root) error {
	dir.Chmod(".", 0o700)
	f, err := dir.Open(".")
	if err != nil {
		return err
	}
	ents, err := f.ReadDir(-1)
	f.Close()
	var first error
	for _, e := range ents {
		name := e.Name()
		if e.Type().IsDir() {
			dir.Chmod(name, 0o700)
			if sub, err := dir.OpenRoot(name); err == nil {
				if err := clearDir(sub); err != nil && first == nil {
					first = err
				}
				sub.Close()
			} else if first == nil {
				first = err
			}
		}
		if err := dir.Remove(name); err != nil && first == nil {
			first = err
		}
	}
	if err != nil && first == nil {
		first = err
	}
	return first
}
