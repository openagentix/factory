package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// StubScript is the script a stub harness follows. The stub is deterministic.
// It is used by tests and by demo mode, and it makes no model call.
type StubScript struct {
	// Files are written in the workspace.
	Files []StubFile `json:"files,omitempty"`
	// Delete lists workspace paths to remove.
	Delete []string `json:"delete,omitempty"`
	// Result is written as result.json. When empty, a succeeded result with
	// a short summary is written.
	Result *api.Result `json:"result,omitempty"`
	// OmitResult writes no result.json, to test a run that forgot it.
	OmitResult bool `json:"omitResult,omitempty"`
	// Events are written to events.jsonl and to standard output.
	Events []StubEvent `json:"events,omitempty"`
	// Denials and Usage are reported in the final line of standard output.
	Denials []api.Denial `json:"denials,omitempty"`
	Usage   *api.Usage   `json:"usage,omitempty"`
	// DelayMS waits before acting. A cancelled context ends the wait.
	DelayMS int `json:"delayMs,omitempty"`
	// TouchProtected is a workspace path the stub writes, as a worker that
	// edits a protected path would.
	TouchProtected string `json:"touchProtected,omitempty"`
	// ExitCode is the exit code, returned after all outputs are written.
	ExitCode int `json:"exitCode,omitempty"`

	// The fields below act as a hostile worker would. They exist so the
	// runner can be tested against each behaviour with a real process.
	// IgnoreSignals makes the stub ignore the terminating signals, so only a
	// kill ends it.
	IgnoreSignals bool `json:"ignoreSignals,omitempty"`
	// Child starts a background process that outlives the stub.
	Child *StubChild `json:"child,omitempty"`
	// FloodStdout and FloodStderr are byte counts of junk to print. LongLine
	// is the size of one single line on standard output.
	FloodStdout int `json:"floodStdout,omitempty"`
	FloodStderr int `json:"floodStderr,omitempty"`
	LongLine    int `json:"longLine,omitempty"`
	// ResultAs makes result.json something else than a plain file: link,
	// hardlink (both to ResultTarget, an absolute path), dir, fifo or huge
	// (a sparse file of ResultSize bytes, 500 MiB when zero).
	ResultAs     string `json:"resultAs,omitempty"`
	ResultTarget string `json:"resultTarget,omitempty"`
	ResultSize   int64  `json:"resultSize,omitempty"`
	// OutAsLink replaces the out directory by a link to this directory,
	// after everything is written.
	OutAsLink string `json:"outAsLink,omitempty"`
	// ForgeEvents replaces events.jsonl with this text. EventsLink replaces
	// it with a link to this absolute path.
	ForgeEvents string `json:"forgeEvents,omitempty"`
	EventsLink  string `json:"eventsLink,omitempty"`
	// ReplaceWorkspace removes the workspace directory ("remove") or swaps
	// it for a link to WorkspaceTarget ("link"), after everything is written.
	ReplaceWorkspace string `json:"replaceWorkspace,omitempty"`
	WorkspaceTarget  string `json:"workspaceTarget,omitempty"`
	// WriteIn tries to write in FACTORY_IN and reports what happened.
	WriteIn bool `json:"writeIn,omitempty"`
	// DumpEnv reports every environment variable the stub saw.
	DumpEnv bool `json:"dumpEnv,omitempty"`
	// SwapRunDir moves the run directory (the parent of the workspace) away
	// and puts a link in its place. Mode "link" keeps the real directory
	// beside it as <run>.moved and leaves the link. Mode "away" moves it to
	// MoveTo. Mode "loop" swaps and restores it again and again for LoopMs
	// milliseconds and ends with the real directory back in place.
	SwapRunDir *StubSwap `json:"swapRunDir,omitempty"`
}

// StubSwap is the swap of the run directory.
type StubSwap struct {
	Mode   string `json:"mode"`
	Target string `json:"target"`
	MoveTo string `json:"moveTo,omitempty"`
	LoopMS int    `json:"loopMs,omitempty"`
}

