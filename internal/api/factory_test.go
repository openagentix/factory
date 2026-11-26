package api

import (
	"strings"
	"testing"
)

const validFactoryYAML = "apiVersion: " + APIVersion + `
kind: Factory
metadata:
  name: main
spec:
  repository: acme/widgets
  defaultWorker:
    harness: claude-code
  maxOpenAgentPullRequests: 3
  pollInterval: 2m
`

func validFactory() *Factory {
	f, err := ParseFactory([]byte(validFactoryYAML))
	if err != nil {
		panic(err)
	}
	return f
}

func TestParseFactory_Valid(t *testing.T) {
	f, err := ParseFactory([]byte(validFactoryYAML))
	if err != nil {
		t.Fatalf("ParseFactory: %v", err)
	}
	if f.Spec.Repository != "acme/widgets" || f.Spec.MaxOpenAgentPullRequests != 3 || f.Spec.PollInterval != "2m" {
		t.Errorf("unexpected spec: %+v", f.Spec)
	}
}

func TestParseFactory_Strict(t *testing.T) {
	_, err := ParseFactory([]byte(validFactoryYAML + "extra: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("err = %v", err)
	}
	_, err = ParseFactory(nil)
	if err == nil {
		t.Fatal("empty input must fail")
	}
}

func TestFactoryValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Factory)
		wantErr string
	}{
		{"valid", func(*Factory) {}, ""},
		{"wrong apiVersion", func(f *Factory) { f.APIVersion = "x/v1" }, "apiVersion: "},
		{"wrong kind", func(f *Factory) { f.Kind = "Task" }, "kind: must be Factory"},
		{"bad name", func(f *Factory) { f.Metadata.Name = "Bad_Name" }, "metadata.name: "},
		{"no repository", func(f *Factory) { f.Spec.Repository = "" }, "spec.repository: "},
		{"repository without owner", func(f *Factory) { f.Spec.Repository = "widgets" }, "spec.repository: "},
		{"repository with extra part", func(f *Factory) { f.Spec.Repository = "a/b/c" }, "spec.repository: "},
		{"repository with space", func(f *Factory) { f.Spec.Repository = "a/b c" }, "spec.repository: "},
		{"dotted repository ok", func(f *Factory) { f.Spec.Repository = "my-org/repo.name_1" }, ""},
		{"bad harness", func(f *Factory) { f.Spec.DefaultWorker.Harness = "" }, "spec.defaultWorker.harness: "},
		{"negative cap", func(f *Factory) { f.Spec.MaxOpenAgentPullRequests = -1 }, "spec.maxOpenAgentPullRequests: "},
		{"bad poll interval", func(f *Factory) { f.Spec.PollInterval = "often" }, "spec.pollInterval: "},
		{"zero poll interval", func(f *Factory) { f.Spec.PollInterval = "0s" }, "spec.pollInterval: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := validFactory()
			tt.mutate(f)
			err := f.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFactoryDefaults(t *testing.T) {
	f := validFactory()
	f.Spec.MaxOpenAgentPullRequests = 0
	f.Spec.PollInterval = ""
	f.ApplyDefaults()
	if f.Spec.MaxOpenAgentPullRequests != DefaultMaxOpenAgentPullRequests || f.Spec.PollInterval != DefaultPollInterval {
		t.Errorf("defaults not applied: %+v", f.Spec)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}
