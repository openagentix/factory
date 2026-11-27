package demo

import (
	"context"
	"strconv"
	"strings"
	"testing"

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