// StubChild is a background process the stub leaves behind.
type StubChild struct {
	// SleepSeconds is how long it sleeps.
	SleepSeconds int `json:"sleepSeconds"`
	// NewSession starts it in a new session, outside the process group.
	NewSession bool `json:"newSession,omitempty"`
}

// StubFile is one file to write.
type StubFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// StubEvent is one event to emit.
type StubEvent struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// StubOptions says where the stub acts.
type StubOptions struct {
	// Script is the JSON script.
	Script    []byte
	Workspace string
	OutDir    string
	// Stdout gets the event stream the stub adapter reads.
	Stdout io.Writer
	// Stderr gets the junk of FloodStderr. Empty means discard.
	Stderr io.Writer
	// Now gives the event time. Empty means time.Now.
	Now func() time.Time
}

// stubUsageExit is the exit code for a bad script or a failed action. It is
// not the script's own exit code.
const stubUsageExit = 2

// RunStub acts on the workspace and the output directory as a real worker
// does under the worker contract: it edits files, writes result.json and
// events.jsonl, and never commits and never touches git state. It returns
// the exit code to use. A non-nil error means the stub itself failed, and
// the code is then non-zero.
func RunStub(ctx context.Context, opts StubOptions) (int, error) {
	now := clock(opts.Now)
	var sc StubScript
	dec := json.NewDecoder(bytes.NewReader(opts.Script))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		return stubUsageExit, fmt.Errorf("stub script: %w", err)
	}
	if dec.More() {
		return stubUsageExit, errors.New("stub script: extra data after the object")
	}
	wsRoot, err := resolveRoot(opts.Workspace, false)
	if err != nil {
		return stubUsageExit, fmt.Errorf("stub workspace: %w", err)
	}
	outRoot, err := resolveRoot(opts.OutDir, true)
	if err != nil {
		return stubUsageExit, fmt.Errorf("stub out directory: %w", err)
	}
	if err := sc.check(); err != nil {
		return stubUsageExit, err
	}
	if err := sc.checkAttackRoot(opts); err != nil {
		return stubUsageExit, err
	}
	// Resolve every target before acting, so a bad path changes nothing.
	for _, p := range sc.paths() {
		if _, err := target(wsRoot, p); err != nil {
			return stubUsageExit, err
		}
	}
	res := sc.result()
	if !sc.OmitResult {
		if err := res.Validate(); err != nil {
			return stubUsageExit, fmt.Errorf("stub script: result: %w", err)
		}
	}

	if sc.IgnoreSignals {
		stop := ignoreSignals()
		defer stop()
	}
	var extra []StubEvent
	if sc.DumpEnv {
		for _, kv := range os.Environ() {
			extra = append(extra, StubEvent{Kind: api.EventStatus, Text: "env " + kv})
		}
	}
	if sc.Child != nil {
		pid, err := startChild(*sc.Child, opts.Stdout)
		if err != nil {
			return stubUsageExit, err
		}
		extra = append(extra, StubEvent{Kind: api.EventStatus, Text: fmt.Sprintf("child-pid=%d", pid)})
	}
	if sc.WriteIn {
		extra = append(extra, tryWriteIn()...)
	}

	if sc.DelayMS > 0 {
		t := time.NewTimer(time.Duration(sc.DelayMS) * time.Millisecond)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return 1, ctx.Err()
		}
	}

	for _, p := range sc.Delete {
		if err := removeSafe(wsRoot, p); err != nil {
			return stubUsageExit, err
		}
	}
	for _, f := range sc.Files {
		if err := writeSafe(wsRoot, f.Path, []byte(f.Content)); err != nil {
			return stubUsageExit, err
		}
	}
	if sc.TouchProtected != "" {
		if err := writeSafe(wsRoot, sc.TouchProtected, []byte("changed by the stub\n")); err != nil {
			return stubUsageExit, err
		}
	}

	var log bytes.Buffer
	out := opts.Stdout
	if out == nil {
		out = io.Discard
	}
	enc := json.NewEncoder(out)
	for _, e := range append(extra, sc.Events...) {
		ev := api.Event{Time: now(), Kind: e.Kind, Text: e.Text}
		b, _ := json.Marshal(ev)
		log.Write(b)
		log.WriteByte('\n')
		if err := enc.Encode(stubLine{Type: "event", Event: &ev}); err != nil {
			return stubUsageExit, err
		}
	}
	if err := sc.flood(out, opts.Stderr); err != nil {
		return stubUsageExit, err
	}
	if err := writeSafe(outRoot, "events.jsonl", log.Bytes()); err != nil {
		return stubUsageExit, err
	}
	if sc.ResultAs != "" {
		if err := makeResultAs(outRoot, sc.ResultAs, sc.ResultTarget, sc.ResultSize); err != nil {
			return stubUsageExit, err
		}
	} else if !sc.OmitResult {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return stubUsageExit, err
		}
		if err := writeSafe(outRoot, "result.json", append(b, '\n')); err != nil {
			return stubUsageExit, err
		}
	}
	if err := sc.tamper(outRoot, opts); err != nil {
		return stubUsageExit, err
	}
	usage := res.Usage
	if sc.Usage != nil {
		usage = *sc.Usage
	}
	final := stubLine{Type: "final", Usage: &usage, Denials: sc.Denials, Turns: 1}
	if err := enc.Encode(final); err != nil {
		return stubUsageExit, err
	}
	return sc.ExitCode, nil
}

