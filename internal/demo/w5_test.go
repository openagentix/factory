package demo

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"errors"

	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/server"
)

// holdWrite takes the write lock, as a follow-up does while it works on the
// holding area, and gives it back at the end of the test.
func holdWrite(t *testing.T, d *Demo) {
	t.Helper()
	d.mu.Lock()
	t.Cleanup(d.mu.Unlock)
}

// Leftover 4 of the W4 review: a snapshot taken after an action replied "done"
// never shows a state older than that action, also while a slow follow-up
// holds the write lock.
func TestFloorNeverShowsAStateOlderThanADoneAnswer(t *testing.T) {
	d := beatsOnly(t, "write_checks", "question")
	c := mustCall(t, d, "q-5-") // a snapshot is read, so a cache holds the question
	if _, err := d.Answer(context.Background(), answerReq(c, "Yes, a full day may take a move.")); err != nil {
		t.Fatal(err)
	}
	holdWrite(t, d)
	if _, still := callOf(t, d, "q-5-"); still {
		t.Fatal("the question is still offered after the answer was done")
	}
}

func TestFloorNeverShowsAStateOlderThanADoneApprove(t *testing.T) {
	d := newDemo(t)
	c := mustCall(t, d, "p-2")
	if _, err := d.Approve(context.Background(), server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}}); err != nil {
		t.Fatal(err)
	}
	holdWrite(t, d)
	if _, still := callOf(t, d, "p-2"); still {
		t.Fatal("the plan is still offered after the approval was done")
	}
}

// acceptReq presses Accept as the page does: from the call in the snapshot and
// the state the holding area is in.
func acceptReq(t testing.TB, d *Demo) server.HoldingRequest {
	t.Helper()
	c := mustCall(t, d, "a-")
	seq, err := strconv.ParseInt(strings.TrimPrefix(c.ID, "a-"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	st, ok, err := d.stgSt.LatestPublished(context.Background())
	if err != nil || !ok || st.Seq != seq {
		t.Fatalf("the call %s is not for the latest published state: %+v %v", c.ID, st, err)
	}
	return server.HoldingRequest{StagingSeq: seq, Commit: st.Commit, Version: c.Version, Steps: &memSteps{}}
}

func queueLen(d *Demo) int {
	d.fuMu.Lock()
	defer d.fuMu.Unlock()
	return len(d.fu)
}

// Leftover 2 of the W4 review: a person takes the approval label off by hand
// and approves again at the same version. The checks and the builds are not
// queued a second time, and the story does not stop.
func TestApproveAgainAfterTheLabelWasRemovedQueuesNothingTwice(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	c := mustCall(t, d, "p-2")
	if _, err := d.Approve(ctx, server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}}); err != nil {
		t.Fatal(err)
	}
	first := queueLen(d)
	if first != 3 { // the checks and two tickets
		t.Fatalf("queued after the first press: %d", first)
	}
	if err := d.host.RemoveLabel(ctx, 2, githost.LabelApproved); err != nil {
		t.Fatal(err)
	}
	c2 := mustCall(t, d, "p-2") // the plan is offered again
	if _, err := d.Approve(ctx, server.ApproveRequest{Call: c2.ID, Version: c2.Version, Steps: &memSteps{}}); err != nil {
		t.Fatalf("second press: %v", err)
	}
	if n := queueLen(d); n != first {
		t.Fatalf("queued after the second press: %d, want %d", n, first)
	}
	// One task of each kind, never two: the checks once and a build for each ticket once.
	d.fuMu.Lock()
	kinds := map[fuKind]int{}
	for _, f := range d.fu {
		kinds[f.kind]++
	}
	d.fuMu.Unlock()
	if kinds[fuChecks] != 1 || kinds[fuTicket] != 2 {
		t.Fatalf("queued tasks = %v", kinds)
	}
}

