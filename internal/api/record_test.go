package api

import (
	"encoding/json"
	"testing"
	"time"
)

// The sample is the evidence bundle from decision 0001, decision 6.
const evidenceJSON = `{
  "schemaVersion": 1,
  "run": "r_01H", "task": "implement", "trigger": { "kind": "issue", "ref": "#42" },
  "worker": { "name": "claude-code", "version": "2.1.326", "model": "m" },
  "diff": { "files": 3, "added": 84, "removed": 12, "paths": ["a.go"] },
  "claims": ["c"],
  "protectedPaths": { "touched": [], "ok": true },
  "testFilesChanged": ["internal/x/x_test.go"],
  "checks": [ { "name": "test", "exitCode": 0, "durationSeconds": 41, "ranBy": "orchestrator" } ],
  "usage": { "costUSD": 0.42, "source": "harness", "enforced": false },
  "limits": { "deadline": "30m", "maxCostUSD": 5 },
  "risk": { "level": "low", "reasons": ["test files changed"] }
}`

func TestEvidenceJSON(t *testing.T) {
	var e Evidence
	if err := decodeJSONStrict([]byte(evidenceJSON), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Run != "r_01H" || e.Trigger.Ref != "#42" || e.Diff.Added != 84 || !e.ProtectedPaths.OK {
		t.Errorf("unexpected: %+v", e)
	}
	if e.Checks[0].RanBy != "orchestrator" || e.Checks[0].DurationSeconds != 41 {
		t.Errorf("checks: %+v", e.Checks)
	}
	if e.Risk.Level != RiskLow || e.Limits.MaxCostUSD != 5 {
		t.Errorf("risk or limits: %+v", e)
	}
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var back Evidence
	if err := decodeJSONStrict(out, &back); err != nil {
		t.Fatalf("round trip: %v", err)
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
		ID: "r_1", Task: "implement", Attempt: 1,
		Trigger:   TriggerRef{Kind: "issue", Ref: "#42"},
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
	for _, k := range []string{"id", "task", "attempt", "trigger", "input", "limits", "phase", "outputs", "createdAt", "startedAt"} {
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
	}{{EventMessage, true}, {EventTool, true}, {EventStatus, true}, {"other", false}, {"", false}}
	for _, tt := range tests {
		if got := (Event{Kind: tt.kind}).ValidKind(); got != tt.ok {
			t.Errorf("ValidKind(%q) = %v", tt.kind, got)
		}
	}
}
