package floor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

var t0 = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

const me = "alice-laptop"

func at(d time.Duration) time.Time { return t0.Add(d) }

// healthy is a project where every source reads fine and nothing is going on.
func healthy() Inputs {
	src := map[SourceKey]SourceState{}
	for _, k := range AllSourceKeys() {
		src[k] = SourceOK
	}
	return Inputs{
		Now: t0, HostTime: Some(at(-5 * time.Second)), HostReadAt: Some(at(-5 * time.Second)), PollInterval: 30 * time.Second,
		Mode: ModeLive, Repository: "acme/booking", Machine: me, Sources: src,
		Goals:                Some([]GoalIn{goal(40)}),
		Tickets:              Some([]TicketIn{}),
		Questions:            Some([]QuestionIn{}),
		Runs:                 Some([]RunIn{}),
		Claims:               Some([]ClaimIn{}),
		Gate:                 Some([]GateIn{}),
		Holding:              Some(HoldingIn{}),
		Accepted:             Some([]AcceptedIn{}),
		ProjectChecksFailing: Some(false), Paused: Some(false),
		Levels:        Some(map[string]int{"implement": 2, "acceptance": 2, "review": 2}),
		MaxAttempts:   Some(3),
		FirstAttempts: Some([]FirstAttemptIn{}), GoalTimings: Some([]GoalTimingIn{}),
		Journal: Some([]JournalIn{}),
	}
}

func goal(n int) GoalIn {
	return GoalIn{Number: n, Title: "Goal title", Open: true, Approved: Some(true), Lane: LaneQuick, Course: CourseOn,
		ManifestOnStaging: Some(true), Lines: Some([]LineIn{{ID: 1, Text: "A customer can move a booking", State: LineWorking}}),
		LinesAsOf: Some(int64(3)), PlanLines: Some([]string{"A customer can move a booking"}), BodyEditedAt: Some(at(-24 * time.Hour))}
}

func ticket(n int) TicketIn {
	return TicketIn{Number: n, Goal: 40, Title: "Ticket title", Open: true, Ready: true, CreatedAt: Some(at(-2 * time.Hour))}
}

func run(ticketNo, attempt int, phase api.Phase) RunIn {
	return RunIn{ID: "r_" + string(rune('A'+attempt)) + itoa(ticketNo), Task: "implement", Goal: 40, Ticket: ticketNo, Attempt: attempt, Phase: phase,
		CreatedAt: Some(at(-time.Hour)), StartedAt: Some(at(-50 * time.Minute)), PhaseSince: Some(at(-10 * time.Minute)), Machine: me, HasChange: Some(true)}
}

func withTickets(in Inputs, ts ...TicketIn) Inputs { in.Tickets = Some(ts); return in }
func withRuns(in Inputs, rs ...RunIn) Inputs       { in.Runs = Some(rs); return in }

func get(t *testing.T, f Floor, id string) Piece {
	t.Helper()
	for _, p := range f.Pieces {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no piece %s in %v", id, pieceIDs(f))
	return Piece{}
}

func pieceIDs(f Floor) []string {
	var ids []string
	for _, p := range f.Pieces {
		ids = append(ids, p.ID)
	}
	return ids
}

func code(p Piece) ReasonCode {
	if p.Reason == nil {
		return ""
	}
	return p.Reason.Code
}

func expect(t *testing.T, f Floor, id string, station StationNo, state PieceState, c ReasonCode) Piece {
	t.Helper()
	p := get(t, f, id)
	if p.Station != station || p.State != state || code(p) != c {
		t.Errorf("%s: station %d state %s code %q; want station %d state %s code %q", id, p.Station, p.State, code(p), station, state, c)
	}
	return p
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func callOfKind(f Floor, k CallKind) *Call {
	for i := range f.Calls {
		if f.Calls[i].Kind == k {
			return &f.Calls[i]
		}
	}
	return nil
}

func station(f Floor, n StationNo) Station { return f.Stations[n-1] }

func gauge(f Floor, k GaugeKey) Gauge {
	for _, g := range f.Gauges {
		if g.Key == k {
			return g
		}
	}
	return Gauge{}
}
