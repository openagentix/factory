package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validEvidence() *Evidence {
	e, err := ParseEvidence([]byte(evidenceJSON))
	if err != nil {
		panic(err)
	}
	return e
}

func TestParseEvidence_Valid(t *testing.T) {
	e, err := ParseEvidence([]byte(evidenceJSON))
	if err != nil {
		t.Fatalf("ParseEvidence: %v", err)
	}
	if e.Run != "r_01H8Z" {
		t.Errorf("unexpected: %+v", e)
	}
}

func TestParseEvidence_Strict(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"empty", "", "evidence"},
		{"unknown field", strings.Replace(evidenceJSON, `"gate": "passed"`, `"gate": "passed", "x": 1`, 1), `unknown field "x"`},
		{"case variant", strings.Replace(evidenceJSON, `"gate"`, `"Gate"`, 1), `unknown field "Gate"`},
		{"case variant nested", strings.Replace(evidenceJSON, `"singleOpinion"`, `"SingleOpinion"`, 1), `unknown field "SingleOpinion"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEvidence([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEvidenceValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Evidence)
		wantErr string // empty means valid
	}{
		{"valid", func(*Evidence) {}, ""},
		{"wheel mode", func(e *Evidence) { e.Mode = ModeWheel }, ""},
		{"bad mode", func(e *Evidence) { e.Mode = "manual" }, "mode: "},
		{"empty mode", func(e *Evidence) { e.Mode = "" }, "mode: "},
		{"schema version", func(e *Evidence) { e.SchemaVersion = 2 }, "schemaVersion: "},
		{"no run", func(e *Evidence) { e.Run = "" }, "run: "},
		{"no task", func(e *Evidence) { e.Task = "" }, "task: "},
		{"attempt zero", func(e *Evidence) { e.Attempt = 0 }, "attempt: "},
		{"bad method", func(e *Evidence) { e.Method = "../x" }, "method: "},
		{"bad method used", func(e *Evidence) { e.MethodUsed = "plugin:" }, "methodUsed: "},
		{"bad trigger kind", func(e *Evidence) { e.Trigger.Kind = "issue" }, "trigger.kind: "},
		{"no trigger ref", func(e *Evidence) { e.Trigger.Ref = "" }, "trigger.ref: "},
		{"no worker name", func(e *Evidence) { e.Worker.Name = "" }, "worker.name: "},
		{"negative diff", func(e *Evidence) { e.Diff.Added = -1 }, "diff.added: "},
		{"diff path absolute", func(e *Evidence) { e.Diff.Paths = []string{"/etc/passwd"} }, "diff.paths[0]: "},
		{"claim without text", func(e *Evidence) { e.Claims[0].Text = "" }, "claims[0].text: "},
		{"ruling without why", func(e *Evidence) { e.Rulings[0].Why = "" }, "rulings[0].why: "},
		{"denial without reason", func(e *Evidence) { e.Denials[0].Reason = "" }, "denials[0].reason: "},
		{"denial without request", func(e *Evidence) { e.Denials[0].Request = "" }, "denials[0].request: "},
		{"ok false with nothing touched and a diff", func(e *Evidence) { e.ProtectedPaths.OK = false }, "protectedPaths.ok: "},
		{"trigger factory", func(e *Evidence) { e.Trigger = TriggerRef{Kind: TriggerFactory, Ref: "r_01H7"} }, ""},
		{"trigger person", func(e *Evidence) { e.Trigger = TriggerRef{Kind: TriggerPerson, Ref: "web"} }, ""},
		{"trigger schedule", func(e *Evidence) { e.Trigger.Kind = "schedule" }, ""},
		{"touched but ok", func(e *Evidence) { e.ProtectedPaths.Touched = []string{".github/x.yml"} }, "protectedPaths.ok: "},
		{"touched not ok", func(e *Evidence) {
			e.ProtectedPaths = ProtectedPathsReport{Touched: []string{".github/x.yml"}, OK: false}
		}, ""},
		{"test file parent", func(e *Evidence) { e.TestFilesChanged = []string{"../x_test.go"} }, "testFilesChanged[0]: "},
		{"test file added bad", func(e *Evidence) { e.TestFilesAdded = []string{"./x_test.go"} }, "testFilesAdded[0]: "},
		{"check without name", func(e *Evidence) { e.Checks[0].Name = "" }, "checks[0].name: "},
		{"check without ran by", func(e *Evidence) { e.Checks[0].RanBy = "" }, "checks[0].ranBy: "},
		{"negative duration", func(e *Evidence) { e.Checks[0].DurationSeconds = -1 }, "checks[0].durationSeconds: "},
		{"acceptance bad state", func(e *Evidence) { e.Acceptance[0].State = "done" }, "acceptance[0].state: "},
		{"acceptance bad before", func(e *Evidence) { e.Acceptance[0].Before = "done" }, "acceptance[0].before: "},
		{"acceptance no before", func(e *Evidence) { e.Acceptance[0].Before = "" }, ""},
		{"acceptance bad id", func(e *Evidence) { e.Acceptance[0].ID = 0 }, "acceptance[0].id: "},
		{"working without a check", func(e *Evidence) { e.Acceptance[0].Check = "" }, "acceptance[0].check: "},
		{"not working without a check", func(e *Evidence) {
			e.Acceptance[0].State, e.Acceptance[0].Check = StateNotWorking, ""
		}, "acceptance[0].check: "},
		{"could not confirm without a check", func(e *Evidence) {
			e.Acceptance[0].State, e.Acceptance[0].Check = StateCouldNotConfirm, ""
		}, ""},
		{"duplicate acceptance id", func(e *Evidence) { e.Acceptance = append(e.Acceptance, e.Acceptance[0]) }, "acceptance[1].id: "},
		{"no review", func(e *Evidence) { e.Review = nil }, ""},
		{"bad verdict", func(e *Evidence) { e.Review.Verdict = "yes" }, "review.verdict: "},
		{"review no harness", func(e *Evidence) { e.Review.Harness = "" }, "review.harness: "},
		{"review no run", func(e *Evidence) { e.Review.Run = "" }, "review.run: "},
		{"bad risk level", func(e *Evidence) { e.Risk.Level = "extreme" }, "risk.level: "},
		{"empty risk level", func(e *Evidence) { e.Risk.Level = "" }, ""},
		{"medium risk", func(e *Evidence) { e.Risk.Level = RiskMedium }, ""},
		{"bad gate", func(e *Evidence) { e.Gate = "ok" }, "gate: "},
		{"empty gate", func(e *Evidence) { e.Gate = "" }, ""},
		{"gate exception", func(e *Evidence) { e.Gate = GateException }, ""},
		{"bad billing", func(e *Evidence) { e.Usage.Billing = "free" }, "usage.billing: "},
		{"cost with plan", func(e *Evidence) { c := 1.0; e.Usage.CostUSD = &c }, "usage.costUSD: "},
		{"cost with metered", func(e *Evidence) { c := 1.0; e.Usage.Billing, e.Usage.CostUSD = BillingMetered, &c }, ""},
		{"bad deadline", func(e *Evidence) { e.Limits.Deadline = "soon" }, "limits.deadline: "},
		{"negative max cost", func(e *Evidence) { e.Limits.MaxCostUSD = -1 }, "limits.maxCostUSD: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvidence()
			tt.mutate(e)
			err := e.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestEvidenceValidate_ReportsAllProblems(t *testing.T) {
	e := validEvidence()
	e.Mode, e.Gate, e.Risk.Level = "x", "y", "z"
	err := e.Validate()
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"mode: ", "gate: ", "risk.level: "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v lacks %q", err, want)
		}
	}
}

const runJSON = `{
  "id": "r_01H8Z", "task": "implement", "attempt": 1, "goal": 40, "ticket": 42,
  "mode": "unattended", "method": "builtin", "methodUsed": "builtin",
  "trigger": { "kind": "issueLabeled", "ref": "#42" },
  "input": { "promptHash": "sha256:abc" },
  "limits": { "deadline": "30m", "maxCostUSD": 5 },
  "phase": "working",
  "usage": { "billing": "plan", "inputTokens": 10 },
  "outputs": { "branch": "factory/r_01H8Z" },
  "createdAt": "2026-11-26T10:00:00Z",
  "startedAt": "2026-11-26T10:01:00Z"
}`

func validRun() *Run {
	r, err := ParseRun([]byte(runJSON))
	if err != nil {
		panic(err)
	}
	return r
}

func TestParseRun(t *testing.T) {
	r, err := ParseRun([]byte(runJSON))
	if err != nil {
		t.Fatalf("ParseRun: %v", err)
	}
	if r.Phase != PhaseWorking || r.Mode != ModeUnattended {
		t.Errorf("unexpected: %+v", r)
	}
	for name, in := range map[string]string{
		"empty":        "",
		"unknown":      strings.Replace(runJSON, `"phase"`, `"x": 1, "phase"`, 1),
		"case variant": strings.Replace(runJSON, `"phase"`, `"Phase"`, 1),
	} {
		if _, err := ParseRun([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestRunValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Run)
		wantErr string
	}{
		{"valid", func(*Run) {}, ""},
		{"no id", func(r *Run) { r.ID = "" }, "id: "},
		{"no task", func(r *Run) { r.Task = "" }, "task: "},
		{"attempt zero", func(r *Run) { r.Attempt = 0 }, "attempt: "},
		{"negative goal", func(r *Run) { r.Goal = -1 }, "goal: "},
		{"bad mode", func(r *Run) { r.Mode = "auto" }, "mode: "},
		{"wheel", func(r *Run) { r.Mode = ModeWheel }, ""},
		{"bad method", func(r *Run) { r.Method = "/abs" }, "method: "},
		{"bad method used", func(r *Run) { r.MethodUsed = "" }, "methodUsed: "},
		{"bad trigger", func(r *Run) { r.Trigger.Kind = "x" }, "trigger.kind: "},
		{"bad phase", func(r *Run) { r.Phase = "sleeping" }, "phase: "},
		{"no created at", func(r *Run) { r.CreatedAt = time.Time{} }, "createdAt: "},
		{"finished before started", func(r *Run) {
			f := r.StartedAt.Add(-time.Hour)
			r.FinishedAt = &f
		}, "finishedAt: "},
		{"bad limits", func(r *Run) { r.Limits.Deadline = "soon" }, "limits.deadline: "},
		{"usage cost with plan", func(r *Run) { c := 1.0; r.Usage.CostUSD = &c }, "usage.costUSD: "},
		{"bad provenance", func(r *Run) {
			e := validEvidence()
			e.Mode = "x"
			r.Provenance = e
		}, "provenance.mode: "},
		{"good provenance", func(r *Run) { r.Provenance = validEvidence() }, ""},
		{"bad result", func(r *Run) { r.Result = &Result{SchemaVersion: 1, Status: "x"} }, "result.status: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validRun()
			tt.mutate(r)
			err := r.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// A run that made no change has an empty diff, no checks, no risk and a zero
// protected-paths report. That must be valid, whatever the task.
func TestEvidenceValidate_NoChangeRuns(t *testing.T) {
	for _, task := range []string{TaskImplement, TaskReview, TaskBrainstorm, TaskDecompose, TaskAcceptance, TaskOnboard} {
		t.Run(task, func(t *testing.T) {
			e := Evidence{
				SchemaVersion: 1, Run: "r_1", Task: task, Attempt: 1, Mode: ModeUnattended,
				Method: MethodBuiltin, MethodUsed: MethodBuiltin,
				Trigger: TriggerRef{Kind: TriggerFactory, Ref: "r_0"},
				Worker:  HarnessInfo{Name: "claude-code"},
				Usage:   Usage{Billing: BillingPlan},
			}
			if err := e.Validate(); err != nil {
				t.Fatalf("no-change evidence: %v", err)
			}
			out, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseEvidence(out); err != nil {
				t.Fatalf("round trip: %v", err)
			}
		})
	}
	// With a diff, ok must still match what was touched.
	e := validEvidence()
	e.ProtectedPaths = ProtectedPathsReport{}
	if err := e.Validate(); err == nil || !strings.Contains(err.Error(), "protectedPaths.ok") {
		t.Errorf("a run with a diff and a zero report must fail, got %v", err)
	}
}

func TestTriggerKinds(t *testing.T) {
	if TriggerFactory != "factory" || TriggerPerson != "person" {
		t.Error("values")
	}
	for _, k := range []string{"issueLabeled", "issueCommented", "pullRequestOpened", "pullRequestMerged", "checkFailed", "schedule", "factory", "person"} {
		if !validTriggerKind(k) {
			t.Errorf("%s must be valid", k)
		}
	}
	for _, k := range []string{"", "issue", "Factory", "web"} {
		if validTriggerKind(k) {
			t.Errorf("%q must be invalid", k)
		}
	}
	r := validRun()
	r.Trigger = TriggerRef{Kind: TriggerPerson, Ref: "web"}
	if err := r.Validate(); err != nil {
		t.Errorf("run started by a person: %v", err)
	}
}