// result is the result.json the script asks for, with defaults filled in.
func (sc StubScript) result() api.Result {
	var r api.Result
	if sc.Result != nil {
		r = *sc.Result
	} else {
		r = api.Result{SchemaVersion: api.SchemaVersion, Status: api.StatusSucceeded, Summary: "Stub run."}
	}
	if r.Harness.Name == "" {
		r.Harness.Name = "stub"
	}
	if sc.Usage != nil && r.Usage == (api.Usage{}) {
		r.Usage = *sc.Usage
	}
	if r.Usage.Billing == "" {
		r.Usage.Billing = api.BillingUnknown
	}
	return r
}

// paths lists every workspace path the script acts on.
func (sc StubScript) paths() []string {
	paths := append([]string{}, sc.Delete...)
	for _, f := range sc.Files {
		paths = append(paths, f.Path)
	}
	if sc.TouchProtected != "" {
		paths = append(paths, sc.TouchProtected)
	}
	return paths
}

// check refuses events of an unknown kind. Paths are checked by target.
func (sc StubScript) check() error {
	for i, e := range sc.Events {
		if !(api.Event{Kind: e.Kind}).ValidKind() {
			return fmt.Errorf("stub script: events[%d].kind %q is not known", i, e.Kind)
		}
	}
	return sc.checkAttacks()
}

// resolveRoot checks a directory given on the command line and returns it
// with its symbolic links resolved. The directory must be absolute and
// clean. When create is set, a missing directory is made.
func resolveRoot(dir string, create bool) (string, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" {
		return "", fmt.Errorf("%q must be an absolute, clean path", dir)
	}
	if create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%q is not a directory", dir)
	}
	return real, nil
}

