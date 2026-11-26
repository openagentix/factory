package api

import "time"

// Risk levels in the evidence bundle.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// TriggerRef says what started a run: the kind and a reference such as #42.
type TriggerRef struct {
	Kind string `json:"kind" yaml:"kind"`
	Ref  string `json:"ref" yaml:"ref"`
}

// DiffSummary sizes the change a run made.
type DiffSummary struct {
	Files   int      `json:"files"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
	Paths   []string `json:"paths"`
}

// ProtectedPathsReport lists protected paths a run touched.
type ProtectedPathsReport struct {
	Touched []string `json:"touched"`
	OK      bool     `json:"ok"`
}

// CheckResult is the outcome of one check run by the orchestrator.
type CheckResult struct {
	Name            string `json:"name"`
	ExitCode        int    `json:"exitCode"`
	DurationSeconds int    `json:"durationSeconds"`
	RanBy           string `json:"ranBy"`
}

// RunLimits are the limits a run had.
type RunLimits struct {
	Deadline   string  `json:"deadline"`
	MaxCostUSD float64 `json:"maxCostUSD"`
}

// Risk is the rule based risk level of a change.
type Risk struct {
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

// How a run was started.
const (
	// ModeUnattended is a run the factory started and nobody watched.
	ModeUnattended = "unattended"
	// ModeWheel is a work stage a person did in their own session.
	ModeWheel = "wheel"
)

// Results of the machine gate.
const (
	GatePending   = "pending"
	GatePassed    = "passed"
	GateFailed    = "failed"
	GateException = "exception"
)

// Denial is a request the permission policy refused.
type Denial struct {
	Request string `json:"request"`
	Reason  string `json:"reason"`
}

// AcceptanceEntry is the state of one criterion after a run, and before it.
type AcceptanceEntry struct {
	ID     int             `json:"id"`
	State  AcceptanceState `json:"state"`
	Before AcceptanceState `json:"before"`
}

// ReviewSummary is the review of a change as the evidence bundle records it.
type ReviewSummary struct {
	Run     string `json:"run"`
	Harness string `json:"harness"`
	Verdict string `json:"verdict"`
	// SingleOpinion is true when the reviewer used the builder's harness.
	// Such a change never counts toward automatic Accept.
	SingleOpinion bool `json:"singleOpinion"`
}

// Evidence is the evidence bundle. The orchestrator builds it, not the worker.
// The review, risk and gate fields are added when the review ends.
type Evidence struct {
	SchemaVersion int    `json:"schemaVersion"`
	Run           string `json:"run"`
	Task          string `json:"task"`
	Goal          int    `json:"goal,omitempty"`
	Ticket        int    `json:"ticket,omitempty"`
	Attempt       int    `json:"attempt"`
	Mode          string `json:"mode"`
	// Method is what was asked for. MethodUsed is what ran.
	Method         string               `json:"method"`
	MethodUsed     string               `json:"methodUsed"`
	Trigger        TriggerRef           `json:"trigger"`
	Worker         HarnessInfo          `json:"worker"`
	Base           string               `json:"base,omitempty"`
	Commit         string               `json:"commit,omitempty"`
	Diff           DiffSummary          `json:"diff"`
	Claims         []Claim              `json:"claims"`
	Rulings        []Ruling             `json:"rulings"`
	Denials        []Denial             `json:"denials"`
	ProtectedPaths ProtectedPathsReport `json:"protectedPaths"`
	// TestFilesChanged lists existing test files that were changed or
	// deleted. TestFilesAdded lists new ones. They are kept apart because
	// only the first raises the risk.
	TestFilesChanged []string          `json:"testFilesChanged"`
	TestFilesAdded   []string          `json:"testFilesAdded"`
	Checks           []CheckResult     `json:"checks"`
	Acceptance       []AcceptanceEntry `json:"acceptance"`
	Review           *ReviewSummary    `json:"review,omitempty"`
	Risk             Risk              `json:"risk"`
	Gate             string            `json:"gate,omitempty"`
	Usage            Usage             `json:"usage"`
	Limits           RunLimits         `json:"limits"`
}

// Phase is the stage of a run.
type Phase string

// Run phases.
const (
	PhaseQueued     Phase = "queued"
	PhaseHeld       Phase = "held"
	PhaseWorking    Phase = "working"
	PhaseVerifying  Phase = "verifying"
	PhasePublishing Phase = "publishing"
	PhaseSucceeded  Phase = "succeeded"
	PhaseFailed     Phase = "failed"
	PhaseNeedsHuman Phase = "needs_human"
	PhaseCancelled  Phase = "cancelled"
)

// Valid reports whether p is one of the phases in decision 0001.
func (p Phase) Valid() bool {
	switch p {
	case PhaseQueued, PhaseHeld, PhaseWorking, PhaseVerifying, PhasePublishing,
		PhaseSucceeded, PhaseFailed, PhaseNeedsHuman, PhaseCancelled:
		return true
	}
	return false
}

// nextPhases lists the phase changes a run may make. A held run waits for the
// cap, a throttle or a staging freeze, and then starts to work. A run that
// made no change still goes through verifying and publishing. The record does
// not list the changes, so this table is a choice.
var nextPhases = map[Phase][]Phase{
	PhaseQueued:     {PhaseHeld, PhaseWorking, PhaseCancelled},
	PhaseHeld:       {PhaseWorking, PhaseCancelled},
	PhaseWorking:    {PhaseVerifying, PhaseFailed, PhaseNeedsHuman, PhaseCancelled},
	PhaseVerifying:  {PhasePublishing, PhaseFailed, PhaseNeedsHuman, PhaseCancelled},
	PhasePublishing: {PhaseSucceeded, PhaseFailed, PhaseNeedsHuman, PhaseCancelled},
}

// CanTransition reports whether a run may change from one phase to another.
// A terminal phase never changes.
func CanTransition(from, to Phase) bool {
	for _, next := range nextPhases[from] {
		if next == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether a run in this phase has ended.
func (p Phase) IsTerminal() bool {
	switch p {
	case PhaseSucceeded, PhaseFailed, PhaseNeedsHuman, PhaseCancelled:
		return true
	}
	return false
}

// RunInput records what a run was given.
type RunInput struct {
	Task       *Task  `json:"task,omitempty"`
	PromptHash string `json:"promptHash,omitempty"`
}

// RunOutputs records what a run produced on the git host.
type RunOutputs struct {
	Branch      string `json:"branch,omitempty"`
	Commit      string `json:"commit,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
}

