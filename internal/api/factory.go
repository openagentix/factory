package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Defaults for optional factory fields.
const (
	DefaultMaxOpenAgentPullRequests = 3
	DefaultPollInterval             = "1m"
	// DefaultAcceptancePath is the root of the acceptance checks.
	DefaultAcceptancePath = "acceptance"
	// FactoryDir is the directory in the repository that holds the factory's
	// own files. It is always protected.
	FactoryDir = ".factory"
)

// DefaultProtectedPaths is the default of spec.protectedPaths.
func DefaultProtectedPaths() []string { return []string{".github/**"} }

// Factory is the file that names the repository and the review cap.
type Factory struct {
	APIVersion string      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string      `json:"kind" yaml:"kind"`
	Metadata   ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec       FactorySpec `json:"spec" yaml:"spec"`
}

// FactorySpec is the body of a Factory.
type FactorySpec struct {
	// Repository is the git host repository as owner/name.
	Repository    string `json:"repository" yaml:"repository"`
	DefaultWorker Worker `json:"defaultWorker" yaml:"defaultWorker"`
	// MaxOpenAgentPullRequests is the review capacity brake.
	MaxOpenAgentPullRequests int `json:"maxOpenAgentPullRequests,omitempty" yaml:"maxOpenAgentPullRequests,omitempty"`
	// PollInterval is a duration string such as 1m.
	PollInterval string `json:"pollInterval,omitempty" yaml:"pollInterval,omitempty"`
	// AcceptancePath is the root of the acceptance checks, relative to the
	// repository. Checks live under <AcceptancePath>/<goal number>/.
	AcceptancePath string `json:"acceptancePath,omitempty" yaml:"acceptancePath,omitempty"`
	// Start is the optional command that lets Try it start the application.
	Start string `json:"start,omitempty" yaml:"start,omitempty"`
	// ProtectedPaths adds to the fixed protected paths. Nil means the
	// default. An empty list removes the default.
	ProtectedPaths []string  `json:"protectedPaths,omitempty" yaml:"protectedPaths,omitempty"`
	RiskPaths      RiskPaths `json:"riskPaths,omitempty" yaml:"riskPaths,omitempty"`
}

// RiskPaths lists the path patterns that raise the risk of a change, by kind.
type RiskPaths struct {
	Contract     []string `json:"contract,omitempty" yaml:"contract,omitempty"`
	Security     []string `json:"security,omitempty" yaml:"security,omitempty"`
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	// Documents lists paths whose change alone is low risk.
	Documents []string `json:"documents,omitempty" yaml:"documents,omitempty"`
}

// FixedProtectedPaths returns the paths that are always protected: the
// factory directory and the acceptance path. Each task has its own exception
// (onboard for the factory directory, acceptance for the acceptance path).
func (f *Factory) FixedProtectedPaths() []string {
	ap := f.Spec.AcceptancePath
	if ap == "" {
		ap = DefaultAcceptancePath
	}
	return []string{FactoryDir + "/**", ap + "/**"}
}

