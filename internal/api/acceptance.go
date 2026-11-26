package api

import "fmt"

// ManifestCriterion is one acceptance criterion in the manifest.
type ManifestCriterion struct {
	ID int `json:"id"`
	// Text is the person's wording.
	Text string `json:"text"`
	// Run is the check command. Empty means no check could be written.
	Run string `json:"run"`
	// Reason says why no check could be written. Set only when Run is empty.
	Reason string `json:"reason,omitempty"`
}

// HasCheck reports whether a check was written for the criterion.
func (c ManifestCriterion) HasCheck() bool { return !blank(c.Run) }

// Manifest is the acceptance manifest of one goal, written by the acceptance
// task under acceptance/<goal number>/.
type Manifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Goal          int                 `json:"goal"`
	Criteria      []ManifestCriterion `json:"criteria"`
}

// ParseManifest reads an acceptance manifest. Unknown fields are an error.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := decodeJSONStrict(data, &m); err != nil {
		return nil, fmt.Errorf("acceptance manifest: %w", err)
	}
	var p problems
	if m.SchemaVersion != SchemaVersion {
		p.add("schemaVersion", "must be %d", SchemaVersion)
	}
	if m.Goal < 1 {
		p.add("goal", "must be the number of the goal issue, 1 or more")
	}
	if len(m.Criteria) == 0 {
		p.add("criteria", "must hold at least one criterion")
	}
	seen := map[int]bool{}
	for i, c := range m.Criteria {
		base := fmt.Sprintf("criteria[%d]", i)
		switch {
		case c.ID < 1:
			p.add(base+".id", "must be 1 or more")
		case seen[c.ID]:
			p.add(base+".id", "duplicate criterion %d", c.ID)
		}
		seen[c.ID] = true
		if blank(c.Text) {
			p.add(base+".text", "must be set")
		}
		switch {
		case c.HasCheck() && c.Reason != "":
			p.add(base+".reason", "must be empty when run is set")
		case !c.HasCheck() && blank(c.Reason):
			p.add(base+".reason", "must say why no check could be written when run is empty")
		}
	}
	if err := p.err(); err != nil {
		return nil, fmt.Errorf("acceptance manifest: %w", err)
	}
	return &m, nil
}

// AcceptanceState is the state of one criterion. There are three and no
// others. Only the orchestrator computes it, from the checks.
type AcceptanceState string

// The three acceptance states.
const (
	StateWorking         AcceptanceState = "working"
	StateNotWorking      AcceptanceState = "not_working"
	StateCouldNotConfirm AcceptanceState = "could_not_confirm"
)

// Valid reports whether s is one of the three states.
func (s AcceptanceState) Valid() bool {
	return s == StateWorking || s == StateNotWorking || s == StateCouldNotConfirm
}

// Name is the plain wording shown to a person.
func (s AcceptanceState) Name() string {
	switch s {
	case StateWorking:
		return "working"
	case StateNotWorking:
		return "not working yet"
	case StateCouldNotConfirm:
		return "could not confirm"
	}
	return string(s)
}

// CheckOutcome is what happened when the orchestrator ran a criterion's check.
type CheckOutcome string

// Check outcomes.
const (
	CheckPassed CheckOutcome = "passed"
	CheckFailed CheckOutcome = "failed"
	CheckNotRun CheckOutcome = "not_run"
)

// ComputeState gives the state of one criterion from its manifest entry and
// the outcome of its check. No opinion goes in. A criterion without a check
// is never working. An outcome that is not known is treated as not run.
func ComputeState(entry ManifestCriterion, outcome CheckOutcome) AcceptanceState {
	if !entry.HasCheck() {
		return StateCouldNotConfirm
	}
	switch outcome {
	case CheckPassed:
		return StateWorking
	case CheckFailed:
		return StateNotWorking
	}
	return StateCouldNotConfirm
}
