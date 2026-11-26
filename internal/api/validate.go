package api

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// Defaults for optional task fields.
const (
	DefaultConcurrencyKey = "{{ .Kind }}-{{ .Number }}"
	DefaultDeadline       = "30m"
	DefaultMaxAttempts    = 2
	MaxMaxAttempts        = 5
)

// Harness names a coding agent program a worker can launch.
const (
	HarnessClaudeCode = "claude-code"
	HarnessCodex      = "codex"
	HarnessStub       = "stub"
)

// FieldError is one validation problem. Path names the field, for example
// spec.trigger.
type FieldError struct {
	Path string
	Msg  string
}

func (e FieldError) Error() string { return e.Path + ": " + e.Msg }

// ValidationErrors is the list of all problems found in one resource.
type ValidationErrors []FieldError

func (v ValidationErrors) Error() string {
	parts := make([]string, len(v))
	for i, e := range v {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

// problems collects field errors.
type problems struct{ list ValidationErrors }

func (p *problems) add(path, format string, args ...any) {
	p.list = append(p.list, FieldError{Path: path, Msg: fmt.Sprintf(format, args...)})
}

func (p *problems) err() error {
	if len(p.list) == 0 {
		return nil
	}
	return p.list
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validHarness(h string) bool {
	return h == HarnessClaudeCode || h == HarnessCodex || h == HarnessStub
}

func checkHeader(p *problems, apiVersion, kind, wantKind, name string) {
	if apiVersion != APIVersion {
		p.add("apiVersion", "must be %s", APIVersion)
	}
	if kind != wantKind {
		p.add("kind", "must be %s", wantKind)
	}
	if len(name) == 0 || len(name) > 63 || !dnsLabel.MatchString(name) {
		p.add("metadata.name", "must be a DNS label: 1 to 63 lower case letters, digits or dashes, starting and ending with a letter or digit")
	}
}

func checkWorker(p *problems, path string, w Worker) {
	if !validHarness(w.Harness) {
		p.add(path+".harness", "must be one of %s, %s, %s", HarnessClaudeCode, HarnessCodex, HarnessStub)
	}
}

func parsePositiveDuration(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	if d <= 0 {
		return fmt.Errorf("must be greater than zero")
	}
	return nil
}

// Kind returns the name of the single trigger kind that is set, or "" when
// none or more than one is set.
func (t Trigger) Kind() string {
	var kinds []string
	if t.IssueLabeled != nil {
		kinds = append(kinds, "issueLabeled")
	}
	if t.PullRequestOpened != nil {
		kinds = append(kinds, "pullRequestOpened")
	}
	if t.CheckFailed != nil {
		kinds = append(kinds, "checkFailed")
	}
	if t.Schedule != nil {
		kinds = append(kinds, "schedule")
	}
	if len(kinds) != 1 {
		return ""
	}
	return kinds[0]
}

// ApplyDefaults fills defaults for unset optional fields.
func (t *Task) ApplyDefaults() {
	if t.Spec.Limits.Deadline == "" {
		t.Spec.Limits.Deadline = DefaultDeadline
	}
	if t.Spec.Limits.MaxAttempts == 0 {
		t.Spec.Limits.MaxAttempts = DefaultMaxAttempts
	}
	if t.Spec.ConcurrencyKey == "" {
		t.Spec.ConcurrencyKey = DefaultConcurrencyKey
	}
}

// Validate checks the task. It returns nil or a ValidationErrors value.
// Call ApplyDefaults first: Validate does not change the task.
func (t *Task) Validate() error {
	var p problems
	checkHeader(&p, t.APIVersion, t.Kind, KindTask, t.Metadata.Name)
	s := t.Spec

	validateTrigger(&p, s.Trigger)
	checkWorker(&p, "spec.worker", s.Worker)

	if s.Prompt == "" {
		p.add("spec.prompt", "must be set")
	} else if !relativeNoParent(s.Prompt) {
		p.add("spec.prompt", "must be a relative path without ..")
	}

	for i, g := range s.ProtectedPaths {
		if g == "" {
			p.add(fmt.Sprintf("spec.protectedPaths[%d]", i), "must not be empty")
		} else if _, err := path.Match(g, ""); err != nil {
			p.add(fmt.Sprintf("spec.protectedPaths[%d]", i), "invalid glob pattern: %v", err)
		}
	}

	seen := map[string]bool{}
	for i, c := range s.Checks {
		base := fmt.Sprintf("spec.checks[%d]", i)
		switch {
		case c.Name == "":
			p.add(base+".name", "must not be empty")
		case seen[c.Name]:
			p.add(base+".name", "duplicate check name %q", c.Name)
		}
		seen[c.Name] = true
		if c.Run == "" {
			p.add(base+".run", "must not be empty")
		}
	}

	if err := parsePositiveDuration(s.Limits.Deadline); err != nil {
		p.add("spec.limits.deadline", "must be a positive duration such as 30m: %v", err)
	}
	if s.Limits.MaxCostUSD < 0 {
		p.add("spec.limits.maxCostUSD", "must be zero or more")
	}
	if s.Limits.MaxAttempts < 1 || s.Limits.MaxAttempts > MaxMaxAttempts {
		p.add("spec.limits.maxAttempts", "must be between 1 and %d", MaxMaxAttempts)
	}

	if _, err := template.New("key").Parse(s.ConcurrencyKey); err != nil {
		p.add("spec.concurrencyKey", "invalid template: %v", err)
	}
	return p.err()
}

func validateTrigger(p *problems, tr Trigger) {
	if tr.Kind() == "" {
		p.add("spec.trigger", "exactly one trigger kind must be set")
	}
	if tr.IssueLabeled != nil && tr.IssueLabeled.Label == "" {
		p.add("spec.trigger.issueLabeled.label", "must not be empty")
	}
	if tr.Schedule != nil && !validCron(tr.Schedule.Cron) {
		p.add("spec.trigger.schedule.cron", "must be a cron expression with five fields, for example \"0 3 * * 1\"")
	}
	if tr.Throttle != nil && tr.Throttle.MaxPerDay < 1 {
		p.add("spec.trigger.throttle.maxPerDay", "must be 1 or more")
	}
}

// validCron checks only the shape: five fields. The scheduler checks the values.
func validCron(s string) bool { return len(strings.Fields(s)) == 5 }

// relativeNoParent reports whether p is a relative path with no .. part.
func relativeNoParent(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || (len(p) > 1 && p[1] == ':') {
		return false
	}
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	return true
}