func inside(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// target maps a relative path to a real path inside root. It refuses the
// root itself, a path that leaves root, a .git component in any letter case,
// and any path whose parent resolves through a symbolic link to outside
// root. It creates nothing.
func target(root, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if rel == "" || clean == "." || !filepath.IsLocal(clean) {
		return "", fmt.Errorf("stub script: path %q must name something inside the workspace", rel)
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if strings.EqualFold(part, ".git") {
			return "", fmt.Errorf("stub script: path %q is git state, which a worker does not touch", rel)
		}
	}
	parent := filepath.Join(root, filepath.Dir(clean))
	existing := parent
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		next := filepath.Dir(existing)
		if next == existing {
			break
		}
		existing = next
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	if !inside(root, real) {
		return "", fmt.Errorf("stub script: path %q leaves the workspace through a link", rel)
	}
	rest, err := filepath.Rel(existing, parent)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(real, rest)
	// The check above looked at the words of the path. A link inside the
	// workspace can point at .git, so look at the resolved path too.
	if within, err := filepath.Rel(root, resolved); err == nil {
		for _, part := range strings.Split(filepath.ToSlash(within), "/") {
			if strings.EqualFold(part, ".git") {
				return "", fmt.Errorf("stub script: path %q reaches git state through a link", rel)
			}
		}
	}
	return filepath.Join(resolved, filepath.Base(clean)), nil
}

// writeSafe writes a file inside root. It refuses a directory, and an
// existing regular file that has more than one hard link, because writing it
// would change a file that may live outside root. The content goes to a new
// temporary file in the same directory (created with O_EXCL, so no link is
// followed) and is renamed into place. A link at the target is therefore
// replaced and never followed.
//
// A check-then-write race remains: another process could change a link
// between the checks and the rename. That is acceptable here. The stub is
// test and demo tooling, it runs only when OXF_ALLOW_STUB=1 is set, and the
// only other writer in the directory is the stub's own run.
func writeSafe(root, rel string, data []byte) error {
	t, err := target(root, rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(t); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("stub script: %q is a directory", rel)
		}
		if fi.Mode().IsRegular() && linkCount(fi) > 1 {
			return fmt.Errorf("stub script: %q has more than one hard link", rel)
		}
	}
	tmp, err := os.CreateTemp(dir, ".stub-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, 0o644)); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, t); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// removeSafe deletes a path inside root. A link is removed itself and never
// followed.
func removeSafe(root, rel string) error {
	t, err := target(root, rel)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(t)
	if err != nil {
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return os.Remove(t)
	}
	return os.RemoveAll(t)
}

// stubLine is one line of the stub's standard output.
type stubLine struct {
	Type    string       `json:"type"`
	Event   *api.Event   `json:"event,omitempty"`
	Usage   *api.Usage   `json:"usage,omitempty"`
	Denials []api.Denial `json:"denials,omitempty"`
	Turns   int          `json:"turns,omitempty"`
}

// Stub is the adapter for the stub harness. Its command is the hidden
// "oxf stub-harness" subcommand, so the local runner can start it as a
// process.
type Stub struct {
	// Program is the program that has the stub-harness subcommand.
	Program string
	// AttackRoot, when set, is passed as OXF_STUB_ATTACK_ROOT. The hostile
	// behaviours of the stub act only under that directory.
	AttackRoot string
}

// NewStub returns the stub adapter. program is usually the path of oxf.
func NewStub(program string) *Stub { return &Stub{Program: program} }

// Name implements Adapter.
func (s *Stub) Name() string { return "stub" }

// Command implements Adapter.
func (s *Stub) Command(sp Spec) (Command, error) {
	if err := sp.validate(); err != nil {
		return Command{}, err
	}
	if sp.StubScript == "" {
		return Command{}, errors.New("harness: the stub needs a script")
	}
	prog := s.Program
	if prog == "" {
		prog = "oxf"
	}
	cmd := Command{
		Program: prog,
		Args:    []string{"stub-harness", "--script=" + sp.StubScript, "--workspace=" + sp.Workspace, "--out=" + sp.OutDir},
		// The subcommand refuses to run without this variable.
		Env: append(sp.env(), "OXF_ALLOW_STUB=1"),
		Dir: sp.Workspace,
	}
	if s.AttackRoot != "" {
		cmd.Env = append(cmd.Env, "OXF_STUB_ATTACK_ROOT="+s.AttackRoot)
	}
	if err := checkForbidden(cmd); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

// Read implements Adapter.
func (s *Stub) Read(r io.Reader, emit func(api.Event)) (Summary, error) {
	sum := Summary{Harness: api.HarnessInfo{Name: s.Name()}}
	sum.Usage.Billing = api.BillingUnknown
	started := time.Now()
	err := eachLine(r, func(line []byte) {
		var l stubLine
		if json.Unmarshal(line, &l) != nil {
			sum.Skipped++
			return
		}
		switch l.Type {
		case "event":
			if l.Event == nil || !l.Event.ValidKind() {
				sum.Skipped++
				return
			}
			emit(*l.Event)
		case "final":
			sum.Complete = true
			sum.Turns = l.Turns
			if l.Usage != nil {
				sum.Usage = *l.Usage
			}
			sum.Denials = l.Denials
		default:
			sum.Skipped++
		}
	}, func() { sum.Skipped++ })
	sum.Duration = time.Since(started)
	return sum, err
}
