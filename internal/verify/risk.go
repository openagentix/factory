package verify

import (
	"fmt"
	"sort"

	"github.com/openagentix/factory/internal/api"
)

// RiskInput holds what the risk rules read. No model sets any of it.
//
// The first group is known in the verify stage. The second group comes from
// the review and from the acceptance checks, which run later. A zero value in
// the second group counts against the change: deny by default.
type RiskInput struct {
	Changes   *ChangeSet
	RiskPaths api.RiskPaths
	// ExistingTestsChanged lists existing test files that were changed or
	// deleted.
	ExistingTestsChanged []string
	// SizeLimit is the most lines (added plus removed) that rule 3 accepts.
	// Zero means no limit is set, and rule 3 then never matches. The value
	// comes from the settings.
	SizeLimit int

	// ReviewOtherHarness is true when the review came from a different
	// harness than the builder.
	ReviewOtherHarness bool
	// CriteriaServed is how many criteria the ticket serves. CriteriaWorking
	// is how many of them are working with the change.
	CriteriaServed, CriteriaWorking int
	// Uncovered lists changed files that no claim covers.
	Uncovered []string
}

// riskKinds lists the risk path groups of rule 1, in a fixed order.
func (in RiskInput) riskKinds() []struct {
	name     string
	patterns []string
} {
	return []struct {
		name     string
		patterns []string
	}{
		{"contract", in.RiskPaths.Contract},
		{"security", in.RiskPaths.Security},
		{"dependencies", in.RiskPaths.Dependencies},
	}
}

// ruleHigh is rule 1.
func ruleHigh(in RiskInput) ([]string, bool) {
	var reasons []string
	paths := in.Changes.Paths()
	for _, k := range in.riskKinds() {
		for _, p := range paths {
			if matchAny(k.patterns, p, Match) {
				reasons = append(reasons, fmt.Sprintf("%s path changed: %s", k.name, p))
			}
		}
	}
	tests := append([]string{}, in.ExistingTestsChanged...)
	sort.Strings(tests)
	for _, p := range tests {
		reasons = append(reasons, "existing test file changed or deleted: "+p)
	}
	return reasons, len(reasons) > 0
}

// ruleDocuments is rule 2. An empty change matches no rule.
func ruleDocuments(in RiskInput) bool {
	paths := in.Changes.Paths()
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if !matchAny(in.RiskPaths.Documents, p, Match) {
			return false
		}
	}
	return true
}

// PreliminaryRisk applies rules 1 and 2. They read only paths and test
// files, and they come first, so a later stage cannot change their answer.
// It reports false when the change is left to rules 3 and 4, which need the
// review and the acceptance states.
//
// A risk pattern that cannot be parsed is an error that names it, not a
// pattern that matches nothing.
func PreliminaryRisk(in RiskInput) (api.Risk, bool, error) {
	rp := in.RiskPaths
	if err := checkPatterns(rp.Contract, rp.Security, rp.Dependencies, rp.Documents); err != nil {
		return api.Risk{}, false, err
	}
	if reasons, ok := ruleHigh(in); ok {
		return api.Risk{Level: api.RiskHigh, Reasons: reasons}, true, nil
	}
	if ruleDocuments(in) {
		return api.Risk{Level: api.RiskLow, Reasons: []string{"only document paths changed"}}, true, nil
	}
	return api.Risk{}, false, nil
}

// ComputeRisk applies the ordered rules of the decision record. The first
// match wins.
//
//  1. High: a contract, security or dependency path changed, or an existing
//     test file was changed or deleted.
//  2. Low: only document paths changed.
//  3. Low: the review came from a different harness, every served criterion
//     is working, every changed file is covered by a claim, and the change is
//     within the size limit.
//  4. Medium: everything else.
//
// Medium lists why rule 3 did not match.
func ComputeRisk(in RiskInput) (api.Risk, error) {
	r, ok, err := PreliminaryRisk(in)
	if err != nil {
		return api.Risk{}, err
	}
	if ok {
		return r, nil
	}
	size := in.Changes.Added + in.Changes.Removed
	var why []string
	if !in.ReviewOtherHarness {
		why = append(why, "review is not from a different harness")
	}
	switch {
	case in.CriteriaServed == 0:
		why = append(why, "the ticket serves no criterion")
	case in.CriteriaWorking < in.CriteriaServed:
		why = append(why, fmt.Sprintf("%d of %d served criteria are not working yet or could not be confirmed",
			in.CriteriaServed-in.CriteriaWorking, in.CriteriaServed))
	}
	switch n := len(in.Uncovered); {
	case n == 1:
		why = append(why, "1 changed file is not covered by a claim")
	case n > 1:
		why = append(why, fmt.Sprintf("%d changed files are not covered by a claim", n))
	}
	switch {
	case in.SizeLimit <= 0:
		why = append(why, "no size limit is set")
	case size > in.SizeLimit:
		why = append(why, fmt.Sprintf("change is %d lines, over the size limit of %d", size, in.SizeLimit))
	}
	if len(why) > 0 {
		return api.Risk{Level: api.RiskMedium, Reasons: why}, nil
	}
	return api.Risk{Level: api.RiskLow, Reasons: []string{
		"review came from a different harness",
		"every served criterion is working",
		"every changed file is covered by a claim",
		fmt.Sprintf("change is within the size limit of %d lines", in.SizeLimit),
	}}, nil
}
