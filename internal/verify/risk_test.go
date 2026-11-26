package verify

import (
	"reflect"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

var testRiskPaths = api.RiskPaths{
	Contract:     []string{"api/**"},
	Security:     []string{"internal/auth/**"},
	Dependencies: []string{"go.mod", "go.sum"},
	Documents:    []string{"docs/**", "*.md"},
}

// goodInput meets every condition of rule 3.
func goodInput(paths ...string) RiskInput {
	return RiskInput{
		Changes:            modifiedWithLines(10, paths...),
		RiskPaths:          testRiskPaths,
		SizeLimit:          100,
		ReviewOtherHarness: true,
		CriteriaServed:     2,
		CriteriaWorking:    2,
	}
}

func modifiedWithLines(added int, paths ...string) *ChangeSet {
	cs := modified(paths...)
	cs.Added = added
	return cs
}

func TestComputeRisk(t *testing.T) {
	with := func(in RiskInput, f func(*RiskInput)) RiskInput { f(&in); return in }
	tests := []struct {
		name    string
		in      RiskInput
		level   string
		reasons []string
	}{
		// rule 1
		{"contract path", goodInput("api/v1/spec.yaml"), api.RiskHigh, []string{"contract path changed: api/v1/spec.yaml"}},
		{"security path", goodInput("internal/auth/login.go"), api.RiskHigh, []string{"security path changed: internal/auth/login.go"}},
		{"dependency path", goodInput("go.mod"), api.RiskHigh, []string{"dependencies path changed: go.mod"}},
		{"rename out of a risk path", with(goodInput(), func(in *RiskInput) {
			in.Changes = &ChangeSet{Changes: []Change{renamed("api/a.yaml", "misc/a.yaml")}}
		}), api.RiskHigh, []string{"contract path changed: api/a.yaml"}},
		{"existing test changed", with(goodInput("a.go"), func(in *RiskInput) { in.ExistingTestsChanged = []string{"a_test.go"} }),
			api.RiskHigh, []string{"existing test file changed or deleted: a_test.go"}},
		{"both reasons", with(goodInput("go.sum"), func(in *RiskInput) { in.ExistingTestsChanged = []string{"a_test.go"} }),
			api.RiskHigh, []string{"dependencies path changed: go.sum", "existing test file changed or deleted: a_test.go"}},
		// rule 1 comes before rule 2
		{"docs plus a risk path", goodInput("docs/a.md", "go.mod"), api.RiskHigh, []string{"dependencies path changed: go.mod"}},
		{"docs plus a changed test", with(goodInput("docs/a.md"), func(in *RiskInput) { in.ExistingTestsChanged = []string{"x_test.go"} }),
			api.RiskHigh, []string{"existing test file changed or deleted: x_test.go"}},
		// rule 2
		{"only documents", with(goodInput("docs/a.md", "README.md"), func(in *RiskInput) {
			in.ReviewOtherHarness, in.CriteriaServed, in.CriteriaWorking = false, 0, 0
		}), api.RiskLow, []string{"only document paths changed"}},
		{"documents and code", with(goodInput("docs/a.md", "main.go"), func(in *RiskInput) { in.ReviewOtherHarness = false }),
			api.RiskMedium, []string{"review is not from a different harness"}},
		{"nested md is not a document", with(goodInput("sub/README.md"), func(in *RiskInput) { in.ReviewOtherHarness = false }),
			api.RiskMedium, []string{"review is not from a different harness"}},
		// rule 2 comes before rule 4
		{"documents with nothing else known", RiskInput{Changes: modified("docs/a.md"), RiskPaths: testRiskPaths},
			api.RiskLow, []string{"only document paths changed"}},
		// rule 3
		{"all conditions", goodInput("main.go"), api.RiskLow, []string{
			"review came from a different harness", "every served criterion is working", "every changed file is covered by a claim", "change is within the size limit of 100 lines"}},
		{"size at the limit", with(goodInput("main.go"), func(in *RiskInput) { in.Changes = modifiedWithLines(60, "main.go"); in.Changes.Removed = 40 }),
			api.RiskLow, nil},
		// rule 4: each condition of rule 3 broken alone
		{"same harness", with(goodInput("main.go"), func(in *RiskInput) { in.ReviewOtherHarness = false }),
			api.RiskMedium, []string{"review is not from a different harness"}},
		{"no criterion served", with(goodInput("main.go"), func(in *RiskInput) { in.CriteriaServed, in.CriteriaWorking = 0, 0 }),
			api.RiskMedium, []string{"the ticket serves no criterion"}},
		{"criterion not working", with(goodInput("main.go"), func(in *RiskInput) { in.CriteriaWorking = 1 }),
			api.RiskMedium, []string{"1 of 2 served criteria are not working yet or could not be confirmed"}},
		{"file not covered", with(goodInput("main.go", "util.go"), func(in *RiskInput) { in.Uncovered = []string{"util.go"} }),
			api.RiskMedium, []string{"1 changed file is not covered by a claim"}},
		{"over the size limit", with(goodInput("main.go"), func(in *RiskInput) { in.Changes = modifiedWithLines(101, "main.go") }),
			api.RiskMedium, []string{"change is 101 lines, over the size limit of 100"}},
		{"no size limit set", with(goodInput("main.go"), func(in *RiskInput) { in.SizeLimit = 0 }),
			api.RiskMedium, []string{"no size limit is set"}},
		{"several broken", with(goodInput("main.go"), func(in *RiskInput) {
			in.ReviewOtherHarness = false
			in.CriteriaServed, in.CriteriaWorking = 0, 0
		}), api.RiskMedium, []string{"review is not from a different harness", "the ticket serves no criterion"}},
		{"new test files do not raise risk", goodInput("main.go", "main_test.go"), api.RiskLow, nil},
		{"empty change", RiskInput{Changes: &ChangeSet{}, RiskPaths: testRiskPaths, SizeLimit: 10},
			api.RiskMedium, []string{"review is not from a different harness", "the ticket serves no criterion"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComputeRisk(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Level != tt.level {
				t.Fatalf("level = %q, want %q (reasons %v)", got.Level, tt.level, got.Reasons)
			}
			if tt.reasons != nil && !reflect.DeepEqual(got.Reasons, tt.reasons) {
				t.Errorf("reasons = %q\nwant %q", got.Reasons, tt.reasons)
			}
			if len(got.Reasons) == 0 {
				t.Error("a risk always has reasons")
			}
		})
	}
}

func TestComputeRisk_Order(t *testing.T) {
	// A change that fits rules 1, 2, 3 and 4 gets rule 1.
	in := goodInput("go.mod")
	in.RiskPaths.Documents = append(in.RiskPaths.Documents, "go.mod")
	if got, _ := ComputeRisk(in); got.Level != api.RiskHigh {
		t.Errorf("rule 1 must win over rule 2: %+v", got)
	}
	// Rule 2 wins over rule 3 and 4.
	in2 := RiskInput{Changes: modified("docs/x.md"), RiskPaths: testRiskPaths}
	if got, _ := ComputeRisk(in2); got.Level != api.RiskLow || got.Reasons[0] != "only document paths changed" {
		t.Errorf("rule 2 must win over rule 4: %+v", got)
	}
}

func TestPreliminaryRisk(t *testing.T) {
	// Rules 1 and 2 need only paths, so they can be settled in verify.
	tests := []struct {
		name  string
		cs    *ChangeSet
		tests []string
		set   bool
		level string
	}{
		{"high by path", modified("go.mod"), nil, true, api.RiskHigh},
		{"high by test", modified("a.go"), []string{"a_test.go"}, true, api.RiskHigh},
		{"low by documents", modified("docs/a.md"), nil, true, api.RiskLow},
		{"left for the later stage", modified("a.go"), nil, false, ""},
		{"no change", &ChangeSet{}, nil, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok, err := PreliminaryRisk(RiskInput{Changes: tt.cs, RiskPaths: testRiskPaths, ExistingTestsChanged: tt.tests})
			if err != nil {
				t.Fatal(err)
			}
			if ok != tt.set || r.Level != tt.level {
				t.Errorf("got %+v ok=%v", r, ok)
			}
		})
	}
}

func TestComputeRisk_BadPatternFailsClosed(t *testing.T) {
	for _, set := range []func(*api.RiskPaths){
		func(r *api.RiskPaths) { r.Contract = []string{"[bad"} },
		func(r *api.RiskPaths) { r.Security = []string{"ok", "[bad"} },
		func(r *api.RiskPaths) { r.Dependencies = []string{"[bad"} },
		func(r *api.RiskPaths) { r.Documents = []string{"[bad"} },
	} {
		in := goodInput("main.go")
		set(&in.RiskPaths)
		if _, err := ComputeRisk(in); err == nil || !strings.Contains(err.Error(), "[bad") {
			t.Errorf("ComputeRisk error = %v, want one naming the pattern", err)
		}
		if _, _, err := PreliminaryRisk(in); err == nil {
			t.Error("PreliminaryRisk must fail closed too")
		}
	}
}