// ParseFactory reads a factory file, applies defaults and validates it.
// Unknown fields are an error.
func ParseFactory(data []byte) (*Factory, error) {
	var f Factory
	if err := decodeYAMLStrict(data, &f); err != nil {
		return nil, err
	}
	f.ApplyDefaults()
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// ApplyDefaults fills defaults for unset optional fields.
func (f *Factory) ApplyDefaults() {
	if f.Spec.MaxOpenAgentPullRequests == 0 {
		f.Spec.MaxOpenAgentPullRequests = DefaultMaxOpenAgentPullRequests
	}
	if f.Spec.PollInterval == "" {
		f.Spec.PollInterval = DefaultPollInterval
	}
	if f.Spec.AcceptancePath == "" {
		f.Spec.AcceptancePath = DefaultAcceptancePath
	}
	if f.Spec.ProtectedPaths == nil {
		f.Spec.ProtectedPaths = DefaultProtectedPaths()
	}
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// Validate checks the factory. Call ApplyDefaults first.
func (f *Factory) Validate() error {
	var p problems
	checkHeader(&p, f.APIVersion, f.Kind, KindFactory, f.Metadata.Name)
	if !repoName.MatchString(f.Spec.Repository) {
		p.add("spec.repository", "must be owner/name")
	}
	checkWorker(&p, "spec.defaultWorker", f.Spec.DefaultWorker)
	if f.Spec.MaxOpenAgentPullRequests < 1 {
		p.add("spec.maxOpenAgentPullRequests", "must be 1 or more")
	}
	if err := parsePositiveDuration(f.Spec.PollInterval); err != nil {
		p.add("spec.pollInterval", "must be a positive duration such as 1m: %v", err)
	}
	ap := f.Spec.AcceptancePath
	if !cleanRelativePath(ap) || strings.ContainsAny(ap, "*?[{ ") {
		p.add("spec.acceptancePath", "must be a plain relative directory below the repository root: no leading /, no trailing /, no empty, . or .. part, no glob character (* ? [ {) and no space")
	}
	f.checkPaths(&p)
	return p.err()
}

// riskEntry is one risk pattern with the name of the field it came from.
type riskEntry struct{ pattern, field string }

// riskEntries lists every risk pattern, in a fixed order.
func (f *Factory) riskEntries() []riskEntry {
	var out []riskEntry
	for _, l := range []struct {
		name string
		list []string
	}{
		{"contract", f.Spec.RiskPaths.Contract},
		{"security", f.Spec.RiskPaths.Security},
		{"dependencies", f.Spec.RiskPaths.Dependencies},
		{"documents", f.Spec.RiskPaths.Documents},
	} {
		for i, g := range l.list {
			out = append(out, riskEntry{g, fmt.Sprintf("spec.riskPaths.%s[%d]", l.name, i)})
		}
	}
	return out
}

// checkPaths validates the path patterns and rejects a protected pattern and
// a risk pattern that overlap.
func (f *Factory) checkPaths(p *problems) {
	var protected []riskEntry
	for _, g := range f.FixedProtectedPaths() {
		protected = append(protected, riskEntry{g, "the fixed protected paths"})
	}
	for i, g := range f.Spec.ProtectedPaths {
		field := fmt.Sprintf("spec.protectedPaths[%d]", i)
		checkGlob(p, field, g)
		protected = append(protected, riskEntry{g, field})
	}
	for _, r := range f.riskEntries() {
		checkGlob(p, r.field, r.pattern)
		for _, pr := range protected {
			if patternsOverlap(r.pattern, pr.pattern) {
				p.add(r.field, "%q overlaps %q in %s: a path cannot be both protected and a risk path", r.pattern, pr.pattern, pr.field)
				break
			}
		}
	}
}

// CheckTaskProtectedPaths checks a task's protectedPaths against the risk
// paths of the factory file with the same overlap rule as the factory file
// uses. The task and the factory are in different files, so the caller must
// run this when it has loaded both.
func CheckTaskProtectedPaths(t *Task, f *Factory) error {
	var p problems
	risks := f.riskEntries()
	for i, g := range t.Spec.ProtectedPaths {
		for _, r := range risks {
			if patternsOverlap(g, r.pattern) {
				p.add(fmt.Sprintf("spec.protectedPaths[%d]", i),
					"%q overlaps %q in %s of the factory file: a path cannot be both protected and a risk path", g, r.pattern, r.field)
				break
			}
		}
	}
	return p.err()
}

// literalPrefix is the part of a pattern before its first glob character.
func literalPrefix(pat string) string {
	if i := strings.IndexAny(pat, "*?["); i >= 0 {
		return pat[:i]
	}
	return pat
}

// patternsOverlap reports whether two path patterns can match the same path.
// It is a prefix rule, not a full glob intersection: two patterns overlap when
// the literal prefix of one (the part before its first glob character, without
// a trailing slash) is a path prefix of the literal prefix of the other, on a
// whole-segment boundary. Limits: a pattern that starts with a glob character,
// such as **/*_test.go or *.md, has no literal prefix and is compared with
// other patterns by exact string only. A prefix that ends inside a segment
// (go.* against go.mod) is not seen as overlapping. So the rule catches the
// usual mistakes and is not a proof that two lists are disjoint.
func patternsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	pa := strings.TrimRight(literalPrefix(a), "/")
	pb := strings.TrimRight(literalPrefix(b), "/")
	if pa == "" || pb == "" {
		return false
	}
	return pa == pb || strings.HasPrefix(pb, pa+"/") || strings.HasPrefix(pa, pb+"/")
}

// factorySpecWire is FactorySpec as written. ProtectedPaths is a pointer so
// that an explicit empty list is kept and only a nil list is left out. Without
// this, omitempty would drop an empty list and a re-parse would bring back the
// default.
type factorySpecWire struct {
	Repository               string    `json:"repository" yaml:"repository"`
	DefaultWorker            Worker    `json:"defaultWorker" yaml:"defaultWorker"`
	MaxOpenAgentPullRequests int       `json:"maxOpenAgentPullRequests,omitempty" yaml:"maxOpenAgentPullRequests,omitempty"`
	PollInterval             string    `json:"pollInterval,omitempty" yaml:"pollInterval,omitempty"`
	AcceptancePath           string    `json:"acceptancePath,omitempty" yaml:"acceptancePath,omitempty"`
	Start                    string    `json:"start,omitempty" yaml:"start,omitempty"`
	ProtectedPaths           *[]string `json:"protectedPaths,omitempty" yaml:"protectedPaths,omitempty"`
	RiskPaths                RiskPaths `json:"riskPaths,omitempty" yaml:"riskPaths,omitempty"`
}

func (s FactorySpec) wire() factorySpecWire {
	w := factorySpecWire{
		Repository: s.Repository, DefaultWorker: s.DefaultWorker,
		MaxOpenAgentPullRequests: s.MaxOpenAgentPullRequests, PollInterval: s.PollInterval,
		AcceptancePath: s.AcceptancePath, Start: s.Start, RiskPaths: s.RiskPaths,
	}
	if s.ProtectedPaths != nil {
		w.ProtectedPaths = &s.ProtectedPaths
	}
	return w
}

// MarshalJSON keeps an explicit empty protectedPaths list.
func (s FactorySpec) MarshalJSON() ([]byte, error) { return json.Marshal(s.wire()) }

// MarshalYAML keeps an explicit empty protectedPaths list.
func (s FactorySpec) MarshalYAML() (any, error) { return s.wire(), nil }

// ValidateFactoryAndTasks validates a factory file together with the task
// files of the same repository. It runs the factory's own checks, each task's
// own checks, and the check of every task's protectedPaths against the risk
// paths of the factory (CheckTaskProtectedPaths). It also rejects two tasks
// with one name. Every error from a task starts with tasks.<task name>. The
// factory and the tasks must have their defaults applied. The orchestrator
// calls this when it loads .factory/.
func ValidateFactoryAndTasks(f *Factory, tasks []*Task) error {
	var p problems
	var fve ValidationErrors
	if errors.As(f.Validate(), &fve) {
		p.list = append(p.list, fve...)
	}
	seen := map[string]bool{}
	for i, t := range tasks {
		if t == nil {
			p.add(fmt.Sprintf("tasks[%d]", i), "must not be nil")
			continue
		}
		prefix := "tasks." + t.Metadata.Name
		if seen[t.Metadata.Name] {
			p.add(prefix+".metadata.name", "duplicate task name %q", t.Metadata.Name)
		}
		seen[t.Metadata.Name] = true
		p.addNested(prefix, t.Validate())
		p.addNested(prefix, CheckTaskProtectedPaths(t, f))
	}
	return p.err()
}
