package floor

import (
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Opt is a value that may be unknown. The zero Opt is unknown. This is how a
// failed or missing read stays different from a real zero or an empty list.
type Opt[T any] struct {
	Val   T
	Known bool
}

// Some makes a known Opt.
func Some[T any](v T) Opt[T] { return Opt[T]{Val: v, Known: true} }

// Inputs is everything Project needs, read by someone else. Nothing in it is
// read again. No field holds a pointer, so equal inputs print equal.
//
// Which source owns which field:
//
//	host        Goals, Tickets, Questions, HostTime, HostReadAt, PollInterval
//	store       Runs, Paused, Levels, FirstAttempts, Journal
//	config      MaxAttempts, Machine, Mode
//	team        Claims
//	gate        Gate
//	staging     Holding, Accepted, ProjectChecksFailing, GoalTimings
//	acceptance  Goal.ManifestOnStaging, Goal.Lines
//	trajectory  Goal.Course
//
// A source marked ok whose field is unknown is treated as read_failed. A
// source that is missing from Sources is read_failed.
type Inputs struct {
	// Now is the server's clock. Zero means unknown, and then nothing can be
	// judged: every source is read_failed.
	Now time.Time
	// HostTime is the git host's time at the last successful read, and
	// HostReadAt is the server's time when that read happened. When HostReadAt
	// is older than two PollIntervals, the host source is read_failed.
	HostTime     Opt[time.Time]
	HostReadAt   Opt[time.Time]
	PollInterval time.Duration

	Mode       Mode
	DemoStory  string // a short key such as "booking_tour"; anything else is dropped
	Repository string // owner/name
	Machine    string // this machine's id

	Sources map[SourceKey]SourceState
	// ScriptedStations and PartlyRealStations mark stations whose source is
	// ok but not fully real (demo mode).
	ScriptedStations   []StationNo
	PartlyRealStations []StationNo

	Goals     Opt[[]GoalIn]
	Tickets   Opt[[]TicketIn]
	Questions Opt[[]QuestionIn]
	Runs      Opt[[]RunIn]
	Claims    Opt[[]ClaimIn]
	Gate      Opt[[]GateIn]
	Holding   Opt[HoldingIn]
	Accepted  Opt[[]AcceptedIn]

	ProjectChecksFailing Opt[bool]
	Paused               Opt[bool]
	// Levels maps a task name to its dial level. A task that is not in the map
	// has no level (level_unset).
	Levels      Opt[map[string]int]
	MaxAttempts Opt[int]

	FirstAttempts Opt[[]FirstAttemptIn]
	GoalTimings   Opt[[]GoalTimingIn]
	// Attention is not built in the first release. Unknown means not built.
	Attention Opt[[]AttentionIn]
	Journal   Opt[[]JournalIn]
}

type GoalIn struct {
	Number int
	Title  string
	Open   bool
	// Approved is the effective state of the approval, as record 0003 reads it.
	Approved  Opt[bool]
	PlanSince Opt[time.Time]
	Lane      Lane   // "" means unknown
	Course    Course // "" means unknown or not built
	// PlanLines are the lines as read from the goal body, and BodyEditedAt the
	// body's last edit time. Both feed the version of a plan call.
	PlanLines    Opt[[]string]
	BodyEditedAt Opt[time.Time]
	// ManifestOnStaging says whether the goal's acceptance manifest is on the
	// staging state.
	ManifestOnStaging Opt[bool]
	Lines             Opt[[]LineIn]
	LinesAsOf         Opt[int64]
}

type LineIn struct {
	ID         int
	Text       string
	State      LineState
	NoteCode   ReasonCode
	NoteTicket int
}

type TicketIn struct {
	Number      int
	Goal        int
	Title       string
	Open        bool
	Ready       bool
	NeedsHuman  bool
	DependsOn   []int
	CreatedAt   Opt[time.Time]
	CutOffs     int
	ClaimLosts  int
	Route       []RouteIn
	Rulings     []string
	Unconfirmed []string
}

type RouteIn struct {
	Station StationNo
	At      Opt[time.Time]
}

type QuestionIn struct {
	Ticket    int
	Goal      int
	CommentID int64
	Since     Opt[time.Time]
	Text      string
}

// RunIn is the part of a run record the Floor reads. Phase is the run's phase.
// HeldReason and EndReason are the codes of record 0003.
type RunIn struct {
	ID         string
	Task       string // acceptance, implement, fix-ci or review
	Goal       int
	Ticket     int // 0 for a goal-level run such as the acceptance task
	Attempt    int
	Phase      api.Phase
	HeldReason ReasonCode
	EndReason  ReasonCode
	CreatedAt  Opt[time.Time]
	StartedAt  Opt[time.Time]
	PhaseSince Opt[time.Time]
	Machine    string
	HasChange  Opt[bool]
	// Deadline is the task's own deadline. Zero means unknown.
	Deadline time.Duration
}

// ClaimIn is a claim by another machine, as read from the git host. Times are
// the host's.
type ClaimIn struct {
	Ticket  int
	Machine string
	Since   Opt[time.Time]
	Expires Opt[time.Time]
}

// GateIn is what the gate says about one change.
type GateIn struct {
	Ticket  int
	Status  GateStatus
	Code    ReasonCode // for GateException: the stuck code
	Since   Opt[time.Time]
	Risk    RiskReason // for GateConsequenceOpen
	Run     string
	Attempt int
	Commit  string
	Machine string // the machine that holds the sealed decision
}

type HoldingIn struct {
	Exists        bool
	StagingSeq    int64
	Commit        string
	Published     bool
	Frozen        bool
	Mine          bool // this machine holds staging
	PendingAccept bool
	PendingSince  Opt[time.Time]
	PendingMode   PendingMode
	PullRequest   int
	Applied       []EntryIn
	Removed       []RemovedIn
}

type EntryIn struct {
	Ticket int
	Goal   int
	At     Opt[time.Time]
}

type RemovedIn struct {
	Ticket int
	At     Opt[time.Time]
}

type AcceptedIn struct {
	Ticket int
	Goal   int
	At     Opt[time.Time]
}

type FirstAttemptIn struct {
	Ticket         int
	EndedAt        Opt[time.Time]
	ReachedHolding bool
}

type GoalTimingIn struct {
	Goal           int
	ApprovedAt     Opt[time.Time]
	FirstHoldingAt Opt[time.Time]
}

type AttentionIn struct {
	Goal       int
	Actions    int
	AcceptedAt Opt[time.Time]
}

type JournalIn struct {
	Seq    int64
	Time   Opt[time.Time]
	Kind   JournalKind
	Goal   int
	Ticket int
	Facts  []NamedFact
}

// NamedFact is one entry of a call's or journal entry's facts. Key is chosen
// by this package or by trusted callers, and must be 1 to 20 ASCII letters.
type NamedFact struct {
	Key  string
	Fact Fact
}
