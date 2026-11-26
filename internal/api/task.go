package api

// ObjectMeta is the metadata block of a resource.
type ObjectMeta struct {
	Name string `json:"name" yaml:"name"`
}

// Task is one task file. The same file runs on every rung.
type Task struct {
	APIVersion string     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string     `json:"kind" yaml:"kind"`
	Metadata   ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec       TaskSpec   `json:"spec" yaml:"spec"`
}

// TaskSpec is the body of a Task.
type TaskSpec struct {
	Description    string   `json:"description,omitempty" yaml:"description,omitempty"`
	Trigger        Trigger  `json:"trigger" yaml:"trigger"`
	Worker         Worker   `json:"worker" yaml:"worker"`
	Prompt         string   `json:"prompt" yaml:"prompt"`
	ProtectedPaths []string `json:"protectedPaths,omitempty" yaml:"protectedPaths,omitempty"`
	Checks         []Check  `json:"checks,omitempty" yaml:"checks,omitempty"`
	Limits         Limits   `json:"limits" yaml:"limits"`
	Outputs        Outputs  `json:"outputs" yaml:"outputs"`
	// ConcurrencyKey is a template that stops two runs doing the same work.
	ConcurrencyKey string `json:"concurrencyKey,omitempty" yaml:"concurrencyKey,omitempty"`
}

// Trigger says what starts a task. Exactly one kind must be set.
type Trigger struct {
	IssueLabeled      *IssueLabeledTrigger      `json:"issueLabeled,omitempty" yaml:"issueLabeled,omitempty"`
	PullRequestOpened *PullRequestOpenedTrigger `json:"pullRequestOpened,omitempty" yaml:"pullRequestOpened,omitempty"`
	CheckFailed       *CheckFailedTrigger       `json:"checkFailed,omitempty" yaml:"checkFailed,omitempty"`
	Schedule          *ScheduleTrigger          `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	// Throttle is optional and applies to any trigger kind.
	Throttle *Throttle `json:"throttle,omitempty" yaml:"throttle,omitempty"`
}

// IssueLabeledTrigger fires when an issue gets the label.
type IssueLabeledTrigger struct {
	Label string `json:"label" yaml:"label"`
}

// PullRequestOpenedTrigger fires when a pull request is opened. It has no options.
type PullRequestOpenedTrigger struct{}

// CheckFailedTrigger fires when a check on a pull request fails. It has no options.
type CheckFailedTrigger struct{}

// ScheduleTrigger fires on a cron schedule.
type ScheduleTrigger struct {
	Cron string `json:"cron" yaml:"cron"`
}

// Throttle caps how often a trigger may start runs.
type Throttle struct {
	MaxPerDay int `json:"maxPerDay" yaml:"maxPerDay"`
}

// Worker names the harness and model a run uses.
type Worker struct {
	Harness string `json:"harness" yaml:"harness"`
	// Model is empty for the harness default.
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
}

// Check is a command the orchestrator runs in the verify stage.
type Check struct {
	Name string `json:"name" yaml:"name"`
	Run  string `json:"run" yaml:"run"`
}

// Limits bound one run. Deadline is a duration string such as 30m.
type Limits struct {
	Deadline    string  `json:"deadline,omitempty" yaml:"deadline,omitempty"`
	MaxCostUSD  float64 `json:"maxCostUSD,omitempty" yaml:"maxCostUSD,omitempty"`
	MaxAttempts int     `json:"maxAttempts,omitempty" yaml:"maxAttempts,omitempty"`
}

// Outputs say what a run may write to the git host.
type Outputs struct {
	PullRequest *PullRequestOutput `json:"pullRequest,omitempty" yaml:"pullRequest,omitempty"`
	Comment     bool               `json:"comment,omitempty" yaml:"comment,omitempty"`
}

// PullRequestOutput configures the pull request a run opens.
type PullRequestOutput struct {
	Draft bool `json:"draft" yaml:"draft"`
}

// ParseTask reads a task file. Unknown fields are an error.
func ParseTask(data []byte) (*Task, error) {
	var t Task
	if err := decodeYAMLStrict(data, &t); err != nil {
		return nil, err
	}
	t.ApplyDefaults()
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}
