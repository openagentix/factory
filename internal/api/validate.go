package api

import (
	"errors"
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
	DefaultMaxAttempts    = 3
	MaxMaxAttempts        = 5
	// DefaultCeiling is the highest dial level when a task sets none. Level 3
	// is shown but not active in the first release.
	DefaultCeiling = LevelAutopilot
)

// Method values. A method may also be plugin:<name> or a relative file path.
const (
	MethodBuiltin      = "builtin"
	MethodPluginPrefix = "plugin:"
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
	if t.IssueCommented != nil {
		kinds = append(kinds, "issueCommented")
	}
	if t.PullRequestOpened != nil {
		kinds = append(kinds, "pullRequestOpened")
	}
	if t.PullRequestMerged != nil {
		kinds = append(kinds, "pullRequestMerged")
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
	if t.Spec.Method == "" {
		t.Spec.Method = MethodBuiltin
	}
	if t.Spec.Autonomy.Ceiling == nil {
		c := DefaultCeiling
		t.Spec.Autonomy.Ceiling = &c
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
	} else if !cleanRelativePath(s.Prompt) {
		p.add("spec.prompt", "must be a clean relative path: no leading /, no empty, . or .. part")
	}

	if !validMethod(s.Method) {
		p.add("spec.method", "must be builtin, plugin:<name>, or a clean relative file path")
	}
	for i, c := range s.Permissions.Commands {
		if blank(c) {
			p.add(fmt.Sprintf("spec.permissions.commands[%d]", i), "must not be empty")
		}
	}
	if c := s.Autonomy.Ceiling; c != nil && !c.Valid() {
		p.add("spec.autonomy.ceiling", "must be 0 to 3")
	}

	for i, g := range s.ProtectedPaths {
		checkGlob(&p, fmt.Sprintf("spec.protectedPaths[%d]", i), g)
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

// cleanRelativePath reports whether p is a clean repository-relative path.
// This one rule applies to every path field. It rejects an empty path, an
// absolute path, a backslash, an empty segment (a doubled or trailing slash)
// and a "." or ".." segment. A path is never cleaned for the caller, so a
// pattern built from it always matches what it says.
func cleanRelativePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) || (len(p) > 1 && p[1] == ':') {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// checkGlob checks a path pattern: clean and relative, and a valid glob.
func checkGlob(p *problems, field, g string) {
	switch {
	case g == "":
		p.add(field, "must not be empty")
	case !cleanRelativePath(g):
		p.add(field, "must be a clean relative pattern: no leading /, no backslash, no empty, . or .. part")
	default:
		if _, err := path.Match(g, ""); err != nil {
			p.add(field, "invalid glob pattern: %v", err)
		}
	}
}

var pluginName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validMethod checks a method value: builtin, plugin:<name> or a relative
// file path with no .. part.
func validMethod(m string) bool {
	switch {
	case m == MethodBuiltin:
		return true
	case strings.HasPrefix(m, MethodPluginPrefix):
		return pluginName.MatchString(strings.TrimPrefix(m, MethodPluginPrefix))
	}
	return cleanRelativePath(m)
}

// Trigger reference kinds besides the task trigger kinds.
const (
	// TriggerFactory is a run the factory started itself after another run:
	// acceptance, review, onboard or a rebuild.
	TriggerFactory = "factory"
	// TriggerPerson is a run a person started: take the wheel, or an action
	// in the web app.
	TriggerPerson = "person"
)

// triggerKinds are the kinds a run's trigger reference may name: the six task
// trigger kinds, factory and person.
var triggerKinds = []string{
	"issueLabeled", "issueCommented", "pullRequestOpened", "pullRequestMerged", "checkFailed", "schedule",
	TriggerFactory, TriggerPerson,
}

func validTriggerKind(k string) bool {
	for _, t := range triggerKinds {
		if t == k {
			return true
		}
	}
	return false
}

// addNested adds the problems of a nested value under a field prefix.
func (p *problems) addNested(prefix string, err error) {
	var ve ValidationErrors
	if errors.As(err, &ve) {
		for _, e := range ve {
			p.list = append(p.list, FieldError{Path: prefix + "." + e.Path, Msg: e.Msg})
		}
	} else if err != nil {
		p.add(prefix, "%v", err)
	}
}
