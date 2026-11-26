package api

import (
	"encoding/json"
	"testing"
	"time"
)

// The sample is the evidence bundle from decision 0001, "The run record and
// the evidence bundle".
const evidenceJSON = `{
  "schemaVersion": 1,
  "run": "r_01H8Z", "task": "implement", "goal": 40, "ticket": 42, "attempt": 1, "mode": "unattended",
  "method": "plugin:superpowers", "methodUsed": "builtin",
  "trigger": { "kind": "issueLabeled", "ref": "#42" },
  "worker": { "name": "claude-code", "version": "2.1.326", "model": "" },
  "base": "9f2c1e7", "commit": "a1b2c3d",
  "diff": { "files": 2, "added": 84, "removed": 12, "paths": ["internal/booking/move.go", "internal/booking/move_test.go"] },
  "claims": [ { "text": "Added a move endpoint with a test", "files": ["internal/booking/move.go", "internal/booking/move_test.go"] } ],
  "rulings": [ { "decision": "Kept the old endpoint as an alias", "why": "Two callers still use it", "costIfWrong": "One extra route to remove later" } ],
  "denials": [ { "request": "curl https://example.com", "reason": "command not in policy" } ],
  "protectedPaths": { "touched": [], "ok": true },
  "testFilesChanged": [], "testFilesAdded": ["internal/booking/move_test.go"],
  "checks": [ { "name": "test", "exitCode": 0, "durationSeconds": 41, "ranBy": "orchestrator" } ],
  "acceptance": [ { "id": 1, "state": "working", "before": "not_working", "check": "go test ./acceptance/40 -run TestMoveBooking" } ],
  "review": { "run": "r_01H9A", "harness": "codex", "verdict": "agrees", "singleOpinion": false },
  "risk": { "level": "low", "reasons": ["review from the other harness", "served criteria working"] },
  "gate": "passed",
  "usage": { "billing": "plan", "inputTokens": 12000, "outputTokens": 3400, "source": "harness", "enforced": false },
  "limits": { "deadline": "30m", "maxCostUSD": 5 }
}`

