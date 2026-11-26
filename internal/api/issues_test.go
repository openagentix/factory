package api

import (
	"fmt"
	"strings"
	"testing"
)

const validIssuesJSON = `{ "schemaVersion": 1, "issues": [
  { "title": "Move endpoint", "body": "Let a customer move a booking.", "criteria": [1, 2], "dependsOn": [] },
  { "title": "Confirmation email", "body": "Send an email after a move.", "criteria": [3], "dependsOn": [0] } ] }`

func TestParseIssues_Valid(t *testing.T) {
	f, err := ParseIssues([]byte(validIssuesJSON))
	if err != nil {
		t.Fatalf("ParseIssues: %v", err)
	}
	if len(f.Issues) != 2 || f.Issues[1].DependsOn[0] != 0 || f.Issues[0].Criteria[1] != 2 {
		t.Errorf("unexpected: %+v", f)
	}
}

func issuesWith(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"title":"t%d","body":"b"}`, i)
	}
	return `{"schemaVersion":1,"issues":[` + strings.Join(items, ",") + `]}`
}

func TestParseIssues_Limit(t *testing.T) {
	if MaxIssuesPerRun != 12 {
		t.Fatalf("MaxIssuesPerRun = %d, want 12", MaxIssuesPerRun)
	}
	if _, err := ParseIssues([]byte(issuesWith(12))); err != nil {
		t.Errorf("12 issues must pass: %v", err)
	}
	if _, err := ParseIssues([]byte(issuesWith(13))); err == nil || !strings.Contains(err.Error(), "issues") {
		t.Errorf("13 issues must fail, got %v", err)
	}
}

func TestParseIssues_Invalid(t *testing.T) {
	const head = `{"schemaVersion":1,"issues":[`
	tests := []struct{ name, input, wantErr string }{
		{"empty", "", "issues"},
		{"unknown field", `{"schemaVersion":1,"issues":[],"x":1}`, `unknown field "x"`},
		{"wrong version", `{"schemaVersion":2,"issues":[{"title":"t"}]}`, "schemaVersion"},
		{"no issues", `{"schemaVersion":1,"issues":[]}`, "issues: must hold"},
		{"no title", head + `{"body":"b"}]}`, "issues[0].title"},
		{"bad criterion", head + `{"title":"t","criteria":[0]}]}`, "issues[0].criteria[0]"},
		{"duplicate criterion", head + `{"title":"t","criteria":[1,1]}]}`, "issues[0].criteria[1]"},
		{"depends out of range", head + `{"title":"t","dependsOn":[1]}]}`, "issues[0].dependsOn[0]"},
		{"depends negative", head + `{"title":"t","dependsOn":[-1]}]}`, "issues[0].dependsOn[0]"},
		{"depends on itself", head + `{"title":"t","dependsOn":[0]}]}`, "issues[0].dependsOn[0]"},
		{"cycle", head + `{"title":"a","dependsOn":[1]},{"title":"b","dependsOn":[0]}]}`, "cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseIssues([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