// Leftover 3: a ticket the factory made and a person edited during a failed
// Approve is never adopted. The next Approve refuses with stale, and makes no
// third ticket.
func TestApproveRefusesWhileAnEditedFactoryTicketStands(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	c := mustCall(t, d, "p-2")
	d.host.FailNext("AddLabel", false) // the first label of the first ticket fails: the ticket is made, not labelled
	if _, err := d.Approve(ctx, server.ApproveRequest{Call: c.ID, Version: c.Version, Steps: &memSteps{}}); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("first press: %v", err)
	}
	var kids []int
	children := func() int {
		kids = nil
		for i := 1; i < 40; i++ {
			if d.host.Parent(i) == 2 {
				kids = append(kids, i)
			}
		}
		return len(kids)
	}
	if children() != 1 {
		t.Fatalf("children after the failure: %d", children())
	}
	d.host.EditIssueBody(kids[0], "A person changed this ticket.") // the one the failed press made
	c2 := mustCall(t, d, "p-2")
	_, err := d.Approve(ctx, server.ApproveRequest{Call: c2.ID, Version: c2.Version, Steps: &memSteps{}})
	if !errors.Is(err, server.ErrStale) {
		t.Fatalf("second press: %v", err)
	}
	if children() != 1 {
		t.Fatalf("a duplicate was made: %d children", children())
	}
	if hasLabel(t, d, 2, githost.LabelApproved) || countKind(t, d, "plan_approved") != 1 || queueLen(d) != 0 {
		t.Fatal("a refused press changed something")
	}
}

// ---- Accept, Cancel, Undo, the preview and the piece detail ----

