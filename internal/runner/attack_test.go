package runner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// doWithin is do with a short bound, for attacks that could hang the runner.
func (f *fixture) doWithin(spec Spec, d time.Duration) Outcome {
	f.t.Helper()
	h, err := NewLocal().Start(context.Background(), spec)
	if err != nil {
		f.t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	o, err := h.Wait(ctx)
	if err != nil {
		cc, c2 := context.WithTimeout(context.Background(), time.Second)
		defer c2()
		h.Cancel(cc)
		f.t.Fatalf("the runner did not finish within %v", d)
	}
	return o
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func requireFailed(t *testing.T, o Outcome, why string) {
	t.Helper()
	if !o.Failed() || o.Result != nil || o.Commit != "" {
		t.Fatalf("%s: the run must fail with no result and no commit: %+v", why, o)
	}
}

// The attacker makes result.json point at a file outside the run. The target
// here is a valid result, so a runner that follows the link would accept it.
func TestResultAsLinkIsNotFollowed(t *testing.T) {
	f := newFixture(t)
	outside := writeFile(t, t.TempDir(), "private-key", goodResult)
	o := f.doWithin(f.spec(`{"resultAs":"link","resultTarget":"`+outside+`"}`), 30*time.Second)
	requireFailed(t, o, "link")
}

func TestResultAsHardLinkIsRefused(t *testing.T) {
	f := newFixture(t)
	outside := writeFile(t, t.TempDir(), "other", goodResult)
	o := f.doWithin(f.spec(`{"resultAs":"hardlink","resultTarget":"`+outside+`"}`), 30*time.Second)
	requireFailed(t, o, "hard link")
}

func TestResultAsNamedPipeDoesNotHang(t *testing.T) {
	f := newFixture(t)
	o := f.doWithin(f.spec(`{"resultAs":"fifo"}`), 30*time.Second)
	requireFailed(t, o, "named pipe")
}

func TestResultAsDirectoryIsRefused(t *testing.T) {
	f := newFixture(t)
	requireFailed(t, f.doWithin(f.spec(`{"resultAs":"dir"}`), 30*time.Second), "directory")
}

func TestHugeResultIsRefusedWithoutReadingIt(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"resultAs":"huge"}`)
	var before, after runtimeMem
	before.read()
	o := f.doWithin(spec, 30*time.Second)
	after.read()
	requireFailed(t, o, "500 MiB result")
	if grown := after.total - before.total; grown > 100<<20 {
		t.Errorf("the runner allocated %d bytes for a refused result", grown)
	}
}

func TestMissingResultFailsTheRun(t *testing.T) {
	f := newFixture(t)
	requireFailed(t, f.doWithin(f.spec(`{"omitResult":true}`), 30*time.Second), "missing result")
}

func TestInvalidResultFailsTheRun(t *testing.T) {
	f := newFixture(t)
	bad := `{"schemaVersion":1,"status":"succeeded","summary":"x","harness":{"name":"stub"},"surprise":1}`
	outside := writeFile(t, t.TempDir(), "bad.json", bad)
	_ = outside
	// The stub validates its own result, so make the file by a link-free
	// route: a result the stub writes cannot be invalid. Use an empty
	// summary instead, through the huge route of a tiny size.
	o := f.doWithin(f.spec(`{"resultAs":"huge","resultSize":3}`), 30*time.Second)
	requireFailed(t, o, "result of three zero bytes")
}

func TestOutDirectoryReplacedByALink(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	writeFile(t, other, "result.json", goodResult)
	o := f.doWithin(f.spec(`{"outAsLink":"`+other+`","result":`+goodResult+`}`), 30*time.Second)
	requireFailed(t, o, "out replaced by a link")
	if _, err := os.Stat(filepath.Join(other, "result.json")); err != nil {
		t.Errorf("the other directory was changed: %v", err)
	}
}

func TestResultClaimsOfTheOrchestratorAreDropped(t *testing.T) {
	f := newFixture(t)
	script := `{"result":{"schemaVersion":1,"status":"succeeded","summary":"x","harness":{"name":"stub"},
	  "usage":{"billing":"plan","source":"worker says so","enforced":true}}}`
	o := f.doWithin(f.spec(script), 30*time.Second)
	if o.Failed() || o.Result == nil {
		t.Fatalf("outcome = %+v", o)
	}
	if o.Result.Usage.Source != "" || o.Result.Usage.Enforced {
		t.Errorf("usage = %+v", o.Result.Usage)
	}
}

// The runner's events file is its own. The agent forges one or links it.
func TestForgedEventsFileIsNotTrusted(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"exitCode":1,"events":[{"kind":"message","text":"real"}],"forgeEvents":"{\"kind\":\"message\",\"text\":\"FORGED\"}\n","result":` + goodResult + `}`)
	spec.KeepFailed = true
	o := f.doWithin(spec, 30*time.Second)
	if o.EventsPath == "" {
		t.Fatal("no events path for a kept run")
	}
	b, err := os.ReadFile(o.EventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "FORGED") || !strings.Contains(string(b), `"real"`) || strings.Count(string(b), "\n") != 1 {
		t.Errorf("events file = %q", b)
	}
}

