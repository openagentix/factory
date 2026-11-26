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
  method: plugin:superpowers
  prompt: prompts/implement.md
  permissions:
    commands: ["go build ./...", "go test ./..."]
  autonomy: { ceiling: 1 }
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
	if task.Spec.Method != "plugin:superpowers" || len(task.Spec.Permissions.Commands) != 2 {
		t.Errorf("method or permissions not parsed: %+v", task.Spec)
	}
	if c := task.Spec.Autonomy.Ceiling; c == nil || *c != LevelGuided {
		t.Errorf("ceiling = %v", c)
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
		{"unknown top field", validTaskYAML + "extra: 1\n", "field extra not found"},
		{"unknown spec field", strings.Replace(validTaskYAML, "  prompt:", "  promt: x\n  prompt:", 1), "field promt not found"},
		{"unknown key in issueCommented", strings.Replace(validTaskYAML, "    issueLabeled: { label: ready-for-agent }", "    issueCommented: { x: 1 }", 1), "field x not found"},
		{"unknown key in issueLabeled", strings.Replace(validTaskYAML, "label: ready-for-agent", "label: a, lable: b", 1), "field lable not found"},
		{"null trigger value", strings.Replace(validTaskYAML, "    issueLabeled: { label: ready-for-agent }", "    issueCommented: null", 1), "spec.trigger: exactly one trigger kind must be set"},
		{"two null and one set", strings.Replace(validTaskYAML, "    issueLabeled: { label: ready-for-agent }", "    pullRequestMerged: null\n    issueCommented: null", 1), "spec.trigger: exactly one trigger kind must be set"},
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

func TestParseTask_IssueCommentedTrigger(t *testing.T) {
	in := strings.Replace(validTaskYAML, "    issueLabeled: { label: ready-for-agent }", "    issueCommented: {}", 1)
	task, err := ParseTask([]byte(in))
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if task.Spec.Trigger.IssueCommented == nil || task.Spec.Trigger.Kind() != "issueCommented" {
		t.Errorf("trigger: %+v", task.Spec.Trigger)
	}
	in = strings.Replace(validTaskYAML, "    issueLabeled: { label: ready-for-agent }", "    pullRequestMerged: {}", 1)
	task, err = ParseTask([]byte(in))
	if err != nil || task.Spec.Trigger.Kind() != "pullRequestMerged" {
		t.Errorf("pullRequestMerged: %v", err)
	}
}
