package api

import (
	"strings"
	"testing"
)

const validResultJSON = `{
  "schemaVersion": 1,
  "status": "succeeded",
  "summary": "Customers can move a booking to another day.",
  "claims": [ { "text": "Added a move endpoint with a test", "files": ["internal/booking/move.go", "internal/booking/move_test.go"] } ],
  "criteria": [ { "id": 1, "opinion": "met", "note": "Covered by the new handler" } ],
  "rulings": [ { "decision": "Kept the old endpoint as an alias", "why": "Two callers still use it", "costIfWrong": "One extra route to remove later" } ],
  "questions": [],
  "usage": { "billing": "plan", "inputTokens": 12000, "outputTokens": 3400 },
  "harness": { "name": "claude-code", "version": "2.1.326", "model": "" }
}`

func TestParseResult_Valid(t *testing.T) {
	r, err := ParseResult([]byte(validResultJSON))
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if r.Status != StatusSucceeded || len(r.Claims) != 1 || r.Harness.Version != "2.1.326" {
		t.Errorf("unexpected result: %+v", r)
	}
	if r.Claims[0].Text != "Added a move endpoint with a test" || len(r.Claims[0].Files) != 2 {
		t.Errorf("claim: %+v", r.Claims[0])
	}
	if len(r.Criteria) != 1 || r.Criteria[0].ID != 1 || r.Criteria[0].Opinion != OpinionMet {
		t.Errorf("criteria: %+v", r.Criteria)
	}
	if len(r.Rulings) != 1 || r.Rulings[0].CostIfWrong == "" {
		t.Errorf("rulings: %+v", r.Rulings)
	}
	if r.Usage.Billing != BillingPlan || r.Usage.CostUSD != nil || r.Usage.InputTokens != 12000 {
		t.Errorf("usage: %+v", r.Usage)
	}
}

func TestParseResult_BillingDefaultsToUnknown(t *testing.T) {
	r, err := ParseResult([]byte(`{"schemaVersion":1,"status":"no_change","summary":"nothing to do"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Usage.Billing != BillingUnknown || r.Usage.CostUSD != nil {
		t.Errorf("usage: %+v", r.Usage)
	}
}

func TestParseResult_MeteredCost(t *testing.T) {
	in := `{"schemaVersion":1,"status":"failed","summary":"x","usage":{"billing":"metered","costUSD":0.42}}`
	r, err := ParseResult([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if r.Usage.CostUSD == nil || *r.Usage.CostUSD != 0.42 {
		t.Errorf("cost: %+v", r.Usage)
	}
}

func TestParseResult_WorkerCannotSetSourceOrEnforced(t *testing.T) {
	in := `{"schemaVersion":1,"status":"failed","summary":"x","usage":{"billing":"plan","source":"me","enforced":true}}`
	r, err := ParseResult([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if r.Usage.Source != "" || r.Usage.Enforced {
		t.Errorf("source and enforced must be ignored: %+v", r.Usage)
	}
}

func TestParseResult_Invalid(t *testing.T) {
	const head = `{"schemaVersion":1,"status":"failed","summary":"x"`
	tests := []struct {
		name, input, wantErr string
	}{
		{"empty", "", "result"},
		{"not json", "{", "result"},
		{"unknown field", head + `,"bogus":1}`, `unknown field "bogus"`},
		{"trailing data", head + `} {}`, "trailing"},
		{"missing schema version", `{"status":"failed","summary":"x"}`, "schemaVersion"},
		{"wrong schema version", `{"schemaVersion":2,"status":"failed","summary":"x"}`, "schemaVersion"},
		{"missing status", `{"schemaVersion":1,"summary":"x"}`, "status"},
		{"bad status", `{"schemaVersion":1,"status":"done","summary":"x"}`, "status"},
		{"missing summary", `{"schemaVersion":1,"status":"failed"}`, "summary"},
		{"blank summary", `{"schemaVersion":1,"status":"failed","summary":"  "}`, "summary"},
		{"claims as strings", head + `,"claims":["a"]}`, "claims"},
		{"claim without text", head + `,"claims":[{"files":["a"]}]}`, "claims[0].text"},
		{"claim empty file", head + `,"claims":[{"text":"a","files":[""]}]}`, "claims[0].files[0]"},
		{"claim unknown field", head + `,"claims":[{"text":"a","x":1}]}`, `unknown field "x"`},
		{"claim absolute file", head + `,"claims":[{"text":"a","files":["/etc/passwd"]}]}`, "claims[0].files[0]"},
		{"claim parent file", head + `,"claims":[{"text":"a","files":["../x.go"]}]}`, "claims[0].files[0]"},
		{"claim dot file", head + `,"claims":[{"text":"a","files":["./x.go"]}]}`, "claims[0].files[0]"},
		{"criterion id zero", head + `,"criteria":[{"id":0,"opinion":"met"}]}`, "criteria[0].id"},
		{"criterion bad opinion", head + `,"criteria":[{"id":1,"opinion":"yes"}]}`, "criteria[0].opinion"},
		{"criterion duplicate", head + `,"criteria":[{"id":1,"opinion":"met"},{"id":1,"opinion":"unsure"}]}`, "criteria[1].id"},
		{"ruling no decision", head + `,"rulings":[{"why":"w","costIfWrong":"c"}]}`, "rulings[0].decision"},
		{"ruling no why", head + `,"rulings":[{"decision":"d","costIfWrong":"c"}]}`, "rulings[0].why"},
		{"ruling no cost", head + `,"rulings":[{"decision":"d","why":"w"}]}`, "rulings[0].costIfWrong"},
		{"blank question", head + `,"questions":[" "]}`, "questions[0]"},
		{"bad billing", head + `,"usage":{"billing":"free"}}`, "usage.billing"},
		{"cost with plan", head + `,"usage":{"billing":"plan","costUSD":1}}`, "usage.costUSD"},
		{"cost with unknown", head + `,"usage":{"costUSD":1}}`, "usage.costUSD"},
		{"negative cost", head + `,"usage":{"billing":"metered","costUSD":-1}}`, "usage.costUSD"},
		{"negative tokens", head + `,"usage":{"inputTokens":-1}}`, "usage.inputTokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResult([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseResult_ReportsAllProblems(t *testing.T) {
	_, err := ParseResult([]byte(`{"schemaVersion":1,"status":"x","summary":"","usage":{"billing":"free"}}`))
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"status", "summary", "usage.billing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v lacks %q", err, want)
		}
	}
}

func TestParseResult_AllStatuses(t *testing.T) {
	for _, s := range []string{StatusSucceeded, StatusFailed, StatusNeedsHuman, StatusNoChange} {
		in := `{"schemaVersion":1,"status":"` + s + `","summary":"x"}`
		if _, err := ParseResult([]byte(in)); err != nil {
			t.Errorf("status %s: %v", s, err)
		}
	}
}