func newDemoWith(t *testing.T, edit func(*Config)) *Demo {
	t.Helper()
	cfg := testConfig(t)
	edit(&cfg)
	d, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// gaugeValue reads one gauge of the snapshot.
func gaugeValue(t testing.TB, d *Demo, key floor.GaugeKey) int {
	t.Helper()
	b, err := d.Floor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Gauges []floor.Gauge `json:"gauges"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	for _, g := range f.Gauges {
		if g.Key == key {
			if g.Value == nil {
				t.Fatalf("gauge %s has no value: %+v", key, g)
			}
			return int(*g.Value)
		}
	}
	t.Fatalf("no gauge %s", key)
	return 0
}

func mainHead(d *Demo) string { return d.host.BranchHead("main") }

func journalSeq(t testing.TB, d *Demo) int64 { return snap(t, d).JournalSeq }

// undoReq asks for an Undo as the page does: from the preview.
func undoReq(t testing.TB, d *Demo, change string) server.UndoRequest {
	t.Helper()
	pv, err := d.UndoPreview(context.Background(), change)
	if err != nil {
		t.Fatalf("UndoPreview(%s): %v", change, err)
	}
	return server.UndoRequest{Change: change, StagingSeq: pv.StagingSeq, Commit: pv.Commit, AlsoRemoved: pv.AlsoRemoved, Steps: &memSteps{}}
}

// runOf is the staging entry id of a ticket's piece, which the demo makes the
// run id.
func runOf(t testing.TB, d *Demo, id string) string {
	t.Helper()
	p := piece(snap(t, d), id)
	if p == nil || p.Run == nil {
		t.Fatalf("no run for %s: %+v", id, p)
	}
	return *p.Run
}

func TestAcceptMergesTheHoldingAreaAndTheStoryGoesOn(t *testing.T) {
	d := beatsOnly(t, "write_checks", "build_move")
	ctx := context.Background()
	c := mustCall(t, d, "a-")
	if len(c.Actions) != 1 || c.Actions[0] != floor.ActionAccept {
		t.Fatalf("actions = %v", c.Actions)
	}
	req := acceptReq(t, d)
	if p := piece(snap(t, d), "t3"); p == nil || p.Station != 6 {
		t.Fatalf("t3 before: %+v", p)
	}
	before := mainHead(d)
	t3 := runOf(t, d, "t3")
	undoBefore := undoReq(t, d, t3) // asked for now, pressed after the Accept
	// Attack 4 and 15: nothing the browser sends is believed.
	seq0 := journalSeq(t, d)
	bad := map[string]func(*server.HoldingRequest){
		"version zero":  func(r *server.HoldingRequest) { r.Version = strings.Repeat("0", 64) },
		"version blank": func(r *server.HoldingRequest) { r.Version = "" },
		"old number":    func(r *server.HoldingRequest) { r.StagingSeq-- },
		"future number": func(r *server.HoldingRequest) { r.StagingSeq++ },
		"other commit":  func(r *server.HoldingRequest) { r.Commit = strings.Repeat("a", 40) },
		"blank commit":  func(r *server.HoldingRequest) { r.Commit = "" },
		"upper commit":  func(r *server.HoldingRequest) { r.Commit = strings.ToUpper(r.Commit) },
	}
	for name, edit := range bad {
		r := req
		r.Steps = &memSteps{}
		edit(&r)
		if _, err := d.Accept(ctx, r); !errors.Is(err, server.ErrStale) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := d.CancelAccept(ctx, r); !errors.Is(err, server.ErrStale) {
			t.Errorf("cancel %s: %v", name, err)
		}
	}
	if _, err := d.CancelAccept(ctx, req); !errors.Is(err, server.ErrStale) {
		t.Errorf("cancel with nothing pending: %v", err)
	}
	if mainHead(d) != before || journalSeq(t, d) != seq0 || countKind(t, d, "accepted") != 0 {
		t.Fatal("a refused press changed something")
	}
	if _, pend, _ := d.stgSt.GetPending(ctx); pend {
		t.Fatal("an Accept is pending after refusals")
	}
	res, err := d.Accept(ctx, req)
	if err != nil || res.Result != server.ResultDone || res.JournalSeq <= 0 || res.JournalSeq != journalSeq(t, d) {
		t.Fatalf("Accept = %+v, %v", res, err)
	}
	if mainHead(d) == before || mainHead(d) != req.Commit {
		t.Fatalf("the default branch is %s, want the accepted state %s", mainHead(d), req.Commit)
	}
	if got, err := d.repo.Resolve(ctx, repo.MirrorRef); err != nil || got != req.Commit {
		t.Fatalf("mirror = %s %v", got, err)
	}
	f := snap(t, d)
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	if p := piece(f, "t3"); p == nil || p.Station != 7 {
		t.Fatalf("t3 after: %+v", p)
	}
	if _, still := callOf(t, d, "a-"); still {
		t.Fatal("the Accept call stays")
	}
	if n := countKind(t, d, "accepted"); n != 1 {
		t.Fatalf("accepted entries = %d", n) // one goal inside
	}
	if gaugeValue(t, d, floor.GaugeAcceptedToday) != 1 {
		t.Fatal("Accepted today is not 1")
	}
	// The same press again is refused: the call is gone.
	again := req
	again.Steps = &memSteps{}
	if _, err := d.Accept(ctx, again); !errors.Is(err, server.ErrStale) {
		t.Fatalf("second press: %v", err)
	}
	// Attack 23: an accepted change cannot be undone, and the preview from before is stale.
	if _, err := d.Undo(ctx, undoBefore); !errors.Is(err, server.ErrStale) {
		t.Fatalf("undo after accept: %v", err)
	}
	if _, err := d.UndoPreview(ctx, t3); !errors.Is(err, server.ErrStale) {
		t.Fatalf("preview after accept: %v", err)
	}
	if countKind(t, d, "undone") != 0 {
		t.Fatal("an undone entry was written")
	}
	// The story goes on, on the new default: t4 depends on t3, which is accepted.
	playBeats(t, d, "second_try")
	f = snap(t, d)
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	if p := piece(f, "t3"); p == nil || p.Station != 7 {
		t.Fatalf("t3 is no longer accepted: %+v", p)
	}
	if p := piece(f, "t4"); p == nil || p.Station != 6 || p.State != floor.StateReady {
		t.Fatalf("t4 after the new state: %+v", p)
	}
	req2 := acceptReq(t, d)
	if req2.StagingSeq <= req.StagingSeq {
		t.Fatalf("no new state: %d", req2.StagingSeq)
	}
	res, err = d.Accept(ctx, req2)
	if err != nil || res.Result != server.ResultDone {
		t.Fatalf("second Accept = %+v, %v", res, err)
	}
	f = snap(t, d)
	if p := piece(f, "t4"); p == nil || p.Station != 7 {
		t.Fatalf("t4 after the second Accept: %+v", p)
	}
	if got := gaugeValue(t, d, floor.GaugeAcceptedToday); got != 2 {
		t.Fatalf("Accepted today = %d, want 2", got)
	}
	if n := countKind(t, d, "accepted"); n != 2 {
		t.Fatalf("accepted entries = %d", n)
	}
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
}

// Developer mode: no accept token. Accept opens the pull request, answers
// "pending", writes no accepted entry, and the line is frozen.
func TestAcceptInDeveloperModeIsPending(t *testing.T) {
	d := newDemoWith(t, func(c *Config) { c.noAcceptToken = true })
	playBeats(t, d, "write_checks", "build_move")
	ctx := context.Background()
	req := acceptReq(t, d)
	res, err := d.Accept(ctx, req)
	if err != nil || res.Result != server.ResultPending || res.JournalSeq != journalSeq(t, d) {
		t.Fatalf("Accept = %+v, %v", res, err)
	}
	if countKind(t, d, "accepted") != 0 {
		t.Fatal("an accepted entry before the change is part of the product")
	}
	c := mustCall(t, d, "a-")
	if len(c.Actions) != 0 {
		t.Fatalf("developer mode offers %v", c.Actions)
	}
	f := snap(t, d)
	if p := piece(f, "t3"); p == nil || p.Station != 6 || code(p) != floor.AcceptPending {
		t.Fatalf("t3: %+v", p)
	}
	for name, err := range map[string]error{
		"accept again": func() error {
			r := req
			r.Version = c.Version
			r.Steps = &memSteps{}
			_, err := d.Accept(ctx, r)
			return err
		}(),
		"cancel": func() error {
			r := req
			r.Version = c.Version
			r.Steps = &memSteps{}
			_, err := d.CancelAccept(ctx, r)
			return err
		}(),
	} {
		if !errors.Is(err, server.ErrStale) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := d.UndoPreview(ctx, runOf(t, d, "t3")); !errors.Is(err, server.ErrStale) {
		t.Errorf("preview while pending: %v", err)
	}
	r := undoReq2(runOf(t, d, "t3"), req)
	if _, err := d.Undo(ctx, r); !errors.Is(err, server.ErrStale) {
		t.Errorf("undo while pending: %v", err)
	}
}

func undoReq2(change string, from server.HoldingRequest) server.UndoRequest {
	return server.UndoRequest{Change: change, StagingSeq: from.StagingSeq, Commit: from.Commit, AlsoRemoved: []string{}, Steps: &memSteps{}}
}

// prFailer makes the host fail when the factory reads a pull request, so a
// merge that began cannot be confirmed.
type prFailer struct {
	githost.Proposer
	fail *atomic.Bool
}

func (p prFailer) GetPullRequest(ctx context.Context, n int) (githost.PullRequest, error) {
	if p.fail.Load() {
		return githost.PullRequest{}, errors.New("host down")
	}
	return p.Proposer.GetPullRequest(ctx, n)
}

// Cancel appears only when a merge started and did not finish. Here the host
// fails during the merge and cannot be read: the Accept stays pending.
func TestCancelWhenAMergeStartedAndDidNotFinish(t *testing.T) {
	var down atomic.Bool
	d := newDemoWith(t, func(c *Config) {
		c.wrapHost = func(p githost.Proposer) githost.Proposer { return prFailer{p, &down} }
	})
	playBeats(t, d, "write_checks", "build_move")
	ctx := context.Background()
	req := acceptReq(t, d)
	seq0 := journalSeq(t, d)
	down.Store(true)
	d.host.FailNext("MergeStaging", false)
	if _, err := d.Accept(ctx, req); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("Accept: %v", err)
	}
	down.Store(false)
	if journalSeq(t, d) != seq0 || countKind(t, d, "accepted") != 0 {
		t.Fatal("a failed press wrote an entry")
	}
	c := mustCall(t, d, "a-")
	if len(c.Actions) != 1 || c.Actions[0] != floor.ActionCancel {
		t.Fatalf("actions = %v", c.Actions)
	}
	// Accept is not offered, and Cancel needs the new version and the commit.
	if _, err := d.Accept(ctx, req); !errors.Is(err, server.ErrStale) {
		t.Fatalf("Accept while pending: %v", err)
	}
	cancel := server.HoldingRequest{StagingSeq: req.StagingSeq, Commit: req.Commit, Version: c.Version, Steps: &memSteps{}}
	wrong := cancel
	wrong.Commit = strings.Repeat("b", 40)
	if _, err := d.CancelAccept(ctx, wrong); !errors.Is(err, server.ErrStale) {
		t.Fatalf("cancel with another commit: %v", err)
	}
	old := cancel
	old.Version = req.Version
	if _, err := d.CancelAccept(ctx, old); !errors.Is(err, server.ErrStale) {
		t.Fatalf("cancel with the old version: %v", err)
	}
	if _, pend, _ := d.stgSt.GetPending(ctx); !pend {
		t.Fatal("a refused cancel ended the Accept")
	}
	res, err := d.CancelAccept(ctx, cancel)
	if err != nil || res.Result != server.ResultDone || res.JournalSeq != journalSeq(t, d) || res.JournalSeq <= seq0 {
		t.Fatalf("Cancel = %+v, %v", res, err)
	}
	if countKind(t, d, "accept_cancelled") != 1 {
		t.Fatal("no accept_cancelled entry")
	}
	c = mustCall(t, d, "a-")
	if len(c.Actions) != 1 || c.Actions[0] != floor.ActionAccept {
		t.Fatalf("after cancel: %v", c.Actions)
	}
	if err := invariants(snap(t, d)); err != nil {
		t.Fatal(err)
	}
	// Attack 21: the same again, but the host merged meanwhile. Cancel is stale,
	// and the merge is recorded as accepted, as the factory reads it from the host.
	req2 := acceptReq(t, d)
	down.Store(true)
	d.host.FailNext("MergeStaging", false)
	if _, err := d.Accept(ctx, req2); !errors.Is(err, server.ErrHostFailed) {
		t.Fatalf("second Accept: %v", err)
	}
	down.Store(false)
	c2 := mustCall(t, d, "a-")
	pend, _, _ := d.stgSt.GetPending(ctx)
	d.host.SimulateMerge(pend.PR, "repo-owner") // the merge went through after all
	late := server.HoldingRequest{StagingSeq: req2.StagingSeq, Commit: req2.Commit, Version: c2.Version, Steps: &memSteps{}}
	if _, err := d.CancelAccept(ctx, late); !errors.Is(err, server.ErrStale) {
		t.Fatalf("Cancel after a merge: %v", err)
	}
	if countKind(t, d, "accept_cancelled") != 1 || countKind(t, d, "accepted") != 1 {
		t.Fatalf("cancelled=%d accepted=%d", countKind(t, d, "accept_cancelled"), countKind(t, d, "accepted"))
	}
	if p := piece(snap(t, d), "t3"); p == nil || p.Station != 7 {
		t.Fatalf("t3: %+v", p)
	}
}

func TestUndoTakesAChangeAndWhatDependsOnIt(t *testing.T) {
	d := beatsOnly(t, "write_checks", "build_move")
	ctx := context.Background()
	// Attack 19: an Accept with the state from before a rebuild is stale. A new
	// change that was added meanwhile is never merged without the person seeing it.
	oldAccept := acceptReq(t, d)
	head0 := mainHead(d)
	playBeats(t, d, "second_try") // t4 is added: a new state
	if _, err := d.Accept(ctx, oldAccept); !errors.Is(err, server.ErrStale) {
		t.Fatalf("accept of the old state: %v", err)
	}
	if mainHead(d) != head0 || countKind(t, d, "accepted") != 0 {
		t.Fatal("a stale press merged")
	}
	if fresh := acceptReq(t, d); fresh.StagingSeq <= oldAccept.StagingSeq || fresh.Commit == oldAccept.Commit {
		t.Fatalf("no new state: %+v %+v", oldAccept, fresh)
	}
	t3, t4 := runOf(t, d, "t3"), runOf(t, d, "t4")
	pv, err := d.UndoPreview(ctx, t3)
	if err != nil || pv.Change != t3 || !reflect.DeepEqual(pv.AlsoRemoved, []string{t4}) ||
		!reflect.DeepEqual(pv.Tickets, []server.UndoTicket{{Change: t3, Ticket: 3}, {Change: t4, Ticket: 4}}) {
		t.Fatalf("preview = %+v, %v", pv, err)
	}
	// The piece offers it, with the same state.
	b, ok, err := d.Piece(ctx, "t3")
	if err != nil || !ok {
		t.Fatalf("Piece: %v %v", ok, err)
	}
	var det struct {
		SchemaVersion int            `json:"schemaVersion"`
		Mode          floor.Mode     `json:"mode"`
		GeneratedAt   string         `json:"generatedAt"`
		Piece         floor.Piece    `json:"piece"`
		Route         []interface{}  `json:"route"`
		Rulings       []interface{}  `json:"rulings"`
		Unconfirmed   []interface{}  `json:"unconfirmed"`
		Undo          *floor.UndoRef `json:"undo"`
	}
	if err := json.Unmarshal(b, &det); err != nil || det.SchemaVersion != 1 || det.Mode != floor.ModeDemo || det.GeneratedAt == "" || det.Piece.ID != "t3" ||
		det.Route == nil || det.Rulings == nil || det.Unconfirmed == nil || det.Undo == nil || det.Undo.Change != t3 || det.Undo.StagingSeq != pv.StagingSeq || det.Undo.Commit != pv.Commit {
		t.Fatalf("detail undo = %+v (%v)", det.Undo, err)
	}
	// The detail of a piece that is not on the line is not there.
	for _, id := range []string{"t99", "p9", "x", "<script>", ""} {
		if _, ok, err := d.Piece(ctx, id); ok || err != nil {
			t.Errorf("Piece(%q) = %v %v", id, ok, err)
		}
	}
	head, seq0 := mainHead(d), journalSeq(t, d)
	good := server.UndoRequest{Change: t3, StagingSeq: pv.StagingSeq, Commit: pv.Commit, AlsoRemoved: pv.AlsoRemoved}
	bad := map[string]func(*server.UndoRequest){
		"list missing":   func(r *server.UndoRequest) { r.AlsoRemoved = []string{} },
		"list extra":     func(r *server.UndoRequest) { r.AlsoRemoved = []string{t4, t3} },
		"list other":     func(r *server.UndoRequest) { r.AlsoRemoved = []string{"nope"} },
		"old number":     func(r *server.UndoRequest) { r.StagingSeq-- },
		"future number":  func(r *server.UndoRequest) { r.StagingSeq++ },
		"other commit":   func(r *server.UndoRequest) { r.Commit = strings.Repeat("a", 40) },
		"unknown change": func(r *server.UndoRequest) { r.Change = "nope" },
		"the second one": func(r *server.UndoRequest) { r.Change, r.AlsoRemoved = t4, []string{} },
		"nil list":       func(r *server.UndoRequest) { r.AlsoRemoved = nil },
	}
	for name, edit := range bad {
		r := good
		r.AlsoRemoved = append([]string{}, good.AlsoRemoved...)
		r.Steps = &memSteps{}
		edit(&r)
		if name == "the second one" {
			// t4 is offered on its own, so this one is valid: it is not refused.
			continue
		}
		if _, err := d.Undo(ctx, r); !errors.Is(err, server.ErrStale) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The checks of the goal are not a piece at 06: not offered, so refused.
	st, _, _ := d.stgSt.LatestPublished(ctx)
	checks := ""
	for _, a := range st.Applied {
		if a.Ticket == a.Goal {
			checks = a.EntryID
		}
	}
	if checks == "" {
		t.Fatal("no checks entry")
	}
	r := good
	r.Change, r.AlsoRemoved, r.Steps = checks, []string{}, &memSteps{}
	if _, err := d.Undo(ctx, r); !errors.Is(err, server.ErrStale) {
		t.Errorf("checks entry: %v", err)
	}
	if journalSeq(t, d) != seq0 || mainHead(d) != head || countKind(t, d, "undone") != 0 {
		t.Fatal("a refused press changed something")
	}
	good.Steps = &memSteps{}
	res, err := d.Undo(ctx, good)
	if err != nil || res.Result != server.ResultDone || res.JournalSeq != journalSeq(t, d) {
		t.Fatalf("Undo = %+v, %v", res, err)
	}
	if n := countKind(t, d, "undone"); n != 2 {
		t.Fatalf("undone entries = %d", n)
	}
	f := snap(t, d)
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t3", "t4"} {
		if p := piece(f, id); p == nil || p.Station == 6 || p.State != floor.StateStuck || code(p) != floor.TakenOut {
			t.Errorf("%s after the undo: %+v", id, p)
		}
	}
	if _, ok := callOf(t, d, "a-"); ok {
		t.Fatal("an Accept call stays for a holding area with nothing left to accept")
	}
	if mainHead(d) != head {
		t.Fatal("an undo reached the default branch")
	}
	// The same press again: the state moved on.
	good.Steps = &memSteps{}
	if _, err := d.Undo(ctx, good); !errors.Is(err, server.ErrStale) {
		t.Fatalf("again: %v", err)
	}
	if b, ok, err := d.Piece(ctx, "t3"); err != nil || !ok || strings.Contains(string(b), `"undo":{`) {
		t.Fatalf("the detail still offers undo: %s %v %v", b, ok, err)
	}
	// While a long task holds the write lock, the last whole picture answers.
	holdWrite(t, d)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if b, ok, err := d.Piece(ctx, "t3"); err != nil || !ok || len(b) == 0 {
			t.Errorf("Piece under the lock: %v %v", ok, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Piece waits for the write lock")
	}
}

// Attack 18: Accept against Undo, with the same view of the state, over the
// server. One wins. The merged state never holds a change that was undone, and
// the winner's replay gives the same bytes. Run with -race.
func TestAcceptAndUndoAtOnceDoOne(t *testing.T) {
	d := beatsOnly(t, "write_checks", "build_move")
	srv, err := server.New(server.Config{Port: 18767, Mode: "demo", Source: d, Actions: d, Detail: d, Static: fstest.MapFS{"index.html": {Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	key := strings.SplitN(srv.Link(), "#t=", 2)[1]
	do := func(method, path, idem, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host, r.RemoteAddr = "127.0.0.1:18767", "127.0.0.1:5000"
		r.Header.Set("Authorization", "Bearer "+key)
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", idem)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	// The piece detail and the preview, as the page reads them.
	det := do("GET", "/api/v1/pieces/t3", "", "")
	var got struct {
		Mode string         `json:"mode"`
		Undo *floor.UndoRef `json:"undo"`
	}
	if det.Code != 200 || json.Unmarshal(det.Body.Bytes(), &got) != nil || got.Mode != "demo" || got.Undo == nil {
		t.Fatalf("piece: %d %s", det.Code, det.Body)
	}
	if rec := do("GET", "/api/v1/pieces/t99", "", ""); rec.Code != 404 {
		t.Fatalf("unknown piece: %d", rec.Code)
	}
	pv := do("GET", "/api/v1/changes/"+got.Undo.Change+"/undo-preview", "", "")
	var view struct {
		Mode        string   `json:"mode"`
		Change      string   `json:"change"`
		StagingSeq  int64    `json:"stagingSeq"`
		Commit      string   `json:"commit"`
		AlsoRemoved []string `json:"alsoRemoved"`
		Tickets     []struct {
			Change string `json:"change"`
			Ticket int    `json:"ticket"`
		} `json:"tickets"`
	}
	if pv.Code != 200 || json.Unmarshal(pv.Body.Bytes(), &view) != nil || view.Mode != "demo" || len(view.AlsoRemoved) != 0 || len(view.Tickets) != 1 ||
		view.Tickets[0].Ticket != 3 || view.StagingSeq != got.Undo.StagingSeq || view.Commit != got.Undo.Commit {
		t.Fatalf("preview: %d %s", pv.Code, pv.Body)
	}
	// A list that is not the preview's is refused.
	wrong, _ := json.Marshal(map[string]any{"stagingSeq": view.StagingSeq, "commit": view.Commit, "change": view.Change, "alsoRemoved": []string{"run999"}})
	if rec := do("POST", "/api/v1/changes/"+view.Change+"/undo", "undo-aaaaaaaaaaaaaaaa", string(wrong)); rec.Code != 409 || strings.TrimSpace(rec.Body.String()) != `{"error":"stale"}` {
		t.Fatalf("wrong list: %d %s", rec.Code, rec.Body)
	}
	accept := acceptReq(t, d)
	head := mainHead(d)
	undoBody, _ := json.Marshal(map[string]any{"stagingSeq": view.StagingSeq, "commit": view.Commit, "change": view.Change, "alsoRemoved": view.AlsoRemoved})
	acceptBody, _ := json.Marshal(map[string]any{"stagingSeq": accept.StagingSeq, "commit": accept.Commit, "version": accept.Version})
	var wg sync.WaitGroup
	var ar, ur *httptest.ResponseRecorder
	wg.Add(2)
	go func() {
		defer wg.Done()
		ar = do("POST", "/api/v1/holding-area/accept", "acc-bbbbbbbbbbbbbbbb", string(acceptBody))
	}()
	go func() {
		defer wg.Done()
		ur = do("POST", "/api/v1/changes/"+view.Change+"/undo", "undo-bbbbbbbbbbbbbbbb", string(undoBody))
	}()
	wg.Wait()
	if (ar.Code == 200) == (ur.Code == 200) {
		t.Fatalf("accept: %d %s, undo: %d %s", ar.Code, ar.Body, ur.Code, ur.Body)
	}
	acceptedN, undoneN := countKind(t, d, "accepted"), countKind(t, d, "undone")
	if ar.Code == 200 {
		if !strings.HasPrefix(ar.Body.String(), `{"mode":"demo","result":"done","journalSeq":`) || ur.Code != 409 || acceptedN != 1 || undoneN != 0 || mainHead(d) != accept.Commit {
			t.Fatalf("accept won: undo=%d %s accepted=%d undone=%d", ur.Code, ur.Body, acceptedN, undoneN)
		}
		if again := do("POST", "/api/v1/holding-area/accept", "acc-bbbbbbbbbbbbbbbb", string(acceptBody)); again.Code != 200 || again.Body.String() != ar.Body.String() {
			t.Fatalf("replay: %d %s", again.Code, again.Body)
		}
	} else {
		if !strings.HasPrefix(ur.Body.String(), `{"mode":"demo","result":"done","journalSeq":`) || ar.Code != 409 || acceptedN != 0 || undoneN != 1 || mainHead(d) != head {
			t.Fatalf("undo won: accept=%d %s accepted=%d undone=%d", ar.Code, ar.Body, acceptedN, undoneN)
		}
		if again := do("POST", "/api/v1/changes/"+view.Change+"/undo", "undo-bbbbbbbbbbbbbbbb", string(undoBody)); again.Code != 200 || again.Body.String() != ur.Body.String() {
			t.Fatalf("replay: %d %s", again.Code, again.Body)
		}
	}
	if err := invariants(snap(t, d)); err != nil {
		t.Fatal(err)
	}
}

// Attack 20: a state that was recorded and not pushed is not the holding area.
// No Accept is offered for it and a press for it is stale, with no merge.
func TestAcceptOfAStateThatWasNotPublishedIsStale(t *testing.T) {
	d, fail := newDemoFailingPush(t)
	playBeats(t, d, "write_checks", "build_move")
	ctx := context.Background()
	old := acceptReq(t, d)
	head := mainHead(d)
	fail.Store(true)
	if _, err := d.stg.Rebuild(ctx); err == nil {
		t.Fatal("the push should have failed")
	}
	st, ok, _ := d.stgSt.Latest(ctx)
	if !ok || st.Published {
		t.Fatalf("setup: the latest state is published: %+v", st)
	}
	if _, err := d.Accept(ctx, old); !errors.Is(err, server.ErrStale) {
		t.Fatalf("old state: %v", err)
	}
	bad := server.HoldingRequest{StagingSeq: st.Seq, Commit: st.Commit, Version: strings.Repeat("a", 64), Steps: &memSteps{}}
	if _, err := d.Accept(ctx, bad); !errors.Is(err, server.ErrStale) {
		t.Fatalf("unpublished state: %v", err)
	}
	if _, ok := callOf(t, d, "a-"); ok {
		c := mustCall(t, d, "a-")
		if len(c.Actions) != 0 {
			t.Fatalf("an unpublished state offers %v", c.Actions)
		}
	}
	if mainHead(d) != head {
		t.Fatal("a merge happened")
	}
}
