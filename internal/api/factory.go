package api

import "regexp"

// Defaults for optional factory fields.
const (
	DefaultMaxOpenAgentPullRequests = 3
	DefaultPollInterval             = "1m"
)

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
	return p.err()
}