func TestEvidenceJSON(t *testing.T) {
	var e Evidence
	if err := decodeJSONStrict([]byte(evidenceJSON), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Run != "r_01H8Z" || e.Goal != 40 || e.Ticket != 42 || e.Attempt != 1 || e.Mode != ModeUnattended {
		t.Errorf("references: %+v", e)
	}
	if e.Method != "plugin:superpowers" || e.MethodUsed != MethodBuiltin || e.Base != "9f2c1e7" || e.Commit != "a1b2c3d" {
		t.Errorf("method or commits: %+v", e)
	}
	if e.Trigger.Ref != "#42" || e.Diff.Added != 84 || !e.ProtectedPaths.OK {
		t.Errorf("unexpected: %+v", e)
	}
	if e.Checks[0].RanBy != "orchestrator" || e.Checks[0].DurationSeconds != 41 {
		t.Errorf("checks: %+v", e.Checks)
	}
	if e.Claims[0].Files[1] != "internal/booking/move_test.go" || e.Rulings[0].CostIfWrong == "" {
		t.Errorf("claims or rulings: %+v %+v", e.Claims, e.Rulings)
	}
	if e.Denials[0].Request != "curl https://example.com" || e.Denials[0].Reason == "" {
		t.Errorf("denials: %+v", e.Denials)
	}
	if len(e.TestFilesChanged) != 0 || len(e.TestFilesAdded) != 1 {
		t.Errorf("test files: %v %v", e.TestFilesChanged, e.TestFilesAdded)
	}
	if a := e.Acceptance[0]; a.ID != 1 || a.State != StateWorking || a.Before != StateNotWorking || a.Check == "" {
		t.Errorf("acceptance: %+v", a)
	}
	if e.Review == nil || e.Review.Harness != "codex" || e.Review.Verdict != VerdictAgrees || e.Review.SingleOpinion {
		t.Errorf("review: %+v", e.Review)
	}
	if e.Risk.Level != RiskLow || len(e.Risk.Reasons) != 2 || e.Gate != GatePassed {
		t.Errorf("risk or gate: %+v", e)
	}
	if e.Usage.Billing != BillingPlan || e.Usage.Source != "harness" || e.Limits.MaxCostUSD != 5 {
		t.Errorf("usage or limits: %+v", e)
	}
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var back Evidence
	if err := decodeJSONStrict(out, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if back.Review == nil || back.Acceptance[0].Before != StateNotWorking {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestEvidenceWithoutReview(t *testing.T) {
	out, err := json.Marshal(Evidence{SchemaVersion: 1, Run: "r"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["review"]; ok {
		t.Errorf("review must be omitted when unset: %s", out)
	}
}

func TestModeAndGateValues(t *testing.T) {
	if ModeUnattended != "unattended" || ModeWheel != "wheel" {
		t.Error("mode values")
	}
	for g, s := range map[string]string{GatePending: "pending", GatePassed: "passed", GateFailed: "failed", GateException: "exception"} {
		if g != s {
			t.Errorf("gate %q, want %q", g, s)
		}
	}
}

func TestPhaseIsTerminal(t *testing.T) {
	tests := []struct {
		phase Phase
		want  bool
	}{
		{PhaseQueued, false}, {PhaseHeld, false}, {PhaseWorking, false},
		{PhaseVerifying, false}, {PhasePublishing, false},
		{PhaseSucceeded, true}, {PhaseFailed, true}, {PhaseNeedsHuman, true}, {PhaseCancelled, true},
		{Phase("bogus"), false},
	}
	for _, tt := range tests {
		if got := tt.phase.IsTerminal(); got != tt.want {
			t.Errorf("%s.IsTerminal() = %v, want %v", tt.phase, got, tt.want)
		}
	}
}

func TestPhaseValues(t *testing.T) {
	want := map[Phase]string{
		PhaseQueued: "queued", PhaseHeld: "held", PhaseWorking: "working",
		PhaseVerifying: "verifying", PhasePublishing: "publishing",
		PhaseSucceeded: "succeeded", PhaseFailed: "failed",
		PhaseNeedsHuman: "needs_human", PhaseCancelled: "cancelled",
	}
	for p, s := range want {
		if string(p) != s {
			t.Errorf("phase %q, want %q", p, s)
		}
	}
}

func TestRunJSON(t *testing.T) {
	created := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	started := created.Add(time.Minute)
	r := Run{
		ID: "r_1", Task: "implement", Attempt: 1, Goal: 40, Ticket: 42,
		Mode: ModeWheel, Method: "plugin:x", MethodUsed: MethodBuiltin,
		Trigger:   TriggerRef{Kind: "issueLabeled", Ref: "#42"},
		Input:     RunInput{PromptHash: "sha256:abc"},
		Limits:    RunLimits{Deadline: "30m", MaxCostUSD: 5},
		Phase:     PhaseWorking,
		Outputs:   RunOutputs{Branch: "factory/42"},
		CreatedAt: created, StartedAt: &started,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "task", "attempt", "goal", "ticket", "mode", "method", "methodUsed", "trigger", "input", "limits", "phase", "outputs", "createdAt", "startedAt"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, data)
		}
	}
	for _, k := range []string{"finishedAt", "result", "provenance", "usage"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q must be omitted when unset", k)
		}
	}
	var back Run
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Goal != 40 || back.Ticket != 42 || back.Mode != ModeWheel || back.MethodUsed != MethodBuiltin {
		t.Errorf("round trip lost references: %+v", back)
	}
	if back.Phase != PhaseWorking || !back.CreatedAt.Equal(created) || back.StartedAt == nil {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestEvent(t *testing.T) {
	line := `{"time":"2026-11-26T10:00:00Z","kind":"tool","text":"ran go test"}`
	var e Event
	if err := decodeJSONStrict([]byte(line), &e); err != nil {
		t.Fatal(err)
	}
	if e.Kind != EventTool || e.Text != "ran go test" || e.Time.Year() != 2026 {
		t.Errorf("unexpected: %+v", e)
	}
	tests := []struct {
		kind string
		ok   bool
	}{{EventMessage, true}, {EventTool, true}, {EventStatus, true}, {EventDenied, true}, {"other", false}, {"", false}}
	for _, tt := range tests {
		if got := (Event{Kind: tt.kind}).ValidKind(); got != tt.ok {
			t.Errorf("ValidKind(%q) = %v", tt.kind, got)
		}
	}
}

func TestCanTransition(t *testing.T) {
	all := []Phase{PhaseQueued, PhaseHeld, PhaseWorking, PhaseVerifying, PhasePublishing,
		PhaseSucceeded, PhaseFailed, PhaseNeedsHuman, PhaseCancelled}
	allowed := map[Phase][]Phase{
		PhaseQueued:     {PhaseHeld, PhaseWorking, PhaseFailed, PhaseCancelled},
		PhaseHeld:       {PhaseWorking, PhaseFailed, PhaseCancelled},
		PhaseWorking:    {PhaseVerifying, PhaseFailed, PhaseNeedsHuman, PhaseCancelled},
		PhaseVerifying:  {PhasePublishing, PhaseFailed, PhaseNeedsHuman, PhaseCancelled},
		PhasePublishing: {PhaseSucceeded, PhaseFailed, PhaseNeedsHuman, PhaseCancelled},
	}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, a := range allowed[from] {
				if a == to {
					want = true
				}
			}
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	if CanTransition(Phase("bogus"), PhaseWorking) || CanTransition(PhaseQueued, Phase("bogus")) {
		t.Error("an unknown phase must never be allowed")
	}
}

func TestPhaseValid(t *testing.T) {
	if !PhaseHeld.Valid() || Phase("bogus").Valid() || Phase("").Valid() {
		t.Error("Valid wrong")
	}
}
