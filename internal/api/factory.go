package api

import (
	"fmt"
	"path"
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
	ap := strings.TrimRight(f.Spec.AcceptancePath, "/")
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
	if ap == "" || path.Clean(ap) == "." || !relativeNoParent(ap) {
		p.add("spec.acceptancePath", "must be a relative directory below the repository root, without ..")
	}
	f.checkPaths(&p)
	return p.err()
}

// checkPaths validates the path patterns and rejects a pattern that is both
// protected and a risk path.
func (f *Factory) checkPaths(p *problems) {
	where := map[string]string{} // pattern -> where it is protected
	for _, g := range f.FixedProtectedPaths() {
		where[g] = "the fixed protected paths"
	}
	for i, g := range f.Spec.ProtectedPaths {
		field := fmt.Sprintf("spec.protectedPaths[%d]", i)
		checkGlob(p, field, g)
		if _, ok := where[g]; !ok {
			where[g] = field
		}
	}
	lists := []struct {
		name string
		list []string
	}{
		{"contract", f.Spec.RiskPaths.Contract},
		{"security", f.Spec.RiskPaths.Security},
		{"dependencies", f.Spec.RiskPaths.Dependencies},
		{"documents", f.Spec.RiskPaths.Documents},
	}
	for _, l := range lists {
		for i, g := range l.list {
			field := fmt.Sprintf("spec.riskPaths.%s[%d]", l.name, i)
			checkGlob(p, field, g)
			if w, ok := where[g]; ok {
				p.add(field, "%q is also in %s: a path cannot be both protected and a risk path", g, w)
			}
		}
	}
}

func checkGlob(p *problems, field, g string) {
	if g == "" {
		p.add(field, "must not be empty")
	} else if _, err := path.Match(g, ""); err != nil {
		p.add(field, "invalid glob pattern: %v", err)
	}
}
