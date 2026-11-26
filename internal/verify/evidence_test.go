package verify

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func testResult() *api.Result {
	return &api.Result{
		SchemaVersion: api.SchemaVersion,
		Status:        api.StatusSucceeded,
		Summary:       "Customers can move a booking to another day.",
		Claims: []api.Claim{{Text: "Added a move endpoint with a test",
			Files: []string{"internal/booking/move.go", "internal/booking/move_test.go"}}},
		Rulings: []api.Ruling{{Decision: "Kept the old endpoint as an alias", Why: "Two callers still use it", CostIfWrong: "One extra route to remove later"}},
		Usage:   api.Usage{Billing: api.BillingPlan, InputTokens: 12000, OutputTokens: 3400},
		Harness: api.HarnessInfo{Name: "claude-code", Version: "2.1.326"},
	}
}

func testFactoryFile() *api.Factory {
	f := testFactory()
	f.Spec.RiskPaths = testRiskPaths
	return f
}

func baseInput(cs *ChangeSet) EvidenceInput {
	return EvidenceInput{
		Run: "r_01H8Z", Task: taskNamed("implement"), Factory: testFactoryFile(),
		Goal: 40, Ticket: 42, Attempt: 1, Mode: api.ModeUnattended,
		Method: "builtin", MethodUsed: "builtin",
		Trigger: api.TriggerRef{Kind: "issueLabeled", Ref: "#42"},
		Base:    "9f2c1e7", Commit: "a1b2c3d",
		Changes: cs, Result: testResult(),
		Checks: []CheckOutcome{{Result: api.CheckResult{Name: "test", ExitCode: 0, DurationSeconds: 41, RanBy: RanByOrchestrator}}},
		Limits: api.RunLimits{Deadline: "30m", MaxCostUSD: 5},
	}
}

func TestBuildEvidence(t *testing.T) {
	cs := &ChangeSet{
		Changes: []Change{
			{Path: "internal/booking/move.go", Status: StatusAdded, Added: 70},
			{Path: "internal/booking/move_test.go", Status: StatusAdded, Added: 14},
			{Path: "internal/booking/old.go", Status: StatusModified, Added: 0, Removed: 12},
		},
		Added: 84, Removed: 12,
	}
	b, err := BuildEvidence(baseInput(cs))
	if err != nil {
		t.Fatal(err)
	}
	ev := b.Evidence
	if err := ev.Validate(); err != nil {
		t.Fatalf("evidence does not validate: %v", err)
	}
	wantPaths := []string{"internal/booking/move.go", "internal/booking/move_test.go", "internal/booking/old.go"}
	if !reflect.DeepEqual(ev.Diff, api.DiffSummary{Files: 3, Added: 84, Removed: 12, Paths: wantPaths}) {
		t.Errorf("diff = %+v", ev.Diff)
	}
	if !ev.ProtectedPaths.OK || len(ev.ProtectedPaths.Touched) != 0 {
		t.Errorf("protected = %+v", ev.ProtectedPaths)
	}
	if !reflect.DeepEqual(ev.TestFilesAdded, []string{"internal/booking/move_test.go"}) || len(ev.TestFilesChanged) != 0 {
		t.Errorf("tests: added %v changed %v", ev.TestFilesAdded, ev.TestFilesChanged)
	}
	if !reflect.DeepEqual(b.Uncovered, []string{"internal/booking/old.go"}) {
		t.Errorf("uncovered = %v", b.Uncovered)
	}
	if ev.Worker.Name != "claude-code" || len(ev.Claims) != 1 || len(ev.Rulings) != 1 {
		t.Errorf("worker parts: %+v", ev)
	}
	if len(ev.Checks) != 1 || ev.Checks[0].Name != "test" {
		t.Errorf("checks = %+v", ev.Checks)
	}
	if ev.Usage.Billing != api.BillingPlan || ev.Usage.InputTokens != 12000 {
		t.Errorf("usage = %+v", ev.Usage)
	}
	// Stages after verify fill these.
	if ev.Review != nil || ev.Gate != "" || len(ev.Acceptance) != 0 || ev.Acceptance == nil {
		t.Errorf("later fields must be empty: review=%v gate=%q acceptance=%v", ev.Review, ev.Gate, ev.Acceptance)
	}
	if ev.Risk.Level != "" {
		t.Errorf("rules 3 and 4 need the review, so risk is left empty: %+v", ev.Risk)
	}
}

