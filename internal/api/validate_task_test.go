package api

import (
	"errors"
	"strings"
	"testing"
)

// validTask returns a fresh valid task for each test case to change.
func validTask() *Task {
	t, err := ParseTask([]byte(validTaskYAML))
	if err != nil {
		panic(err)
	}
	return t
}

func TestTaskValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Task)
		wantErr string // empty means valid
	}{
		{"valid", func(*Task) {}, ""},
		{"wrong apiVersion", func(x *Task) { x.APIVersion = "agentfleet.dev/v1alpha1" }, "apiVersion: must be " + APIVersion},
		{"wrong kind", func(x *Task) { x.Kind = "Factory" }, "kind: must be Task"},
		{"empty name", func(x *Task) { x.Metadata.Name = "" }, "metadata.name: "},
		{"uppercase name", func(x *Task) { x.Metadata.Name = "Implement" }, "metadata.name: "},
		{"name with underscore", func(x *Task) { x.Metadata.Name = "a_b" }, "metadata.name: "},
		{"name too long", func(x *Task) { x.Metadata.Name = strings.Repeat("a", 64) }, "metadata.name: "},
		{"no trigger", func(x *Task) { x.Spec.Trigger = Trigger{} }, "spec.trigger: exactly one trigger kind must be set"},
		{"throttle only", func(x *Task) { x.Spec.Trigger = Trigger{Throttle: &Throttle{MaxPerDay: 1}} }, "spec.trigger: exactly one trigger kind must be set"},
		{"two triggers", func(x *Task) { x.Spec.Trigger.CheckFailed = &CheckFailedTrigger{} }, "spec.trigger: exactly one trigger kind must be set"},
		{"empty label", func(x *Task) { x.Spec.Trigger.IssueLabeled.Label = "" }, "spec.trigger.issueLabeled.label: "},
		{"pull request trigger ok", func(x *Task) {
			x.Spec.Trigger = Trigger{PullRequestOpened: &PullRequestOpenedTrigger{}}
		}, ""},
		{"bad cron", func(x *Task) { x.Spec.Trigger = Trigger{Schedule: &ScheduleTrigger{Cron: "every day"}} }, "spec.trigger.schedule.cron: "},
		{"good cron", func(x *Task) { x.Spec.Trigger = Trigger{Schedule: &ScheduleTrigger{Cron: "0 3 * * 1"}} }, ""},
		{"throttle zero", func(x *Task) { x.Spec.Trigger.Throttle.MaxPerDay = 0 }, "spec.trigger.throttle.maxPerDay: "},
		{"bad harness", func(x *Task) { x.Spec.Worker.Harness = "gpt" }, "spec.worker.harness: "},
		{"codex harness", func(x *Task) { x.Spec.Worker.Harness = "codex" }, ""},
		{"stub harness", func(x *Task) { x.Spec.Worker.Harness = "stub" }, ""},
		{"empty prompt", func(x *Task) { x.Spec.Prompt = "" }, "spec.prompt: "},
		{"absolute prompt", func(x *Task) { x.Spec.Prompt = "/etc/passwd" }, "spec.prompt: "},
		{"parent prompt", func(x *Task) { x.Spec.Prompt = "../x.md" }, "spec.prompt: "},
		{"nested parent prompt", func(x *Task) { x.Spec.Prompt = "a/../../x.md" }, "spec.prompt: "},
		{"bad deadline", func(x *Task) { x.Spec.Limits.Deadline = "soon" }, "spec.limits.deadline: "},
		{"negative deadline", func(x *Task) { x.Spec.Limits.Deadline = "-5m" }, "spec.limits.deadline: "},
		{"zero deadline", func(x *Task) { x.Spec.Limits.Deadline = "0s" }, "spec.limits.deadline: "},
		{"negative cost", func(x *Task) { x.Spec.Limits.MaxCostUSD = -1 }, "spec.limits.maxCostUSD: "},
		{"zero cost ok", func(x *Task) { x.Spec.Limits.MaxCostUSD = 0 }, ""},
		{"attempts too high", func(x *Task) { x.Spec.Limits.MaxAttempts = 6 }, "spec.limits.maxAttempts: "},
		{"attempts negative", func(x *Task) { x.Spec.Limits.MaxAttempts = -1 }, "spec.limits.maxAttempts: "},
		{"attempts five ok", func(x *Task) { x.Spec.Limits.MaxAttempts = 5 }, ""},
		{"empty check name", func(x *Task) { x.Spec.Checks[0].Name = "" }, "spec.checks[0].name: "},
		{"empty check run", func(x *Task) { x.Spec.Checks[0].Run = "" }, "spec.checks[0].run: "},
		{"duplicate check", func(x *Task) { x.Spec.Checks = append(x.Spec.Checks, x.Spec.Checks[0]) }, "spec.checks[1].name: duplicate"},
		{"bad glob", func(x *Task) { x.Spec.ProtectedPaths = []string{"[abc"} }, "spec.protectedPaths[0]: "},
		{"empty glob", func(x *Task) { x.Spec.ProtectedPaths = []string{""} }, "spec.protectedPaths[0]: "},
		{"method builtin", func(x *Task) { x.Spec.Method = "builtin" }, ""},
		{"method plugin", func(x *Task) { x.Spec.Method = "plugin:superpowers" }, ""},
		{"method plugin dotted", func(x *Task) { x.Spec.Method = "plugin:open-gsd.v2" }, ""},
		{"method file", func(x *Task) { x.Spec.Method = "methods/mine.md" }, ""},
		{"prompt dot segment", func(x *Task) { x.Spec.Prompt = "./prompts/x.md" }, "spec.prompt: "},
		{"prompt doubled slash", func(x *Task) { x.Spec.Prompt = "prompts//x.md" }, "spec.prompt: "},
		{"prompt trailing slash", func(x *Task) { x.Spec.Prompt = "prompts/" }, "spec.prompt: "},
		{"prompt middle dot", func(x *Task) { x.Spec.Prompt = "a/./x.md" }, "spec.prompt: "},
		{"prompt backslash", func(x *Task) { x.Spec.Prompt = `a\x.md` }, "spec.prompt: "},
		{"method dot segment", func(x *Task) { x.Spec.Method = "./m.md" }, "spec.method: "},
		{"method doubled slash", func(x *Task) { x.Spec.Method = "a//m.md" }, "spec.method: "},
		{"method trailing slash", func(x *Task) { x.Spec.Method = "a/" }, "spec.method: "},
		{"protected absolute", func(x *Task) { x.Spec.ProtectedPaths = []string{"/etc/**"} }, "spec.protectedPaths[0]: "},
		{"protected parent", func(x *Task) { x.Spec.ProtectedPaths = []string{"../x/**"} }, "spec.protectedPaths[0]: "},
		{"protected dot segment", func(x *Task) { x.Spec.ProtectedPaths = []string{"./x/**"} }, "spec.protectedPaths[0]: "},
		{"protected double star ok", func(x *Task) { x.Spec.ProtectedPaths = []string{"**/*_test.go"} }, ""},
		{"method plugin no name", func(x *Task) { x.Spec.Method = "plugin:" }, "spec.method: "},
		{"method plugin bad name", func(x *Task) { x.Spec.Method = "plugin:a b" }, "spec.method: "},
		{"method absolute", func(x *Task) { x.Spec.Method = "/etc/m.md" }, "spec.method: "},
		{"method parent", func(x *Task) { x.Spec.Method = "../m.md" }, "spec.method: "},
		{"method nested parent", func(x *Task) { x.Spec.Method = "a/../../m.md" }, "spec.method: "},
		{"commands ok", func(x *Task) { x.Spec.Permissions.Commands = []string{"go test ./..."} }, ""},
		{"empty command", func(x *Task) { x.Spec.Permissions.Commands = []string{"go build", " "} }, "spec.permissions.commands[1]: "},
		{"ceiling zero", func(x *Task) { x.Spec.Autonomy.Ceiling = levelPtr(0) }, ""},
		{"ceiling three", func(x *Task) { x.Spec.Autonomy.Ceiling = levelPtr(3) }, ""},
		{"ceiling four", func(x *Task) { x.Spec.Autonomy.Ceiling = levelPtr(4) }, "spec.autonomy.ceiling: "},
		{"ceiling negative", func(x *Task) { x.Spec.Autonomy.Ceiling = levelPtr(-1) }, "spec.autonomy.ceiling: "},
		{"issue commented", func(x *Task) { x.Spec.Trigger = Trigger{IssueCommented: &IssueCommentedTrigger{}} }, ""},
		{"pull request merged", func(x *Task) { x.Spec.Trigger = Trigger{PullRequestMerged: &PullRequestMergedTrigger{}} }, ""},
		{"merged and commented", func(x *Task) {
			x.Spec.Trigger = Trigger{IssueCommented: &IssueCommentedTrigger{}, PullRequestMerged: &PullRequestMergedTrigger{}}
		}, "spec.trigger: exactly one trigger kind must be set"},
		{"bad concurrency template", func(x *Task) { x.Spec.ConcurrencyKey = "{{ .Number" }, "spec.concurrencyKey: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := validTask()
			tt.mutate(task)
			err := task.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			var ve ValidationErrors
			if !errors.As(err, &ve) {
				t.Errorf("error is not ValidationErrors: %T", err)
			}
		})
	}
}