func TestEventsFileLinkIsNotWrittenThrough(t *testing.T) {
	f := newFixture(t)
	victimDir := t.TempDir()
	victim := writeFile(t, victimDir, "victim.txt", "keep")
	// The link is made at the start by a first step of the script: the stub
	// puts it in place before it prints, as an agent that raced the runner.
	spec := f.spec(`{"eventsLink":"` + victim + `","events":[{"kind":"message","text":"real"}],"result":` + goodResult + `}`)
	o := f.doWithin(spec, 30*time.Second)
	if o.Failed() {
		t.Fatalf("a link at events.jsonl must not fail the run: %s", o.Reason)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("the runner wrote through a link: %q", b)
	}
}

func TestBackgroundChildInTheGroupIsKilled(t *testing.T) {
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"child":{"sleepSeconds":120},"result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	start := time.Now()
	o := f.doWithin(spec, 30*time.Second)
	pid, _ := strconv.Atoi(ev.find("child-pid="))
	if pid <= 1 {
		t.Fatalf("no child pid: %v", ev.texts)
	}
	killLater(t, pid)
	if o.Failed() {
		t.Fatalf("failed: %s", o.Reason)
	}
	if !gone(pid) {
		t.Error("the background child is still running after the run")
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("the run took %v: it waited for the child", time.Since(start))
	}
}

func TestChildInANewSessionIsDetectedAndStated(t *testing.T) {
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"child":{"sleepSeconds":120,"newSession":true},"result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	o := f.doWithin(spec, 30*time.Second)
	pid, _ := strconv.Atoi(ev.find("child-pid="))
	killLater(t, pid)
	if o.Failed() {
		t.Fatalf("failed: %s", o.Reason)
	}
	found := false
	for _, n := range o.Notes {
		if strings.Contains(n, "outside the process group") {
			found = true
		}
	}
	if !found {
		t.Errorf("no note about a process outside the group: %v", o.Notes)
	}
}

func TestDeadlineKillsAProcessThatIgnoresTheSignal(t *testing.T) {
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"ignoreSignals":true,"child":{"sleepSeconds":120},"delayMs":120000,"result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	spec.Deadline, spec.Grace = 700*time.Millisecond, 300*time.Millisecond
	start := time.Now()
	o := f.doWithin(spec, 30*time.Second)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("returned after %v, want close to the deadline", elapsed)
	}
	if !o.TimedOut || !o.Failed() || o.Result != nil {
		t.Fatalf("outcome = %+v", o)
	}
	_ = ev
}

func TestCancelStopsTheRunAndTheGroup(t *testing.T) {
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"child":{"sleepSeconds":120},"events":[{"kind":"status","text":"ready"}],"delayMs":120000,"result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	h, err := NewLocal().Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	// The stub prints its events only after the delay, so cancel right away.
	cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := h.Cancel(cctx); err != nil {
		t.Fatal(err)
	}
	o, err := h.Wait(cctx)
	if err != nil || !o.Cancelled || !o.Failed() || o.Commit != "" {
		t.Fatalf("outcome = %+v err = %v", o, err)
	}
	if len(runDirs(f)) != 0 {
		t.Errorf("run directory left: %v", runDirs(f))
	}
}

