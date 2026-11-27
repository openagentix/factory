package collect

import (
	"context"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
)

// Ledger holds the facts that no package stores yet (record 0004, "Code that
// must change"). The demo driver keeps them in memory. The orchestrator will
// replace this interface with real stored records. The collector reads the
// ledger and never writes to it, and it never invents a fact the ledger
// does not hold.
type Ledger interface {
	Facts(ctx context.Context) (LedgerFacts, error)
}

// RunFacts is what a run record lacks: when its phase last changed, why it was
// held, why it ended, whether it changed anything, and which build run a
// second-opinion run judged.
type RunFacts struct {
	PhaseSince     time.Time
	HeldReason     floor.ReasonCode
	EndReason      floor.ReasonCode
	HasChange      *bool // nil: not known
	SubjectRun     string
	SubjectAttempt int
	Deadline       time.Duration
}

// GoalFacts is what the host and the store do not keep about a goal.
type GoalFacts struct {
	PlanSince    time.Time
	BodyEditedAt time.Time
	Lane         floor.Lane
	// ManifestEntry is the id of the staging entry that carries the goal's
	// acceptance checks. The manifest is on staging when that entry is applied.
	ManifestEntry string
	// AcceptanceSource is the commit where the reviewed checks landed.
	AcceptanceSource string
	// Baseline is the state of each line before any building.
	Baseline map[int]api.AcceptanceState
}

// TicketFacts is what is kept about a ticket outside the host.
type TicketFacts struct {
	Route       []floor.RouteIn
	Rulings     []string
	Unconfirmed []string
	CutOffs     int
}

// Removal is the time a change was first seen taken out of the holding area.
type Removal struct {
	EntryID string
	Ticket  int
	At      time.Time
}

// LedgerFacts is a copy of everything the ledger holds.
type LedgerFacts struct {
	Runs    map[string]RunFacts
	Goals   map[int]GoalFacts
	Tickets map[int]TicketFacts
	// Gate is the sealed decision of each judged change, as a fact.
	Gate []floor.GateIn
	// Applied is the time each staging entry was first seen applied.
	Applied  map[string]time.Time
	Removals []Removal
	Accepted []floor.AcceptedIn
	// PendingSince is the start of a pending Accept.
	PendingSince time.Time
	Journal      []floor.JournalIn

	Paused               bool
	ProjectChecksFailing bool
	MaxAttempts          int
	FirstAttempts        []floor.FirstAttemptIn
	GoalTimings          []floor.GoalTimingIn
	// ScriptedStations and PartlyRealStations tell which stations are not
	// fully real.
	ScriptedStations   []floor.StationNo
	PartlyRealStations []floor.StationNo
}