func TestTaskValidate_ReportsAllProblems(t *testing.T) {
	task := validTask()
	task.Spec.Prompt = ""
	task.Spec.Worker.Harness = "x"
	err := task.Validate()
	if err == nil || !strings.Contains(err.Error(), "spec.prompt") || !strings.Contains(err.Error(), "spec.worker.harness") {
		t.Fatalf("want both problems, got %v", err)
	}
}

func TestTaskDefaults(t *testing.T) {
	task := validTask()
	task.Spec.Limits = Limits{}
	task.Spec.ConcurrencyKey = ""
	task.Spec.Method = ""
	task.Spec.Autonomy = Autonomy{}
	task.ApplyDefaults()
	if task.Spec.Limits.Deadline != "30m" {
		t.Errorf("deadline = %q", task.Spec.Limits.Deadline)
	}
	if task.Spec.Limits.MaxAttempts != 3 || DefaultMaxAttempts != 3 {
		t.Errorf("maxAttempts = %d", task.Spec.Limits.MaxAttempts)
	}
	if task.Spec.Method != MethodBuiltin {
		t.Errorf("method = %q", task.Spec.Method)
	}
	if c := task.Spec.Autonomy.Ceiling; c == nil || *c != LevelAutopilot || DefaultCeiling != LevelAutopilot {
		t.Errorf("ceiling = %v", c)
	}
	if task.Spec.ConcurrencyKey != DefaultConcurrencyKey || DefaultConcurrencyKey != "{{ .Kind }}-{{ .Number }}" {
		t.Errorf("concurrencyKey = %q", task.Spec.ConcurrencyKey)
	}
	if err := task.Validate(); err != nil {
		t.Errorf("defaults should validate: %v", err)
	}
}