// Run is the run record. Field names follow the planned OpenAgentix AgentRun.
type Run struct {
	ID         string     `json:"id"`
	Task       string     `json:"task"`
	Attempt    int        `json:"attempt"`
	Goal       int        `json:"goal,omitempty"`
	Ticket     int        `json:"ticket,omitempty"`
	Mode       string     `json:"mode"`
	Method     string     `json:"method"`
	MethodUsed string     `json:"methodUsed"`
	Trigger    TriggerRef `json:"trigger"`
	Input      RunInput   `json:"input"`
	Limits     RunLimits  `json:"limits"`
	Phase      Phase      `json:"phase"`
	Result     *Result    `json:"result,omitempty"`
	Usage      *Usage     `json:"usage,omitempty"`
	Provenance *Evidence  `json:"provenance,omitempty"`
	Outputs    RunOutputs `json:"outputs"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Event kinds in events.jsonl.
const (
	EventMessage = "message"
	EventTool    = "tool"
	EventStatus  = "status"
	// EventDenied records a request the permission policy refused.
	EventDenied = "denied"
)

// Event is one line of events.jsonl. It must never carry a secret.
type Event struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

// ValidKind reports whether the event kind is known.
func (e Event) ValidKind() bool {
	return e.Kind == EventMessage || e.Kind == EventTool || e.Kind == EventStatus || e.Kind == EventDenied
}
