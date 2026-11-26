package api

import (
	"strings"
	"testing"
)

const validResultJSON = `{
  "schemaVersion": 1,
  "status": "succeeded",
  "summary": "Added retry to fetch()",
  "claims": ["Added retry to fetch()", "Added a test for timeout"],
  "questions": [],
  "usage": { "costUSD": 0.42, "inputTokens": 12000, "outputTokens": 3400, "source": "harness" },
  "harness": { "name": "claude-code", "version": "2.1.326", "model": "m" }
}`

func TestParseResult_Valid(t *testing.T) {
	r, err := ParseResult([]byte(validResultJSON))
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if r.Status != StatusSucceeded || len(r.Claims) != 2 || r.Harness.Version != "2.1.326" {
		t.Errorf("unexpected result: %+v", r)
	}
	if r.Usage.CostUSD == nil || *r.Usage.CostUSD != 0.42 || r.Usage.InputTokens != 12000 {
		t.Errorf("usage not parsed: %+v", r.Usage)
	}
}

func TestParseResult_UnknownCost(t *testing.T) {
	r, err := ParseResult([]byte(`{"schemaVersion":1,"status":"no_change","summary":"nothing to do"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Usage.CostUSD != nil {
		t.Error("missing cost must stay nil (unknown)")
	}
}

func TestParseResult_Invalid(t *testing.T) {
	tests := []struct {
		name, input, wantErr string
	}{
		{"empty", "", "result"},
		{"not json", "{", "result"},
		{"unknown field", `{"schemaVersion":1,"status":"failed","summary":"x","bogus":1}`, "bogus"},
		{"trailing data", `{"schemaVersion":1,"status":"failed","summary":"x"} {}`, "trailing"},
		{"missing schema version", `{"status":"failed","summary":"x"}`, "schemaVersion"},
		{"wrong schema version", `{"schemaVersion":2,"status":"failed","summary":"x"}`, "schemaVersion"},
		{"missing status", `{"schemaVersion":1,"summary":"x"}`, "status"},
		{"bad status", `{"schemaVersion":1,"status":"done","summary":"x"}`, "status"},
		{"missing summary", `{"schemaVersion":1,"status":"failed"}`, "summary"},
		{"blank summary", `{"schemaVersion":1,"status":"failed","summary":"  "}`, "summary"},
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

func TestParseResult_AllStatuses(t *testing.T) {
	for _, s := range []string{StatusSucceeded, StatusFailed, StatusNeedsHuman, StatusNoChange} {
		in := `{"schemaVersion":1,"status":"` + s + `","summary":"x"}`
		if _, err := ParseResult([]byte(in)); err != nil {
			t.Errorf("status %s: %v", s, err)
		}
	}
}
