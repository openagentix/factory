package floor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// undoInputs has ticket 57 at station 06, with the entries given for it.
func undoInputs(entries ...EntryIn) Inputs {
	in := withTickets(healthy(), ticket(57))
	in = withRuns(in, run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}})
	h := HoldingIn{Exists: true, StagingSeq: 7, Commit: strings.Repeat("a", 40), Published: true, Mine: true, Applied: entries}
	in.Holding = Some(h)
	return in
}

func entryID(ticket int, id string) EntryIn {
	return EntryIn{Ticket: ticket, Goal: 40, At: Some(at(-time.Hour)), EntryID: id}
}

func TestDetailUndoForAPieceAtTheHoldingArea(t *testing.T) {
	// The first applied entry is the earliest by time, whatever the list order.
	later := entryID(57, "run-1")
	later.At = Some(at(-30 * time.Minute))
	earlier := entryID(57, "run-4")
	earlier.At = Some(at(-2 * time.Hour))
	in := undoInputs(entryID(56, "run-9"), later, earlier)
	d, ok := Detail(in, "t57")
	if !ok || d.Piece.Station != 6 {
		t.Fatalf("setup: %+v %v", d.Piece, ok)
	}
	if d.Undo == nil || d.Undo.Change != "run-4" || d.Undo.StagingSeq != 7 || d.Undo.Commit != strings.Repeat("a", 40) {
		t.Fatalf("undo %+v", d.Undo) // the first applied entry of the ticket
	}
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), `"undo":{"change":"run-4","stagingSeq":7,"commit":"`) {
		t.Errorf("json %s", b)
	}
}

// Every other case is null, and the field is still in the JSON.
func TestDetailUndoIsNullUnlessAllRulesHold(t *testing.T) {
	cases := map[string]func(*Inputs){
		"not published": func(in *Inputs) { h := in.Holding.Val; h.Published = false; in.Holding = Some(h) },
		"frozen":        func(in *Inputs) { h := in.Holding.Val; h.Frozen = true; in.Holding = Some(h) },
		"pending": func(in *Inputs) {
			h := in.Holding.Val
			h.Frozen, h.PendingAccept, h.PendingMode, h.PendingSince = true, true, PendingMerge, Some(at(-time.Minute))
			in.Holding = Some(h)
		},
		"another machine holds it": func(in *Inputs) { h := in.Holding.Val; h.Mine = false; in.Holding = Some(h) },
		"no entry id":              func(in *Inputs) { h := in.Holding.Val; h.Applied = []EntryIn{entryID(57, "")}; in.Holding = Some(h) },
		"staging not read":         func(in *Inputs) { in.Sources[SourceStaging] = SourceReadFailed },
		"holding unknown":          func(in *Inputs) { in.Holding = Opt[HoldingIn]{} },
		"no holding area":          func(in *Inputs) { in.Holding = Some(HoldingIn{}) },
	}
	for name, edit := range cases {
		in := undoInputs(entryID(57, "run-1"))
		edit(&in)
		d, ok := Detail(in, "t57")
		if !ok {
			continue // the piece itself is not there: no undo either
		}
		if d.Undo != nil {
			t.Errorf("%s: undo %+v", name, d.Undo)
		}
		b, _ := json.Marshal(d)
		if !strings.Contains(string(b), `"undo":null`) {
			t.Errorf("%s: the field is missing: %s", name, b)
		}
	}
}

func TestDetailUndoNotForOtherPieces(t *testing.T) {
	// A ticket that is not at 06, and a plan piece and a checks piece.
	in := undoInputs(entryID(57, "run-1"))
	in = withTickets(in, ticket(57), ticket(58))
	for _, id := range []string{"t58", "p40", "k40"} {
		if d, ok := Detail(in, id); ok && d.Undo != nil {
			t.Errorf("%s has undo %+v", id, d.Undo)
		}
	}
	// A piece at 07 has none.
	acc := healthy()
	acc = withTickets(acc, ticket(58))
	acc.Accepted = Some([]AcceptedIn{{Ticket: 58, Goal: 40, At: Some(at(-time.Minute))}})
	if d, ok := Detail(acc, "t58"); ok && d.Undo != nil {
		t.Errorf("accepted: %+v", d.Undo)
	}
}

// Undoable lists every change that the Floor offers to take back: the same
// refs that the piece details carry, and nothing else.
func TestUndoableMatchesTheDetails(t *testing.T) {
	in := undoInputs(entryID(57, "run-1"))
	got := Undoable(in)
	want := []UndoRef{{Change: "run-1", StagingSeq: 7, Commit: strings.Repeat("a", 40)}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %+v", got)
	}
	d, _ := Detail(in, "t57")
	if d.Undo == nil || *d.Undo != got[0] {
		t.Fatalf("detail %+v", d.Undo)
	}
	h := in.Holding.Val
	h.Frozen = true
	in.Holding = Some(h)
	if got := Undoable(in); len(got) != 0 {
		t.Errorf("frozen: %+v", got)
	}
	if got := Undoable(healthy()); len(got) != 0 {
		t.Errorf("nothing: %+v", got)
	}
}
