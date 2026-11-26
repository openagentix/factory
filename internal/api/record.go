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

// Evidence is the evidence bundle. The orchestrator builds it, not the worker.
type Evidence struct {
	SchemaVersion    int                  `json:"schemaVersion"`
	Run              string               `json:"run"`
	Task             string               `json:"task"`
	Trigger          TriggerRef           `json:"trigger"`
	Worker           HarnessInfo          `json:"worker"`
	Diff             DiffSummary          `json:"diff"`
	Claims           []string             `json:"claims"`
	ProtectedPaths   ProtectedPathsReport `json:"protectedPaths"`
	TestFilesChanged []string             `json:"testFilesChanged"`
	Checks           []CheckResult        `json:"checks"`
	Usage            Usage                `json:"usage"`
	Limits           RunLimits            `json:"limits"`
	Risk             Risk                 `json:"risk"`
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
)

// Event is one line of events.jsonl. It must never carry a secret.
type Event struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

// ValidKind reports whether the event kind is known.
func (e Event) ValidKind() bool {
	return e.Kind == EventMessage || e.Kind == EventTool || e.Kind == EventStatus
}
