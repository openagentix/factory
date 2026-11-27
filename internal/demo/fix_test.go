package demo

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/harness"
	"github.com/openagentix/factory/internal/staging"
)

func failingScript() harness.StubScript {
	return harness.StubScript{ExitCode: 3, Files: []harness.StubFile{{Path: "booking/x.sh", Content: "echo x\n"}}}
}

// F5: a beat that works says what it expects. With every script failing, the
// story must stop with an error, not report a good end.
func TestStoryStopsWhenTheRealWorkFails(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	if _, _, err := d.Step(ctx); err != nil { // write_checks, real
		t.Fatal(err)
	}
	d.jobHook = func(j *job) { j.script = failingScript() }
	err := d.Play(ctx)
	var div *DivergedError
	if !errors.As(err, &div) {
		t.Fatalf("Play = %v, want a DivergedError", err)
	}
	if div.Want != OutcomeAdmitted || div.Got != outcomeFailedPrefix+"bad_result" {
		t.Errorf("want %q got %q", div.Want, div.Got)
	}
	// The snapshot is the honest one: the piece is stuck.
	if p := piece(snap(t, d), "t3"); p == nil || p.State != floor.StateStuck {
		t.Errorf("piece t3: %+v", p)
	}
}

func TestStoryStopsWhenAFailingBeatSucceeds(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	names := d.Beats()
	for i := 0; names[i] != "checks_fail"; i++ {
		if _, _, err := d.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	d.jobHook = func(j *job) { j.script = tickets()[1].scripts[1] } // the fixed script
	_, _, err := d.Step(ctx)
	var div *DivergedError
	if !errors.As(err, &div) || div.Want != checkFailedOutcome(gate.OutcomeRework, "syntax") || div.Got != OutcomeAdmitted {
		t.Fatalf("Step = %v", err)
	}
}

// F10 and F11b: a fact is written only after the call it describes succeeded,
// and a state that was not pushed is not the holding area.
func TestNoFactBeforeTheHoldingAreaTookTheChange(t *testing.T) {
	d, failPush := newDemoFailingPush(t)
	ctx := context.Background()
	failPush.Store(true)
	if _, _, err := d.Step(ctx); err == nil { // write_checks
		t.Fatal("the push failed and the beat did not")
	}
	lf, _ := d.led.Facts(ctx)
	gf := lf.Goals[goalMove]
	if gf.AcceptanceSource != "" || gf.Baseline != nil || gf.ManifestEntry != "" {
		t.Errorf("the checks were recorded though the holding area did not take them: %+v", gf)
	}
	if len(lf.Applied) != 0 {
		t.Errorf("applied: %v", lf.Applied)
	}
	for _, e := range lf.Journal {
		if e.Kind == "checks_written" || e.Kind == "entered_holding_area" {
			t.Errorf("journal says %s", e.Kind)
		}
	}
}

// A ticket whose push of the staging state fails leaves no gate fact and no
// entry in the ledger.
func TestNoGateFactWhenTheHoldingAreaRefuses(t *testing.T) {
	d, failPush := newDemoFailingPush(t)
	ctx := context.Background()
	if _, _, err := d.Step(ctx); err != nil { // write_checks
		t.Fatal(err)
	}
	failPush.Store(true)
	if _, _, err := d.Step(ctx); err == nil {
		t.Fatal("build_move: the push failed and the beat did not")
	}
	lf, _ := d.led.Facts(ctx)
	if len(lf.Gate) != 0 {
		t.Errorf("gate facts without an entry: %+v", lf.Gate)
	}
	for _, e := range lf.Journal {
		if e.Kind == "entered_holding_area" {
			t.Error("entered_holding_area with no published state")
		}
	}
	for id := range lf.Applied {
		if id != "run001" { // the checks, which were taken
			t.Errorf("applied stamp for %s", id)
		}
	}
}

// observe counts only a published state.
func TestObserveIgnoresAStateThatIsNotPublished(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	d.mu.Lock()
	d.observe(stagingState(false), job{})
	d.mu.Unlock()
	lf, _ := d.led.Facts(ctx)
	if len(lf.Applied) != 0 || len(lf.Journal) != 1 { // the seeded approval only
		t.Errorf("an unpublished state was counted: applied %v journal %v", lf.Applied, lf.Journal)
	}
	d.mu.Lock()
	d.observe(stagingState(true), job{})
	d.mu.Unlock()
	lf, _ = d.led.Facts(ctx)
	if len(lf.Applied) != 1 {
		t.Errorf("a published state was not counted: %v", lf.Applied)
	}
}

// F11a: a stub run that hits its deadline ends with the deadline code.
func TestRunThatHitsItsDeadlineIsLabelledDeadline(t *testing.T) {
	cfg := testConfig(t)
	cfg.runDeadline = time.Second
	d, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	j, _ := d.ticketJob("ask", 0)
	j.script = harness.StubScript{DelayMS: 60000, Result: okResult("late")}
	res, err := d.work(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	if res.ok || res.endCode != floor.Deadline {
		t.Fatalf("ok=%v endCode=%q status=%v", res.ok, res.endCode, res.outcome.Status)
	}
	if p := piece(snap(t, d), "t3"); p != nil && p.Reason != nil && p.Reason.Code == floor.BadResult {
		t.Error("the piece says bad_result")
	}
}

func stagingState(published bool) staging.State {
	return staging.State{Seq: 9, Published: published, Applied: []staging.AppliedEntry{{EntryID: "runX", Ticket: 3, Goal: 1}}}
}

// failingHost is the host as staging sees it, with a push that can be made to
// fail. It lives in a test file: the product code never names the push.
type failingHost struct {
	githost.Proposer
	fail *atomic.Bool
}

func (h failingHost) PushStaging(ctx context.Context, dir, commit string) error {
	if h.fail.Load() {
		return errors.New("the push of the staging state failed")
	}
	return h.Proposer.PushStaging(ctx, dir, commit)
}

func newDemoFailingPush(t *testing.T) (*Demo, *atomic.Bool) {
	t.Helper()
	fail := &atomic.Bool{}
	cfg := testConfig(t)
	cfg.wrapHost = func(p githost.Proposer) githost.Proposer { return failingHost{Proposer: p, fail: fail} }
	d, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, fail
}

// stepTo plays the beats before the named one and returns the demo.
func stepTo(t *testing.T, beat string) *Demo {
	t.Helper()
	d := newDemo(t)
	ctx := context.Background()
	for _, n := range d.Beats() {
		if n == beat {
			return d
		}
		if _, _, err := d.Step(ctx); err != nil {
			t.Fatalf("beat %s: %v", n, err)
		}
	}
	t.Fatalf("no beat %s", beat)
	return nil
}

// stepToAfter plays only the beats this one needs before it, then makes the
// story ready to run `beat` next. The whole story is played by the story tests.
func stepToAfter(t *testing.T, beat string, prereqs ...string) *Demo {
	t.Helper()
	d := newDemo(t)
	playBeats(t, d, prereqs...)
	for i, b := range d.beats {
		if b.name == beat {
			d.next = i
			return d
		}
	}
	t.Fatalf("no beat %s", beat)
	return nil
}

func diverged(t *testing.T, d *Demo, want, got Outcome) {
	t.Helper()
	_, _, err := d.Step(context.Background())
	var div *DivergedError
	if !errors.As(err, &div) {
		t.Fatalf("Step = %v, want a DivergedError", err)
	}
	if div.Want != want || div.Got != got {
		t.Errorf("want %q, got %q; expected want %q got %q", div.Want, div.Got, want, got)
	}
}

// A: each beat expects the exact result it was written for. The real code is
// made to give a different reason of the same kind, and the story stops.

func TestTakenOutBeatExpectsTheExactRemovalReason(t *testing.T) {
	d := stepToAfter(t, "taken_out", "write_checks", "build_move")
	d.jobHook = func(j *job) {
		// The move step changes under it, so the replay conflicts instead of
		// the acceptance line regressing.
		j.script = harness.StubScript{
			Files:  []harness.StubFile{{Path: "booking/move.sh", Content: "echo moved another way\n"}},
			Result: okResult("Changed the move step.", claimOf("Changed the move step.", "booking/move.sh")),
		}
	}
	diverged(t, d, "taken_out:acceptance_regressed:1", "taken_out:conflict")
}

func TestChecksFailBeatExpectsTheExactCheckAndGateReason(t *testing.T) {
	d := stepTo(t, "checks_fail")
	d.jobHook = func(j *job) {
		// A good confirm step, but the rules file is gone: another check fails.
		j.script = harness.StubScript{
			Files:  []harness.StubFile{{Path: "booking/confirm.sh", Content: "echo confirmed\n"}},
			Delete: []string{"booking/rules.txt"},
			Result: okResult("Done.", claimOf("Done.", "booking/confirm.sh")),
		}
	}
	diverged(t, d, "check_failed:rework:syntax", "check_failed:rework:rules")
}

func TestRiskBeatExpectsTheExactExceptionAndRiskReason(t *testing.T) {
	d := stepToAfter(t, "risk", "write_checks")
	// The same path is now a contract path. The real code gives another reason.
	d.factory.Spec.RiskPaths.Contract = []string{"auth/**"}
	d.factory.Spec.RiskPaths.Security = nil
	diverged(t, d, "consequence:high_risk:security", "consequence:high_risk:contract")
}

func TestQuestionBeatExpectsExactlyOneQuestion(t *testing.T) {
	d := stepToAfter(t, "question", "write_checks")
	d.jobHook = func(j *job) {
		j.script.Result.Questions = []string{"First?", "Second?"}
	}
	diverged(t, d, "question:1", "question:2")
}
