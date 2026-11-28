package demo

import (
	"context"
	"sync"
	"time"

	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
)

// memLedger is the demo's own memory of the facts no package stores yet. The
// orchestrator will replace it (see collect.Ledger). It is the only place
// where the driver writes a fact that the Floor reads. The driver never
// writes a piece, a station or a signal.
type memLedger struct {
	mu  sync.Mutex
	f   collect.LedgerFacts
	seq int64
}

func newLedger() *memLedger {
	return &memLedger{f: collect.LedgerFacts{
		Runs:        map[string]collect.RunFacts{},
		Goals:       map[int]collect.GoalFacts{},
		Tickets:     map[int]collect.TicketFacts{},
		Applied:     map[string]time.Time{},
		MaxAttempts: maxAttempts,
		// Station 1 and 5 are scripted. Station 2 and 3 are partly real: the
		// stub agent writes the files, and the checks and runs are real.
		ScriptedStations:   []floor.StationNo{1, 5},
		PartlyRealStations: []floor.StationNo{2, 3},
	}}
}

// Facts returns a copy, so a reader never sees a half-made change.
func (l *memLedger) Facts(context.Context) (collect.LedgerFacts, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.f
	c.Runs = map[string]collect.RunFacts{}
	for k, v := range l.f.Runs {
		c.Runs[k] = v
	}
	c.Goals = map[int]collect.GoalFacts{}
	for k, v := range l.f.Goals {
		c.Goals[k] = v
	}
	c.Tickets = map[int]collect.TicketFacts{}
	for k, v := range l.f.Tickets {
		c.Tickets[k] = v
	}
	c.Applied = map[string]time.Time{}
	for k, v := range l.f.Applied {
		c.Applied[k] = v
	}
	c.Gate = append([]floor.GateIn(nil), l.f.Gate...)
	c.Removals = append([]collect.Removal(nil), l.f.Removals...)
	c.Accepted = append([]floor.AcceptedIn(nil), l.f.Accepted...)
	c.Journal = append([]floor.JournalIn(nil), l.f.Journal...)
	c.FirstAttempts = append([]floor.FirstAttemptIn(nil), l.f.FirstAttempts...)
	c.GoalTimings = append([]floor.GoalTimingIn(nil), l.f.GoalTimings...)
	return c, nil
}

func (l *memLedger) run(id string, fn func(*collect.RunFacts)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.f.Runs[id]
	fn(&r)
	l.f.Runs[id] = r
}

func (l *memLedger) goal(n int, fn func(*collect.GoalFacts)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	g := l.f.Goals[n]
	fn(&g)
	l.f.Goals[n] = g
}

func (l *memLedger) goalFacts(n int) collect.GoalFacts {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Goals[n]
}

func (l *memLedger) setGate(g floor.GateIn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.f.Gate[:0:0]
	for _, o := range l.f.Gate {
		if o.Ticket != g.Ticket {
			out = append(out, o)
		}
	}
	l.f.Gate = append(out, g)
}

func (l *memLedger) dropGate(ticket int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.f.Gate[:0:0]
	for _, o := range l.f.Gate {
		if o.Ticket != ticket {
			out = append(out, o)
		}
	}
	l.f.Gate = out
}

// journal appends one entry and returns its number.
func (l *memLedger) journal(kind floor.JournalKind, goal, ticket int, at time.Time) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	l.f.Journal = append(l.f.Journal, floor.JournalIn{Seq: l.seq, Time: floor.Some(at), Kind: kind, Goal: goal, Ticket: ticket})
	return l.seq
}

func (l *memLedger) stampApplied(entry string, at time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.f.Applied[entry]; ok {
		return false
	}
	l.f.Applied[entry] = at
	return true
}

func (l *memLedger) stampRemoval(entry string, ticket int, at time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.f.Removals {
		if r.EntryID == entry {
			return false
		}
	}
	l.f.Removals = append(l.f.Removals, collect.Removal{EntryID: entry, Ticket: ticket, At: at})
	return true
}

func (l *memLedger) with(fn func(*collect.LedgerFacts)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fn(&l.f)
}

// gateOf returns the gate fact of a ticket, if there is one.
func (l *memLedger) gateOf(ticket int) (floor.GateIn, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, g := range l.f.Gate {
		if g.Ticket == ticket {
			return g, true
		}
	}
	return floor.GateIn{}, false
}

// lastSeq is the number of the newest journal entry, or zero.
func (l *memLedger) lastSeq() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}
