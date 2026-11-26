package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

const resultA = `{"schemaVersion":1,"status":"succeeded","summary":"A result","harness":{"name":"stub"}}`
const resultB = `{"schemaVersion":1,"status":"succeeded","summary":"B result","harness":{"name":"stub"}}`

// keptRun makes a failed run that stays on disk, as the victim of a swap.
func keptRun(f *fixture) Outcome {
	spec := f.spec(`{"exitCode":1,"files":[{"path":"b-secret.txt","content":"B"}],"result":` + resultB + `}`)
	spec.KeepFailed = true
	return f.do(spec)
}

func exportTo(t *testing.T, f *fixture, commit string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "x")
	if _, err := f.repo.Export(context.Background(), commit, dest, repoLimits()); err != nil {
		t.Fatal(err)
	}
	return dest
}

func TestSwapToAnotherRunIsRefused(t *testing.T) {
	f := newFixture(t)
	b := keptRun(f)
	if b.RunDir == "" {
		t.Fatal("run B was not kept")
	}
	o := f.doWithin(f.spec(`{"swapRunDir":{"mode":"link","target":"`+b.RunDir+`"},"result":`+resultA+`}`), 30*time.Second)
	requireFailed(t, o, "swap to another run")
	if o.Status != StatusFailed {
		t.Errorf("status = %v", o.Status)
	}
	if b, err := os.ReadFile(filepath.Join(b.RunDir, "workspace", "b-secret.txt")); err != nil || string(b) != "B" {
		t.Errorf("run B was changed: %q %v", b, err)
	}
	if left := runDirs(f); len(left) != 1 {
		t.Errorf("only run B may be left, got %v", left)
	}
}