func TestBuildEvidence_RiskSettledByPaths(t *testing.T) {
	in := baseInput(modified("go.mod", "main.go"))
	b, err := BuildEvidence(in)
	if err != nil {
		t.Fatal(err)
	}
	if b.Evidence.Risk.Level != api.RiskHigh {
		t.Errorf("risk = %+v", b.Evidence.Risk)
	}
	in = baseInput(modified("main.go"))
	in.Changes.Changes = append(in.Changes.Changes, Change{Path: "main_test.go", Status: StatusModified})
	b, _ = BuildEvidence(in)
	if b.Evidence.Risk.Level != api.RiskHigh || !reflect.DeepEqual(b.Evidence.TestFilesChanged, []string{"main_test.go"}) {
		t.Errorf("changed existing test must give high risk: %+v", b.Evidence)
	}
}

func TestBuildEvidence_ProtectedTouch(t *testing.T) {
	b, err := BuildEvidence(baseInput(modified(".github/workflows/ci.yml", "a.go")))
	if err != nil {
		t.Fatal(err)
	}
	if b.Evidence.ProtectedPaths.OK || !reflect.DeepEqual(b.Evidence.ProtectedPaths.Touched, []string{".github/workflows/ci.yml"}) {
		t.Errorf("protected = %+v", b.Evidence.ProtectedPaths)
	}
	if b.Evidence.Validate() != nil {
		t.Error("must still validate")
	}
}

func TestBuildEvidence_NoChange(t *testing.T) {
	in := baseInput(&ChangeSet{})
	in.Result.Status = api.StatusNoChange
	in.Checks = nil
	b, err := BuildEvidence(in)
	if err != nil {
		t.Fatal(err)
	}
	ev := b.Evidence
	if ev.Diff.Files != 0 || len(ev.Diff.Paths) != 0 || ev.Risk.Level != "" || len(ev.Checks) != 0 {
		t.Errorf("no change must give an empty diff, risk and checks: %+v", ev)
	}
}

func TestBuildEvidence_Invalid(t *testing.T) {
	in := baseInput(modified("a.go"))
	in.Run = ""
	if _, err := BuildEvidence(in); err == nil {
		t.Error("an invalid bundle must be an error")
	}
	in = baseInput(modified("a.go"))
	in.Result = nil
	if _, err := BuildEvidence(in); err == nil {
		t.Error("a missing result must be an error")
	}
}

func TestBuildEvidence_FromGit(t *testing.T) {
	r := newTestRepo(t)
	r.write("main.go", "package a\n")
	r.write("main_test.go", "package a\n")
	base := r.commit("base")
	r.write("main.go", "package a\n// x\n")
	r.write("go.mod", "module x\n")
	r.write("new_test.go", "package a\n")
	head := r.commit("head")

	cs, err := ChangedFiles(context.Background(), r.dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := RunChecks(context.Background(), r.dir, head,
		[]api.Check{{Name: "t", Run: "test -f main.go"}}, CheckOptions{Deadline: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	in := baseInput(cs)
	in.Base, in.Commit, in.Checks = base[:7], head[:7], checks
	in.Result.Claims = []api.Claim{{Text: "x", Files: []string{"main.go"}}}
	b, err := BuildEvidence(in)
	if err != nil {
		t.Fatal(err)
	}
	ev := b.Evidence
	if ev.Risk.Level != api.RiskHigh {
		t.Errorf("go.mod is a dependency path: %+v", ev.Risk)
	}
	if !reflect.DeepEqual(ev.TestFilesAdded, []string{"new_test.go"}) {
		t.Errorf("added tests = %v", ev.TestFilesAdded)
	}
	if !reflect.DeepEqual(b.Uncovered, []string{"go.mod", "new_test.go"}) {
		t.Errorf("uncovered = %v", b.Uncovered)
	}
	if ev.Checks[0].ExitCode != 0 {
		t.Errorf("checks = %+v", ev.Checks)
	}
}
