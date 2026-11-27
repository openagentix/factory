package demo

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"errors"

	"github.com/openagentix/factory/internal/githost"
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
	if err := d.Drain(ctx); err != nil || d.Failed() {
		t.Fatalf("Drain: %v, failed=%v", err, d.Failed())
	}
	f := snap(t, d)
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
	ready := 0
	for _, p := range f.Pieces {
		if p.Goal == 2 && p.Station == 6 {
			ready++
		}
	}
	if ready != 2 || countKind(t, d, "checks_written") != 1 {
		t.Fatalf("ready=%d checks_written=%d", ready, countKind(t, d, "checks_written"))
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
