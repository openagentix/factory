package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/harness"
	"github.com/openagentix/factory/internal/server"
	"github.com/openagentix/factory/internal/staging"
)

// memSteps is the step record of one Idempotency-Key, as the server keeps it.
type memSteps struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSteps) Done(slot string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.m[slot]
	return id, ok
}
func (s *memSteps) Mark(slot, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[slot] = id
}

type callView struct {
	ID      string             `json:"id"`
	Kind    floor.CallKind     `json:"kind"`
	Actions []floor.ActionName `json:"actions"`
	Version string             `json:"version"`
}

// callOf reads a call, with its version and actions, from the snapshot.
func callOf(t testing.TB, d *Demo, prefix string) (callView, bool) {
	t.Helper()
	b, err := d.Floor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Calls []callView `json:"calls"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c.ID, prefix) {
			return c, true
		}
	}
	return callView{}, false
}

func mustCall(t testing.TB, d *Demo, prefix string) callView {
	t.Helper()
	c, ok := callOf(t, d, prefix)
	if !ok {
		t.Fatalf("no call %s* in the snapshot", prefix)
	}
	return c
}

func stepsTo(t *testing.T, d *Demo, last string) {
	t.Helper()
	for {
		name, _, err := d.Step(context.Background())
		if err != nil {
			t.Fatalf("beat %s: %v", name, err)
		}
		if name == last {
			return
		}
	}
}

func journalKinds(t testing.TB, d *Demo) []string {
	t.Helper()
	var out []string
	for _, e := range snap(t, d).Log {
		out = append(out, string(e.Kind)+":"+strconv.Itoa(e.Ticket)+":"+strconv.Itoa(e.Goal))
	}
	return out
}

func countKind(t testing.TB, d *Demo, kind string) int {
	n := 0
	for _, e := range snap(t, d).Log {
		if string(e.Kind) == kind {
			n++
		}
	}
	return n
}

func comments(t testing.TB, d *Demo, ticket int) []githost.Comment {
	t.Helper()
	cs, err := d.host.ListComments(context.Background(), ticket, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func countKindOf(cs []githost.Comment, k githost.CommentKind) int {
	n := 0
	for _, c := range cs {
		if c.Kind == k {
			n++
		}
	}
	return n
}

func hasLabel(t testing.TB, d *Demo, n int, l string) bool {
	iss, err := d.host.GetIssue(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	return iss.HasLabel(l)
}

func answerReq(c callView, text string) server.AnswerRequest {
	return server.AnswerRequest{Call: c.ID, Version: c.Version, Text: text, Steps: &memSteps{}}
}

// Answer: the question leaves "Needs a person", the ticket really starts
// again through the real runner, and the shift log keeps its fixed lines.
func TestAnswerStartsTheTicketAgainThroughTheRealRunner(t *testing.T) {
	d := stepTo(t, "unconfirmed") // the question beat is done
	ctx := context.Background()
	c := mustCall(t, d, "q-5-")
	if len(c.Actions) != 1 || c.Actions[0] != floor.ActionAnswer {
		t.Fatalf("actions = %v", c.Actions)
	}
	if !hasLabel(t, d, 5, githost.LabelNeedsHuman) {
		t.Fatal("the question run did not put the needs-human label on the ticket")
	}
	if n := countKind(t, d, "question_asked"); n != 1 {
		t.Fatalf("question_asked entries = %d", n)
	}
	res, err := d.Answer(ctx, answerReq(c, "Yes, a full day may take a move."))
	if err != nil || res.Result != server.ResultDone || res.JournalSeq <= 0 {
		t.Fatalf("Answer = %+v, %v", res, err)
	}
	f := snap(t, d)
	if _, still := callOf(t, d, "q-5-"); still {
		t.Fatal("the question is still a call")
	}
	if p := piece(f, "t5"); p == nil || p.State != floor.StateWaitingMachine || code(p) != floor.Queued {
		t.Fatalf("t5 right after the answer: %+v", p)
	}
	if hasLabel(t, d, 5, githost.LabelNeedsHuman) {
		t.Fatal("the label is still on")
	}
	cs := comments(t, d, 5)
	if countKindOf(cs, githost.KindAnswer) != 1 {
		t.Fatalf("answer comments = %d", countKindOf(cs, githost.KindAnswer))
	}
	if n := countKind(t, d, "answered"); n != 1 {
		t.Fatalf("answered entries = %d", n)
	}
	// Nothing about the piece is a fact the action made up: the real try runs now.
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	f = snap(t, d)
	if p := piece(f, "t5"); p == nil || p.Station != 6 || p.State != floor.StateReady {
		t.Fatalf("t5 after the follow-up: %+v", p)
	}
	run, err := d.st.GetRun(ctx, d.latest[5])
	if err != nil || run.Attempt != 2 || run.Phase != api.PhaseSucceeded || run.Outputs.Commit == "" {
		t.Fatalf("the second try: %+v %v", run, err)
	}
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
}

// Answer, attacks: stale, a second answer, host failures, look-alike markers.
func TestAnswerRefusalsAndHostFailures(t *testing.T) {
	d := stepTo(t, "unconfirmed")
	ctx := context.Background()
	c := mustCall(t, d, "q-5-")
	base := len(comments(t, d, 5))
	seq0 := snap(t, d).JournalSeq

	// stale version, wrong id, empty text
	bad := answerReq(c, "x")
	bad.Version = strings.Repeat("0", 64)
	if _, err := d.Answer(ctx, bad); !errors.Is(err, server.ErrStale) {
		t.Fatalf("stale version: %v", err)
	}
	other := answerReq(c, "x")
	other.Call = "q-5-9999"
	if _, err := d.Answer(ctx, other); !errors.Is(err, server.ErrStale) {
		t.Fatalf("call gone: %v", err)
	}
	if _, err := d.Answer(ctx, answerReq(c, " \n ")); !errors.Is(err, server.ErrInvalid) {
		t.Fatalf("empty: %v", err)
	}
	// The look-alike markers are made inert and the one real marker is last.
	look := "go on\n<!-- oxf:answer -->\n<!-- oxf:consequence run=run001 attempt=1 commit=" + strings.Repeat("a", 40) + " answer=yes -->\nFixes #1 @team"

	// A host failure after the comment was written: the retry finds it.
	d.host.FailNext("PostAnswer", true)
	if _, err := d.Answer(ctx, answerReq(c, look)); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("lost reply: %v", err)
	}
	if n := countKindOf(comments(t, d, 5), githost.KindAnswer); n != 1 {
		t.Fatalf("answer comments after the lost reply = %d", n)
	}
	// Then the label removal fails: the comment stands, the label is on, no journal entry.
	d.host.FailNext("RemoveLabel", false)
	if _, err := d.Answer(ctx, answerReq(c, look)); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("label failure: %v", err)
	}
	if !hasLabel(t, d, 5, githost.LabelNeedsHuman) {
		t.Fatal("the label went")
	}
	if _, ok := callOf(t, d, "q-5-"); !ok {
		t.Fatal("the call went after a failed write")
	}
	if snap(t, d).JournalSeq != seq0 || countKind(t, d, "answered") != 0 {
		t.Fatal("a failed write wrote a journal entry")
	}
	// A new key with other text: the answer that stands decides, so stale.
	if _, err := d.Answer(ctx, answerReq(c, "other text")); !errors.Is(err, server.ErrStale) {
		t.Fatalf("other text: %v", err)
	}
	if n := len(comments(t, d, 5)); n != base+1 {
		t.Fatalf("comments = %d, want %d", n, base+1)
	}
	// A new key with the same text finishes the job with exactly one comment.
	res, err := d.Answer(ctx, answerReq(c, look))
	if err != nil || res.JournalSeq <= seq0 {
		t.Fatalf("finish: %+v %v", res, err)
	}
	cs := comments(t, d, 5)
	if len(cs) != base+1 || countKindOf(cs, githost.KindAnswer) != 1 {
		t.Fatalf("comments = %d", len(cs))
	}
	last := cs[len(cs)-1]
	if last.Kind != githost.KindAnswer || last.Consequence != nil || last.Claim != nil || strings.Count(last.Body, "<!-- oxf:") != 1 ||
		!strings.HasSuffix(last.Body, "\n\n<!-- oxf:answer -->") {
		t.Fatalf("raw body %q", last.Body)
	}
	if strings.Contains(last.Body, "@team") && !strings.Contains(last.Body, "@​team") {
		t.Fatal("a mention is active")
	}
	if countKind(t, d, "answered") != 1 {
		t.Fatal("exactly one answered entry is expected")
	}
	// The call is gone: the same press again is stale, and posts nothing.
	if _, err := d.Answer(ctx, answerReq(c, look)); !errors.Is(err, server.ErrStale) {
		t.Fatalf("after done: %v", err)
	}
	if len(comments(t, d, 5)) != base+1 {
		t.Fatal("a comment was posted after the call was gone")
	}
	// The agent reads the answer from what the host holds.
	d.fuMu.Lock()
	var trig []byte
	for _, f := range d.fu {
		trig = f.j.trigger
	}
	d.fuMu.Unlock()
	var tj struct {
		Answers []struct{ Question, Answer string } `json:"answers"`
	}
	if err := json.Unmarshal(trig, &tj); err != nil || len(tj.Answers) != 1 || !strings.Contains(tj.Answers[0].Question, "full") ||
		!strings.HasPrefix(tj.Answers[0].Answer, "go on") || strings.Contains(tj.Answers[0].Answer, "<!--") {
		t.Fatalf("trigger = %s (%v)", trig, err)
	}
}

// Several answers at once, with different texts: one comment.
func TestTwoAnswersAtOnceMakeOneComment(t *testing.T) {
	d := stepTo(t, "unconfirmed")
	c := mustCall(t, d, "q-5-")
	base := len(comments(t, d, 5))
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = d.Answer(context.Background(), answerReq(c, fmt.Sprintf("answer %d", i)))
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, server.ErrStale) {
			t.Fatalf("unexpected error %v", err)
		}
	}
	if ok != 1 || len(comments(t, d, 5)) != base+1 || countKind(t, d, "answered") != 1 {
		t.Fatalf("ok=%d comments=%d", ok, len(comments(t, d, 5))-base)
	}
}

// A real step that fails after the action was accepted shows as failure.
func TestAnswerWithAScriptThatFailsIsStuckNeverPassed(t *testing.T) {
	d := stepTo(t, "unconfirmed")
	ctx := context.Background()
	c := mustCall(t, d, "q-5-")
	if _, err := d.Answer(ctx, answerReq(c, "go")); err != nil {
		t.Fatal(err)
	}
	d.fuMu.Lock()
	d.fu[0].j.script.Files = []harness.StubFile{{Path: "booking/full-day.sh", Content: "if then fi (\n"}}
	d.fuMu.Unlock()
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	p := piece(snap(t, d), "t5")
	if p == nil || p.State != floor.StateStuck || p.Station == 6 {
		t.Fatalf("t5 = %+v", p)
	}
}

func riskCall(t *testing.T, d *Demo) (callView, int) {
	t.Helper()
	c := mustCall(t, d, "c-6-")
	return c, 6
}

func consReq(c callView, yes bool) server.ConsequenceRequest {
	return server.ConsequenceRequest{Call: c.ID, Version: c.Version, Yes: yes, Steps: &memSteps{}}
}

// Yes: the real gate decision is applied and the change really enters the
// holding area. One comment, one journal entry.
func TestYesAppliesTheSealedDecisionAndTheChangeEntersTheHoldingArea(t *testing.T) {
	d := stepTo(t, "taken_out") // the risk beat is done
	ctx := context.Background()
	c, tk := riskCall(t, d)
	base := len(comments(t, d, tk))
	res, err := d.Consequence(ctx, consReq(c, true))
	if err != nil || res.JournalSeq <= 0 {
		t.Fatalf("yes: %+v %v", res, err)
	}
	cs := comments(t, d, tk)
	if len(cs) != base+1 || countKindOf(cs, githost.KindConsequence) != 1 {
		t.Fatalf("comments = %d", len(cs)-base)
	}
	if p := piece(snap(t, d), "t6"); p == nil || p.Station != 5 || code(p) != floor.WaitingCombine {
		t.Fatalf("t6 right after the yes: %+v", p)
	}
	if _, still := callOf(t, d, "c-6-"); still {
		t.Fatal("the call stays after the answer")
	}
	if countKind(t, d, "consequence_answered") != 1 {
		t.Fatal("expected one consequence_answered entry")
	}
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	f := snap(t, d)
	if p := piece(f, "t6"); p == nil || p.Station != 6 || p.State != floor.StateReady {
		t.Fatalf("t6 after the follow-up: %+v", p)
	}
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	// The decision is applied once: the same press again is stale.
	if _, err := d.Consequence(ctx, consReq(c, true)); !errors.Is(err, server.ErrStale) {
		t.Fatalf("second yes: %v", err)
	}
}

// No: the ticket is really reworked. The second try is not risky and passes.
func TestNoSendsTheTicketBackAndTheSecondTryPasses(t *testing.T) {
	d := stepTo(t, "taken_out")
	ctx := context.Background()
	c, _ := riskCall(t, d)
	if _, err := d.Consequence(ctx, consReq(c, false)); err != nil {
		t.Fatal(err)
	}
	if p := piece(snap(t, d), "t6"); p == nil || p.State != floor.StateStuck || code(p) != floor.PersonSaidNo {
		t.Fatalf("t6 after the no: %+v", p)
	}
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	f := snap(t, d)
	if p := piece(f, "t6"); p == nil || p.Station != 6 || p.State != floor.StateReady {
		t.Fatalf("t6 after the rework: %+v", p)
	}
	run, err := d.st.GetRun(ctx, d.latest[6])
	if err != nil || run.Attempt != 2 || run.Phase != api.PhaseSucceeded {
		t.Fatalf("second try: %+v %v", run, err)
	}
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
}

// A no after a yes, with a new key: refused, one comment, the first applies.
func TestYesThenNoWithAnotherKeyIsRefused(t *testing.T) {
	d := stepTo(t, "taken_out")
	ctx := context.Background()
	c, tk := riskCall(t, d)
	base := len(comments(t, d, tk))
	// The yes posts its comment, then the apply step fails to be seen.
	d.host.FailNext("PostConsequenceAnswer", true)
	if _, err := d.Consequence(ctx, consReq(c, true)); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("lost reply: %v", err)
	}
	if _, err := d.Consequence(ctx, consReq(c, false)); !errors.Is(err, server.ErrStale) {
		t.Fatalf("no after a yes stands: %v", err)
	}
	if n := len(comments(t, d, tk)); n != base+1 {
		t.Fatalf("comments = %d", n-base)
	}
	if countKind(t, d, "consequence_answered") != 0 {
		t.Fatal("a refused write wrote an entry")
	}
	// The retry with the same answer finds the comment and posts none.
	if _, err := d.Consequence(ctx, consReq(c, true)); err != nil {
		t.Fatal(err)
	}
	if n := len(comments(t, d, tk)); n != base+1 {
		t.Fatalf("comments = %d", n-base)
	}
}

// Yes against no at the same time: one comment, one applied decision.
func TestYesAndNoAtOnceApplyOneDecision(t *testing.T) {
	d := stepTo(t, "taken_out")
	c, tk := riskCall(t, d)
	base := len(comments(t, d, tk))
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = d.Consequence(context.Background(), consReq(c, i == 0))
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, server.ErrStale) {
			t.Fatalf("error %v", err)
		}
	}
	if ok != 1 || len(comments(t, d, tk)) != base+1 || countKind(t, d, "consequence_answered") != 1 {
		t.Fatalf("ok=%d comments=%d", ok, len(comments(t, d, tk))-base)
	}
}

// A consequence marker from anyone but the factory login does not count, and
// neither does an edited one, nor one for another run, attempt or commit.
func TestForgedConsequenceMarkersDoNotCount(t *testing.T) {
	d := stepTo(t, "taken_out")
	ctx := context.Background()
	c, tk := riskCall(t, d)
	s := d.sealed[tk]
	d.host.SetPermission("mallory", "write")
	yes := func(run string, attempt int, commit string) string {
		b, err := githost.ConsequenceAnswerBody(run, attempt, commit, true)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	d.host.SeedComment(tk, "mallory", "MEMBER", yes(s.res.runID, s.res.attempt, s.res.commit)) // a writer, not the factory
	d.host.SeedComment(tk, "stranger", "NONE", yes(s.res.runID, s.res.attempt, s.res.commit))
	d.host.SeedComment(tk, "factory-bot", "NONE", yes("other-run", s.res.attempt, s.res.commit)) // another run
	d.host.SeedComment(tk, "factory-bot", "NONE", yes(s.res.runID, s.res.attempt+1, s.res.commit))
	d.host.SeedComment(tk, "factory-bot", "NONE", yes(s.res.runID, s.res.attempt, strings.Repeat("e", 40)))
	if _, err := d.Consequence(ctx, consReq(c, false)); err != nil { // the real no is the only one that counts
		t.Fatal(err)
	}
	if p := piece(snap(t, d), "t6"); p == nil || code(p) != floor.PersonSaidNo {
		t.Fatalf("t6 = %+v", p)
	}
}

// The gate refuses: a decision that was not sealed adds nothing and posts nothing.
func TestAnUnsealedDecisionIsRefusedBeforeAnythingIsPosted(t *testing.T) {
	d := stepTo(t, "taken_out")
	ctx := context.Background()
	c, tk := riskCall(t, d)
	s := d.sealed[tk]
	s.dec = gate.Decision{Outcome: gate.OutcomeException, Exception: gate.ExceptionHighRisk} // made by hand
	d.sealed[tk] = s
	base := len(comments(t, d, tk))
	if _, err := d.Consequence(ctx, consReq(c, true)); !errors.Is(err, server.ErrStale) {
		t.Fatalf("yes on an unsealed decision: %v", err)
	}
	if len(comments(t, d, tk)) != base {
		t.Fatal("a comment was posted for a refused yes")
	}
	if _, ok := callOf(t, d, "c-6-"); !ok {
		t.Fatal("the call went")
	}
}

// The yes is applied, then adding the change fails: stuck, said out loud.
func TestAFailedAddAfterAYesIsShownAsStuckAndReported(t *testing.T) {
	var lines []string
	var mu sync.Mutex
	cfg := testConfig(t)
	cfg.Report = func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() }
	d, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	stepsTo(t, d, "risk")
	d.addHook = func() error { return staging.ErrMirrorBehind }
	c, _ := riskCall(t, d)
	if _, err := d.Consequence(context.Background(), consReq(c, true)); err != nil {
		t.Fatal(err)
	}
	if err := d.Drain(context.Background()); !errors.Is(err, staging.ErrMirrorBehind) {
		t.Fatalf("Drain = %v", err)
	}
	f := snap(t, d)
	if p := piece(f, "t6"); p == nil || p.State != floor.StateStuck || p.Station == 6 {
		t.Fatalf("t6 = %+v", p)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "DEMO ") || strings.Contains(lines[0], "mirror") {
		t.Fatalf("lines = %q", lines)
	}
}

// Approve: the label is added, and the second goal really gets its checks and
// its tickets built.
func TestApproveBuildsTheSecondGoal(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	c := mustCall(t, d, "p-2")
	if len(c.Actions) != 1 || c.Actions[0] != floor.ActionApprove {
		t.Fatalf("plan call actions = %v", c.Actions)
	}
	res, err := d.Approve(ctx, server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}})
	if err != nil || res.JournalSeq <= 0 {
		t.Fatalf("Approve: %+v %v", res, err)
	}
	if !hasLabel(t, d, 2, githost.LabelApproved) {
		t.Fatal("the approval label is not on the goal")
	}
	if _, still := callOf(t, d, "p-2"); still {
		t.Fatal("the plan call stays")
	}
	if countKind(t, d, "plan_approved") != 2 { // goal 1 was seeded
		t.Fatal("expected a plan_approved entry for goal 2")
	}
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	f := snap(t, d)
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	ready := 0
	for _, p := range f.Pieces {
		if p.Goal == 2 && p.Station == 6 && p.State == floor.StateReady {
			ready++
		}
	}
	if ready != 2 {
		t.Fatalf("pieces of goal 2 in the holding area = %d (%+v)", ready, f.Pieces)
	}
	var g2 *floor.Goal
	for i := range f.Goals {
		if f.Goals[i].Goal == 2 {
			g2 = &f.Goals[i]
		}
	}
	if g2 == nil || len(g2.Lines) != 2 {
		t.Fatalf("goal 2 = %+v", g2)
	}
	for _, l := range g2.Lines {
		if l.State != "working" {
			t.Errorf("line %d is %s", l.ID, l.State)
		}
	}
	if countKind(t, d, "checks_written") != 1 {
		t.Fatal("the checks of goal 2 were not written")
	}
}

// Approve, attacks: the body was edited, the label is read back, host failure.
func TestApproveRefusals(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	c := mustCall(t, d, "p-2")
	req := server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}}
	d.host.EditIssueBody(2, "Customers can cancel a booking.\n\n- A booking can be cancelled.\n- A cancelled booking is refunded.\n- It frees the day.\n- It is quick.\n")
	if _, err := d.Approve(ctx, req); !errors.Is(err, server.ErrStale) {
		t.Fatalf("edited body: %v", err)
	}
	if hasLabel(t, d, 2, githost.LabelApproved) {
		t.Fatal("the label was added for a stale version")
	}
	// With four lines the call offers no Approve at all.
	c4 := mustCall(t, d, "p-2")
	if len(c4.Actions) != 0 {
		t.Fatalf("four lines: actions = %v", c4.Actions)
	}
	if _, err := d.Approve(ctx, server.ApproveRequest{Call: c4.ID, Version: c4.Version, Steps: &memSteps{}}); !errors.Is(err, server.ErrStale) {
		t.Fatalf("approve with 4 lines: %v", err)
	}
	if countKind(t, d, "plan_approved") != 1 {
		t.Fatal("a refused write wrote an entry")
	}
	// Back to two lines: a host failure leaves the plan waiting.
	d.host.EditIssueBody(2, goalCancelBody)
	c = mustCall(t, d, "p-2")
	d.host.FailNext("AddLabel", false)
	_, err := d.Approve(ctx, server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}})
	if !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("host failure: %v", err)
	}
	if _, ok := callOf(t, d, "p-2"); !ok {
		t.Fatal("the plan call went after a failed write")
	}
	if countKind(t, d, "plan_approved") != 1 {
		t.Fatal("a failed write wrote an entry")
	}
	if _, err := d.Approve(ctx, server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	// The tickets were made once.
	tk := 0
	for n := 1; n < 30; n++ {
		if d.host.Parent(n) == 2 {
			tk++
		}
	}
	if tk != 2 {
		t.Fatalf("tickets of goal 2 = %d", tk)
	}
}

// All three actions arrive while the story plays and the follow-up loop runs.
// Every snapshot keeps its contract, and each action is done exactly once.
// Run with -race.
func TestActionsAndTheStoryTogether(t *testing.T) {
	cfg := testConfig(t)
	risk := make(chan struct{})
	var once sync.Once
	cfg.OnBeat = func(name string) {
		if name == "risk" {
			once.Do(func() { close(risk) })
		}
	}
	d, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stepsTo(t, d, "question")
	q := mustCall(t, d, "q-5-")
	p := mustCall(t, d, "p-2")

	// readers: the contract holds after every change
	var stop atomic.Bool
	var readers sync.WaitGroup
	bad := make(chan error, 4)
	for i := 0; i < 3; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for !stop.Load() {
				b, err := d.Floor(context.Background())
				if err == nil {
					var f tfloor
					if err = json.Unmarshal(b, &f); err == nil {
						err = invariants(f)
					}
				}
				if err != nil {
					bad <- err
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	var work, acts sync.WaitGroup
	work.Add(1)
	go func() { defer work.Done(); _ = d.Work(ctx) }()
	var playErr, ansErr, appErr, conErr error
	acts.Add(4)
	go func() { defer acts.Done(); playErr = d.Play(context.Background()) }()
	go func() { defer acts.Done(); _, ansErr = d.Answer(context.Background(), answerReq(q, "together")) }()
	go func() {
		defer acts.Done()
		_, appErr = d.Approve(context.Background(), server.ApproveRequest{Call: p.ID, Version: p.Version, Steps: &memSteps{}})
	}()
	go func() {
		defer acts.Done()
		<-risk
		// A fresh read: Floor may give the last whole snapshot while a write runs.
		d.mu.RLock()
		f := d.project(context.Background())
		d.mu.RUnlock()
		var c callView
		for _, fc := range f.Calls {
			if strings.HasPrefix(fc.ID, "c-6-") {
				c = callView{ID: fc.ID, Version: fc.Version}
			}
		}
		if c.ID == "" {
			t.Errorf("no consequence call after the risk beat")
			return
		}
		_, conErr = d.Consequence(context.Background(), consReq(c, true))
	}()
	acts.Wait()
	if playErr != nil || ansErr != nil || appErr != nil || conErr != nil {
		t.Fatalf("play %v, answer %v, approve %v, consequence %v", playErr, ansErr, appErr, conErr)
	}
	if err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel()
	work.Wait()
	stop.Store(true)
	readers.Wait()
	close(bad)
	for err := range bad {
		t.Errorf("a snapshot broke the contract: %v", err)
	}
	f := snap(t, d)
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	if countKindOf(comments(t, d, 5), githost.KindAnswer) != 1 || countKindOf(comments(t, d, 6), githost.KindConsequence) != 1 {
		t.Fatal("each action posts exactly one comment")
	}
	for kind, want := range map[string]int{"answered": 1, "consequence_answered": 1, "plan_approved": 2} {
		if got := countKind(t, d, kind); got != want {
			t.Errorf("%s entries = %d, want %d", kind, got, want)
		}
	}
	for _, id := range []string{"t5", "t6"} {
		if pc := piece(f, id); pc == nil || pc.Station != 6 {
			t.Errorf("%s = %+v", id, pc)
		}
	}
}

// F3: when the label goes and no answer stands, the label comes back with one
// fixed line. That line is never a question.
func TestNoAnswerFoundIsOneFixedLineAndNeverAQuestion(t *testing.T) {
	d := stepTo(t, "unconfirmed")
	ctx := context.Background()
	q := mustCall(t, d, "q-5-")
	noop := func(floor.JournalKind, int, int) int64 { return 0 }
	cycle := func() error {
		return d.mutate(ctx, func() error { return d.resume(ctx, 5, noop) })
	}
	fixed := func() []githost.Comment {
		var out []githost.Comment
		for _, c := range comments(t, d, 5) {
			if c.Kind == githost.KindFactory && strings.HasSuffix(c.Body, ".no_answer -->") {
				out = append(out, c)
			}
		}
		return out
	}
	for round := 1; round <= 2; round++ {
		if err := d.host.RemoveLabel(ctx, 5, githost.LabelNeedsHuman); err != nil {
			t.Fatal(err)
		}
		if err := cycle(); !errors.Is(err, server.ErrStale) {
			t.Fatalf("round %d: %v", round, err)
		}
		if !hasLabel(t, d, 5, githost.LabelNeedsHuman) {
			t.Fatal("the label did not come back")
		}
		if n := len(fixed()); n != round {
			t.Fatalf("round %d: %d fixed comments", round, n)
		}
		// A cycle with the label on does nothing more.
		if err := cycle(); err != nil {
			t.Fatal(err)
		}
		if n := len(fixed()); n != round {
			t.Fatalf("round %d: the line was posted again: %d", round, n)
		}
		again := mustCall(t, d, "q-5-")
		if again.ID != q.ID {
			t.Fatalf("a new call appeared: %s", again.ID)
		}
		for _, c := range snap(t, d).Calls {
			if strings.HasPrefix(c.ID, "q-5-") && c.ID != q.ID {
				t.Fatalf("a second question: %s", c.ID)
			}
		}
	}
	runID := d.latest[5]
	if c := fixed()[0]; c.RunID != runID+".no_answer" {
		t.Fatalf("marker id = %q", c.RunID)
	}
	// A failure between the comment and the label does not post the line twice.
	if err := d.host.RemoveLabel(ctx, 5, githost.LabelNeedsHuman); err != nil {
		t.Fatal(err)
	}
	d.host.FailNext("AddLabel", false)
	if err := cycle(); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("label failure: %v", err)
	}
	if err := cycle(); !errors.Is(err, server.ErrStale) {
		t.Fatalf("retry: %v", err)
	}
	if n := len(fixed()); n != 3 {
		t.Fatalf("fixed comments = %d, want 3", n)
	}
}

// F1: an Approve while a story step runs. The story reads the ticket numbers
// that Approve writes. Run with -race.
func TestApproveWhileAStoryStepRunsIsRaceFree(t *testing.T) {
	d := stepTo(t, "checks_fail")
	ctx := context.Background()
	c := mustCall(t, d, "p-2")
	var wg sync.WaitGroup
	var stepErr, actErr error
	start := make(chan struct{})
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, _, stepErr = d.Step(ctx) }()
	go func() {
		defer wg.Done()
		<-start
		_, actErr = d.Approve(ctx, server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}})
	}()
	close(start)
	wg.Wait()
	if stepErr != nil || actErr != nil {
		t.Fatalf("step %v, approve %v", stepErr, actErr)
	}
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := invariants(snap(t, d)); err != nil {
		t.Fatal(err)
	}
}