func TestSwapToADirectoryTheAgentBuiltIsRefused(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	for _, d := range []string{"workspace", "out"} {
		if err := os.Mkdir(filepath.Join(other, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(other, "workspace"), "evil.txt", "evil")
	writeFile(t, filepath.Join(other, "out"), "result.json", resultB)
	o := f.doWithin(f.spec(`{"swapRunDir":{"mode":"link","target":"`+other+`"},"result":`+resultA+`}`), 30*time.Second)
	requireFailed(t, o, "swap to a built directory")
	if _, err := os.Stat(filepath.Join(other, "workspace", "evil.txt")); err != nil {
		t.Errorf("the built directory was changed: %v", err)
	}
	if left := runDirs(f); len(left) != 0 {
		t.Errorf("run directory left: %v", left)
	}
}

// The agent swaps its run directory away and back again, many times, while
// the runner reads. Whatever the runner returns must be this run's own.
func TestSwapAndBackNeverGivesForeignData(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	for _, d := range []string{"workspace", "out"} {
		os.Mkdir(filepath.Join(other, d), 0o755)
	}
	writeFile(t, filepath.Join(other, "workspace"), "evil.txt", "evil")
	writeFile(t, filepath.Join(other, "out"), "result.json", resultB)
	for i := 0; i < 8; i++ {
		spec := f.spec(`{"files":[{"path":"mine.txt","content":"mine"}],"swapRunDir":{"mode":"loop","target":"` + other + `","loopMs":120},"result":` + resultA + `}`)
		o := f.doWithin(spec, 30*time.Second)
		if o.Failed() {
			continue // refusing is allowed
		}
		if o.Result == nil || o.Result.Summary != "A result" {
			t.Fatalf("round %d: foreign result: %+v", i, o.Result)
		}
		dest := exportTo(t, f, o.Commit)
		if _, err := os.Stat(filepath.Join(dest, "evil.txt")); err == nil {
			t.Fatalf("round %d: foreign file in the commit", i)
		}
		if _, err := os.Stat(filepath.Join(dest, "mine.txt")); err != nil {
			t.Fatalf("round %d: own file missing: %v", i, err)
		}
	}
	if left := runDirs(f); len(left) != 0 {
		t.Errorf("run directories left: %v", left)
	}
}

func TestMovedAwayDirectoryIsEmptiedAndReported(t *testing.T) {
	f := newFixture(t)
	base := t.TempDir()
	escaped := filepath.Join(base, "escaped")
	o := f.doWithin(f.spec(`{"files":[{"path":"w.txt","content":"x"}],"swapRunDir":{"mode":"away","target":"`+base+`","moveTo":"`+escaped+`"},"result":`+resultA+`}`), 30*time.Second)
	requireFailed(t, o, "moved away")
	if left := runDirs(f); len(left) != 0 {
		t.Errorf("the link was not removed: %v", left)
	}
	ents, err := os.ReadDir(escaped)
	if err != nil || len(ents) != 0 {
		t.Errorf("the moved directory must be emptied through the handle: %v %v", ents, err)
	}
	if !strings.Contains(strings.Join(o.Notes, "\n"), "moved") {
		t.Errorf("no note about the leftover: %v", o.Notes)
	}
}

func TestMovedCopyInsideRunsIsRemoved(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	o := f.doWithin(f.spec(`{"swapRunDir":{"mode":"link","target":"`+other+`"},"result":`+resultA+`}`), 30*time.Second)
	requireFailed(t, o, "link")
	if left := runDirs(f); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestRunDirectoryNamesAreUnguessable(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"exitCode":1,"result":` + resultA + `}`)
	spec.KeepFailed = true
	o := f.do(spec)
	if ok, _ := regexp.MatchString(`^[0-9a-f]{32,}$`, filepath.Base(o.RunDir)); !ok {
		t.Errorf("run directory name = %q", filepath.Base(o.RunDir))
	}
	fi, err := os.Stat(filepath.Dir(o.RunDir))
	if err != nil || fi.Mode().Perm() != 0o700 || filepath.Base(filepath.Dir(o.RunDir)) != "runs" {
		t.Errorf("runs folder: %v %v", fi, err)
	}
}

func TestStatusOfEachOutcome(t *testing.T) {
	mk := func(status string, q bool) *api.Result {
		r := &api.Result{SchemaVersion: 1, Status: status, Summary: "s", Harness: api.HarnessInfo{Name: "stub"}}
		if q {
			r.Questions = []string{"which?"}
		}
		return r
	}
	boom := errors.New("refused")
	tests := []struct {
		name string
		in   verdictInput
		want Status
	}{
		{"timed out", verdictInput{TimedOut: true, Result: mk("succeeded", false)}, StatusTimedOut},
		{"cancelled", verdictInput{Cancelled: true, Result: mk("succeeded", false)}, StatusCancelled},
		{"cancelled in import", verdictInput{Cancelled: true, Result: mk("succeeded", false), Changed: true}, StatusCancelled},
		{"nonzero exit", verdictInput{ExitCode: 3, Result: mk("succeeded", false), Changed: true}, StatusFailed},
		{"killed by a signal", verdictInput{ExitCode: -1, Result: mk("succeeded", false)}, StatusFailed},
		{"no result", verdictInput{}, StatusFailed},
		{"succeeded with a change", verdictInput{Result: mk("succeeded", false), Changed: true}, StatusSucceeded},
		{"succeeded no change", verdictInput{Result: mk("succeeded", false)}, StatusSucceededNoChange},
		{"succeeded, import refused", verdictInput{Result: mk("succeeded", false), ImportErr: boom}, StatusFailed},
		{"no_change and nothing changed", verdictInput{Result: mk("no_change", false)}, StatusSucceededNoChange},
		{"no_change but files changed", verdictInput{Result: mk("no_change", false), Changed: true}, StatusFailed},
		{"no_change, import refused", verdictInput{Result: mk("no_change", false), ImportErr: boom}, StatusFailed},
		{"failed, nothing changed", verdictInput{Result: mk("failed", false)}, StatusFailed},
		{"failed but asking to be imported", verdictInput{Result: mk("failed", false), Changed: true}, StatusFailed},
		{"needs a person", verdictInput{Result: mk("needs_human", true)}, StatusNeedsHuman},
		{"needs a person with edits", verdictInput{Result: mk("needs_human", true), Changed: true}, StatusNeedsHuman},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := classify(tt.in)
			if got != tt.want {
				t.Errorf("status = %v, want %v (%s)", got, tt.want, reason)
			}
			if (got == StatusFailed || got == StatusTimedOut || got == StatusCancelled) && reason == "" {
				t.Error("a failure needs a reason")
			}
		})
	}
	// Only a succeeded or no_change result is imported.
	for _, s := range []string{"failed", "needs_human"} {
		if wantsImport(mk(s, s == "needs_human")) {
			t.Errorf("%s must not be imported", s)
		}
	}
	if !wantsImport(mk("succeeded", false)) || !wantsImport(mk("no_change", false)) || wantsImport(nil) {
		t.Error("wantsImport")
	}
}

func TestZeroOutcomeMeansFailed(t *testing.T) {
	var o Outcome
	if !o.Failed() || o.Status != StatusFailed {
		t.Errorf("zero outcome: %+v", o)
	}
	for _, s := range []Status{StatusFailed, StatusTimedOut, StatusCancelled, StatusUnknown} {
		if !(Outcome{Status: s}).Failed() {
			t.Errorf("%v must count as failed", s)
		}
	}
	for _, s := range []Status{StatusSucceeded, StatusSucceededNoChange, StatusNeedsHuman} {
		if (Outcome{Status: s}).Failed() {
			t.Errorf("%v must not count as failed", s)
		}
	}
	if StatusUnknown.String() == "" || StatusSucceeded.String() == StatusFailed.String() {
		t.Error("status names")
	}
}

func TestEndToEndStatuses(t *testing.T) {
	f := newFixture(t)
	file := `"files":[{"path":"e.txt","content":"x"}],`
	need := `{"schemaVersion":1,"status":"needs_human","summary":"s","questions":["which?"],"harness":{"name":"stub"}}`
	tests := []struct {
		name, script string
		want         Status
		commit       bool
	}{
		{"failed with edits", `{` + file + `"result":{"schemaVersion":1,"status":"failed","summary":"s","harness":{"name":"stub"}}}`, StatusFailed, false},
		{"no_change with edits", `{` + file + `"result":{"schemaVersion":1,"status":"no_change","summary":"s","harness":{"name":"stub"}}}`, StatusFailed, false},
		{"needs a person", `{` + file + `"result":` + need + `}`, StatusNeedsHuman, false},
		{"succeeded with edits", `{` + file + `"result":` + resultA + `}`, StatusSucceeded, true},
		{"succeeded, no edits", `{"result":` + resultA + `}`, StatusSucceededNoChange, true},
	}
	for _, tt := range tests {
		o := f.doWithin(f.spec(tt.script), 30*time.Second)
		if o.Status != tt.want || (o.Commit != "") != tt.commit {
			t.Errorf("%s: status %v commit %q reason %q", tt.name, o.Status, o.Commit, o.Reason)
		}
		if o.Status == StatusNeedsHuman && (o.Result == nil || len(o.Result.Questions) == 0) {
			t.Errorf("%s: the questions must be kept", tt.name)
		}
	}
}

func TestWaitErrorGivesUnknownStatus(t *testing.T) {
	f := newFixture(t)
	h, err := NewLocal().Start(context.Background(), f.spec(`{"delayMs":60000,"result":`+resultA+`}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	o, err := h.Wait(ctx)
	if err == nil || o.Status != StatusUnknown || !o.Failed() {
		t.Errorf("o=%+v err=%v", o, err)
	}
	c2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()
	if err := h.Cancel(c2); err != nil {
		t.Fatal(err)
	}
}

func TestPanicInTheCallbackFailsTheRunAndKillsTheGroup(t *testing.T) {
	f := newFixture(t)
	pid := 0
	spec := f.spec(`{"child":{"sleepSeconds":120},"events":[{"kind":"message","text":"one"},{"kind":"message","text":"two"}],"result":` + resultA + `}`)
	spec.OnEvent = func(e api.Event) {
		if v, ok := strings.CutPrefix(e.Text, "child-pid="); ok {
			pid, _ = strconv.Atoi(v)
		}
		panic("callback bug")
	}
	o := f.doWithin(spec, 30*time.Second)
	killLater(t, pid)
	requireFailed(t, o, "panic")
	if !strings.Contains(o.Reason, "callback") && !strings.Contains(strings.Join(o.Notes, "\n"), "callback") {
		t.Errorf("reason=%q notes=%v", o.Reason, o.Notes)
	}
	if pid <= 1 || !gone(pid) {
		t.Errorf("the child must be gone (pid %d)", pid)
	}
	if len(runDirs(f)) != 0 {
		t.Errorf("run directory left: %v", runDirs(f))
	}
}

func TestSlowCallbackDoesNotBlockTheRun(t *testing.T) {
	f := newFixture(t)
	var evs []string
	for i := 0; i < 5000; i++ {
		evs = append(evs, `{"kind":"status","text":"e"}`)
	}
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	spec := f.spec(`{"events":[` + strings.Join(evs, ",") + `],"result":` + resultA + `}`)
	spec.Limits.CallbackWait = 300 * time.Millisecond
	spec.OnEvent = func(api.Event) { <-release }
	o := f.doWithin(spec, 30*time.Second)
	if o.Failed() || o.Result == nil {
		t.Fatalf("outcome = %+v", o)
	}
	if o.CallbackDropped == 0 || o.EventCount != 5001 && o.EventCount != 5000 {
		t.Errorf("dropped %d, counted %d", o.CallbackDropped, o.EventCount)
	}
	if !strings.Contains(strings.Join(o.Notes, "\n"), "too slow") {
		t.Errorf("notes = %v", o.Notes)
	}
}

func TestExitCodeOfTimeoutAndCancelIsTheSame(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"delayMs":120000,"result":` + resultA + `}`)
	spec.Deadline = 500 * time.Millisecond
	o := f.doWithin(spec, 30*time.Second)
	if o.Status != StatusTimedOut || !o.TimedOut || o.ExitCode != -1 {
		t.Errorf("timeout: %+v", o)
	}
	h, err := NewLocal().Start(context.Background(), f.spec(`{"delayMs":120000,"result":`+resultA+`}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h.Cancel(ctx)
	c, _ := h.Wait(ctx)
	if c.Status != StatusCancelled || !c.Cancelled || c.ExitCode != -1 {
		t.Errorf("cancel: %+v", c)
	}
}

func TestCancelDuringImportIsCancelled(t *testing.T) {
	f := newFixture(t)
	beforeImport = func(r *run) { r.requestCancel() }
	t.Cleanup(func() { beforeImport = nil })
	o := f.doWithin(f.spec(`{"files":[{"path":"a","content":"x"}],"result":`+resultA+`}`), 30*time.Second)
	if o.Status != StatusCancelled || !o.Cancelled || o.Commit != "" {
		t.Errorf("outcome = %+v", o)
	}
}

func TestProcessesOutsideTheGroupAreNeverSilent(t *testing.T) {
	f := newFixture(t)
	o := f.doWithin(f.spec(`{"result":`+resultA+`}`), 30*time.Second)
	notes := strings.Join(o.Notes, "\n")
	if runtime.GOOS == "linux" {
		t.Skip("on linux the runner looks for them: see the stray test")
	}
	if !strings.Contains(notes, "not detected") {
		t.Errorf("a standing note is required where detection is not possible: %v", o.Notes)
	}
}