func TestParseTask_ValidatesAndDefaults(t *testing.T) {
	minimal := "\napiVersion: " + APIVersion + `
kind: Task
metadata: { name: review }
spec:
  trigger: { pullRequestOpened: {} }
  worker: { harness: stub }
  prompt: prompts/review.md
`
	task, err := ParseTask([]byte(minimal))
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if task.Spec.Trigger.PullRequestOpened == nil {
		t.Error("pullRequestOpened {} must parse as set")
	}
	if task.Spec.Limits.Deadline != "30m" || task.Spec.Limits.MaxAttempts != 3 {
		t.Errorf("defaults not applied: %+v", task.Spec.Limits)
	}
	_, err = ParseTask([]byte(strings.Replace(minimal, "prompts/review.md", "../x", 1)))
	if err == nil || !strings.Contains(err.Error(), "spec.prompt") {
		t.Errorf("want validation error, got %v", err)
	}
}

func levelPtr(l Level) *Level { return &l }

// Every task named in decision 0001 must be a valid task name.
func TestTaskNamesFromTheRecord(t *testing.T) {
	for _, name := range []string{TaskOnboard, TaskAcceptance, TaskImplement, TaskFixCI, TaskReview, TaskBrainstorm, TaskDecompose} {
		task := validTask()
		task.Metadata.Name = name
		if err := task.Validate(); err != nil {
			t.Errorf("task %q: %v", name, err)
		}
	}
}
