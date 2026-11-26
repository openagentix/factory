package api

import (
	"strings"
	"testing"
)

const validTaskYAML = "\napiVersion: " + APIVersion + `
kind: Task
metadata:
  name: implement
spec:
  description: Turn a ready ticket into a draft pull request.
  trigger:
    issueLabeled: { label: ready-for-agent }
    throttle: { maxPerDay: 5 }
  worker:
    harness: claude-code
  prompt: prompts/implement.md
  protectedPaths:
    - "**/*_test.go"
  checks:
    - { name: test, run: "go test ./..." }
  limits:
    deadline: 30m
    maxCostUSD: 5
    maxAttempts: 2
  outputs:
    pullRequest: { draft: true }
    comment: true
  concurrencyKey: "issue-{{ .Number }}"
`

func TestParseTask_Valid(t *testing.T) {
	task, err := ParseTask([]byte(validTaskYAML))
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if task.Metadata.Name != "implement" {
		t.Errorf("name = %q", task.Metadata.Name)
	}
	if task.Spec.Trigger.IssueLabeled == nil || task.Spec.Trigger.IssueLabeled.Label != "ready-for-agent" {
		t.Errorf("issueLabeled not parsed: %+v", task.Spec.Trigger)
	}
	if task.Spec.Trigger.Throttle == nil || task.Spec.Trigger.Throttle.MaxPerDay != 5 {
		t.Errorf("throttle not parsed")
	}
	if task.Spec.Outputs.PullRequest == nil || !task.Spec.Outputs.PullRequest.Draft {
		t.Errorf("outputs not parsed")
	}
	if task.Spec.ConcurrencyKey != "issue-{{ .Number }}" {
		t.Errorf("concurrencyKey = %q", task.Spec.ConcurrencyKey)
	}
}

func TestParseTask_Strict(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"unknown top field", validTaskYAML + "extra: 1\n", "extra"},
		{"unknown spec field", strings.Replace(validTaskYAML, "  prompt:", "  promt: x\n  prompt:", 1), "promt"},
		{"empty input", "", "empty"},
		{"two documents", validTaskYAML + "---\n" + validTaskYAML, "one document"},
		{"not yaml", "a: [", "parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTask([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
