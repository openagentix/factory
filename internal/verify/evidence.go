package verify

import (
	"fmt"

	"github.com/openagentix/factory/internal/api"
)

// EvidenceInput is everything the verify stage knows when it builds the
// bundle. The caller supplies the run facts, and the parts come from the
// other functions in this package.
type EvidenceInput struct {
	Run     string
	Task    *api.Task
	Factory *api.Factory

	Goal, Ticket, Attempt int
	Mode                  string
	Method, MethodUsed    string
	Trigger               api.TriggerRef
	// Base and Commit are the commit ids to show.
	Base, Commit string

	// Changes is the change from ChangedFiles. Use an empty set for a run
	// that made no change.
	Changes *ChangeSet
	// Result is the worker's parsed result.json.
	Result  *api.Result
	Denials []api.Denial
	// Checks are the outcomes of RunChecks.
	Checks []CheckOutcome
	// TestPatterns replaces DefaultTestPatterns when not nil.
	TestPatterns []string
	// UsageSource is who counted the usage, for example "harness".
	UsageSource string
	Limits      api.RunLimits
}

// Built is the bundle and the facts that later stages and the engineering
// view need and the bundle has no field for.
type Built struct {
	Evidence *api.Evidence
	// Uncovered lists changed files that no claim covers.
	Uncovered []string
}

// BuildEvidence builds the evidence bundle from the parts and validates it.
// It leaves empty what later stages fill: the review, the gate and the
// acceptance states.
//
// Risk is set only when rules 1 and 2 decide it, because they read only
// paths and test files and come first. For every other change, risk stays
// empty until the review ends and ComputeRisk can read all its inputs.
func BuildEvidence(in EvidenceInput) (*Built, error) {
	b, err := buildEvidence(in)
	if err != nil {
		// A zero value, so a caller that skips the error check does not crash.
		return &Built{Evidence: &api.Evidence{}, Uncovered: []string{}}, err
	}
	return b, nil
}

func buildEvidence(in EvidenceInput) (*Built, error) {
	if in.Result == nil || in.Task == nil || in.Factory == nil || in.Changes == nil {
		return nil, fmt.Errorf("evidence: result, task, factory and changes are required")
	}
	cs := in.Changes
	paths := cs.Paths()
	tests, err := ClassifyTestFiles(cs, in.TestPatterns)
	if err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	protected, err := CheckProtected(in.Factory, in.Task, cs)
	if err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	ev := &api.Evidence{
		SchemaVersion:    api.SchemaVersion,
		Run:              in.Run,
		Task:             in.Task.Metadata.Name,
		Goal:             in.Goal,
		Ticket:           in.Ticket,
		Attempt:          in.Attempt,
		Mode:             in.Mode,
		Method:           in.Method,
		MethodUsed:       in.MethodUsed,
		Trigger:          in.Trigger,
		Worker:           in.Result.Harness,
		Base:             in.Base,
		Commit:           in.Commit,
		Diff:             api.DiffSummary{Files: len(paths), Added: cs.Added, Removed: cs.Removed, Paths: nonNil(paths)},
		Claims:           in.Result.Claims,
		Rulings:          in.Result.Rulings,
		Denials:          in.Denials,
		ProtectedPaths:   protected,
		TestFilesChanged: tests.Changed,
		TestFilesAdded:   tests.Added,
		Acceptance:       []api.AcceptanceEntry{},
		Usage:            in.Result.Usage,
		Limits:           in.Limits,
	}
	if in.UsageSource != "" {
		ev.Usage.Source = in.UsageSource
	}
	if ev.Claims == nil {
		ev.Claims = []api.Claim{}
	}
	if ev.Rulings == nil {
		ev.Rulings = []api.Ruling{}
	}
	if ev.Denials == nil {
		ev.Denials = []api.Denial{}
	}
	ev.Checks = []api.CheckResult{}
	for _, c := range in.Checks {
		ev.Checks = append(ev.Checks, c.Result)
	}
	if len(paths) > 0 {
		r, ok, err := PreliminaryRisk(RiskInput{
			Changes:              cs,
			RiskPaths:            in.Factory.Spec.RiskPaths,
			ExistingTestsChanged: tests.Changed,
		})
		if err != nil {
			return nil, fmt.Errorf("evidence: %w", err)
		}
		if ok {
			ev.Risk = r
		}
	}
	if err := ev.Validate(); err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	return &Built{Evidence: ev, Uncovered: UncoveredFiles(cs, in.Result.Claims)}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