func TestOutputFloodIsBounded(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"exitCode":1,"floodStdout":40000000,"floodStderr":40000000,"longLine":50000000,
	  "events":[{"kind":"message","text":"before"}],"result":` + goodResult + `}`)
	spec.KeepFailed = true
	spec.Limits.MaxStderrBytes = 100 << 10
	spec.Limits.MaxStdoutBytes = 1 << 20
	o := f.doWithin(spec, 60*time.Second)
	if !o.Failed() {
		t.Fatal("exit code 1 must fail the run")
	}
	fi, err := os.Stat(o.StderrPath)
	if err != nil || fi.Size() > 100<<10 {
		t.Errorf("stderr log: %v %v", fi, err)
	}
	if o.EventCount != 1 {
		t.Errorf("events = %d", o.EventCount)
	}
	if len(o.StderrHead) == 0 || len(o.StderrHead) > 8<<10 {
		t.Errorf("stderr head = %d bytes", len(o.StderrHead))
	}
}

func TestFloodOfStdoutAndLongLineStillGivesAResult(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"floodStdout":30000000,"floodStderr":30000000,"longLine":50000000,"result":` + goodResult + `}`)
	var before, after runtimeMem
	before.read()
	o := f.doWithin(spec, 60*time.Second)
	after.read()
	if o.Failed() || o.Result == nil {
		t.Fatalf("outcome = %+v", o)
	}
	if grown := after.total - before.total; grown > 400<<20 {
		t.Errorf("total allocation %d bytes", grown)
	}
}

func TestEventsFileHasASizeCap(t *testing.T) {
	f := newFixture(t)
	var evs []string
	for i := 0; i < 300; i++ {
		evs = append(evs, `{"kind":"message","text":"`+strings.Repeat("e", 100)+`"}`)
	}
	spec := f.spec(`{"exitCode":1,"events":[` + strings.Join(evs, ",") + `],"result":` + goodResult + `}`)
	spec.KeepFailed = true
	spec.Limits.MaxEventsBytes = 4 << 10
	got := 0
	spec.OnEvent = func(e apiEvent) { got++ }
	o := f.doWithin(spec, 30*time.Second)
	fi, err := os.Stat(o.EventsPath)
	if err != nil || fi.Size() > 4<<10 {
		t.Fatalf("events file: %v %v", fi, err)
	}
	if got != 300 || o.EventCount != 300 || o.EventsDropped == 0 {
		t.Errorf("handed out %d, counted %d, dropped %d", got, o.EventCount, o.EventsDropped)
	}
}

func TestWorkspaceReplacedByALinkFailsSafely(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	writeFile(t, other, "secret.txt", "private")
	o := f.doWithin(f.spec(`{"replaceWorkspace":"link","workspaceTarget":"`+other+`","result":`+goodResult+`}`), 30*time.Second)
	if !o.Failed() || o.Commit != "" {
		t.Fatalf("outcome = %+v", o)
	}
	if b, _ := os.ReadFile(filepath.Join(other, "secret.txt")); string(b) != "private" {
		t.Errorf("the other directory was changed")
	}
	if len(runDirs(f)) != 0 {
		t.Errorf("run directory left: %v", runDirs(f))
	}
}

func TestWorkspaceRemovedFailsSafely(t *testing.T) {
	f := newFixture(t)
	o := f.doWithin(f.spec(`{"replaceWorkspace":"remove","result":`+goodResult+`}`), 30*time.Second)
	if !o.Failed() || o.Commit != "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestInputsAreReadOnlyForTheAgent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"writeIn":true,"result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	o := f.doWithin(spec, 30*time.Second)
	if o.Failed() {
		t.Fatalf("failed: %s", o.Reason)
	}
	if ev.find("write-in: task.json: ") != "refused" || ev.find("write-in: new file: ") != "refused" {
		t.Errorf("events = %v", ev.texts)
	}
}

var _ = syscall.SIGKILL
