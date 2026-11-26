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
  acceptancePath: acceptance
  start: "make run"
  protectedPaths: [".github/**", "Makefile"]
  riskPaths:
    contract: ["api/**"]
    security: ["internal/auth/**"]
    dependencies: ["go.mod", "go.sum"]
    documents: ["docs/**", "*.md"]
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
		{"acceptance path parent", func(f *Factory) { f.Spec.AcceptancePath = "../x" }, "spec.acceptancePath: "},
		{"acceptance path absolute", func(f *Factory) { f.Spec.AcceptancePath = "/x" }, "spec.acceptancePath: "},
		{"acceptance path dot", func(f *Factory) { f.Spec.AcceptancePath = "." }, "spec.acceptancePath: "},
		{"acceptance path nested ok", func(f *Factory) { f.Spec.AcceptancePath = "tests/acceptance" }, ""},
		{"acceptance path dot segment", func(f *Factory) { f.Spec.AcceptancePath = "./acceptance" }, "spec.acceptancePath: "},
		{"acceptance path trailing slash", func(f *Factory) { f.Spec.AcceptancePath = "acceptance/" }, "spec.acceptancePath: "},
		{"acceptance path doubled slash", func(f *Factory) { f.Spec.AcceptancePath = "a//b" }, "spec.acceptancePath: "},
		{"protected absolute", func(f *Factory) { f.Spec.ProtectedPaths = []string{"/etc/**"} }, "spec.protectedPaths[0]: "},
		{"protected parent", func(f *Factory) { f.Spec.ProtectedPaths = []string{"a/../../b/**"} }, "spec.protectedPaths[0]: "},
		{"protected dot segment", func(f *Factory) { f.Spec.ProtectedPaths = []string{"./.github/**"} }, "spec.protectedPaths[0]: "},
		{"risk absolute", func(f *Factory) { f.Spec.RiskPaths.Security = []string{"/etc/**"} }, "spec.riskPaths.security[0]: "},
		{"risk parent", func(f *Factory) { f.Spec.RiskPaths.Dependencies = []string{"../go.mod"} }, "spec.riskPaths.dependencies[0]: "},
		{"risk dot segment", func(f *Factory) { f.Spec.RiskPaths.Contract = []string{"./api/**"} }, "spec.riskPaths.contract[0]: "},
		{"bad protected glob", func(f *Factory) { f.Spec.ProtectedPaths = []string{"[a"} }, "spec.protectedPaths[0]: "},
		{"empty protected glob", func(f *Factory) { f.Spec.ProtectedPaths = []string{""} }, "spec.protectedPaths[0]: "},
		{"bad risk glob", func(f *Factory) { f.Spec.RiskPaths.Contract = []string{"[a"} }, "spec.riskPaths.contract[0]: "},
		{"empty risk glob", func(f *Factory) { f.Spec.RiskPaths.Documents = []string{""} }, "spec.riskPaths.documents[0]: "},
		{"github moved to security", func(f *Factory) {
			f.Spec.ProtectedPaths = []string{}
			f.Spec.RiskPaths.Security = append(f.Spec.RiskPaths.Security, ".github/**")
		}, ""},
		{"same path protected and risk", func(f *Factory) {
			f.Spec.RiskPaths.Security = append(f.Spec.RiskPaths.Security, ".github/**")
		}, `spec.riskPaths.security[1]: ".github/**" is also in spec.protectedPaths[0]`},
		{"risk path in fixed factory dir", func(f *Factory) {
			f.Spec.RiskPaths.Contract = []string{".factory/**"}
		}, `spec.riskPaths.contract[0]: ".factory/**" is also in the fixed protected paths`},
		{"risk path in acceptance path", func(f *Factory) {
			f.Spec.RiskPaths.Documents = []string{"acceptance/**"}
		}, `spec.riskPaths.documents[0]: "acceptance/**" is also in the fixed protected paths`},
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

func TestFactoryNewFieldsParse(t *testing.T) {
	f := validFactory()
	if f.Spec.AcceptancePath != "acceptance" || f.Spec.Start != "make run" {
		t.Errorf("spec: %+v", f.Spec)
	}
	r := f.Spec.RiskPaths
	if r.Contract[0] != "api/**" || r.Security[0] != "internal/auth/**" || len(r.Dependencies) != 2 || len(r.Documents) != 2 {
		t.Errorf("riskPaths: %+v", r)
	}
	_, err := ParseFactory([]byte(validFactoryYAML + "  riskPaths: {}\n"))
	if err == nil {
		t.Error("duplicate key must fail")
	}
	_, err = ParseFactory([]byte(strings.Replace(validFactoryYAML, "    contract:", "    tests: [\"x\"]\n    contract:", 1)))
	if err == nil || !strings.Contains(err.Error(), "tests") {
		t.Errorf("unknown risk kind must fail, got %v", err)
	}
}

func TestFactoryNewDefaults(t *testing.T) {
	min := "apiVersion: " + APIVersion + `
kind: Factory
metadata: { name: main }
spec:
  repository: acme/widgets
  defaultWorker: { harness: claude-code }
`
	f, err := ParseFactory([]byte(min))
	if err != nil {
		t.Fatal(err)
	}
	if f.Spec.AcceptancePath != DefaultAcceptancePath || DefaultAcceptancePath != "acceptance" {
		t.Errorf("acceptancePath = %q", f.Spec.AcceptancePath)
	}
	if len(f.Spec.ProtectedPaths) != 1 || f.Spec.ProtectedPaths[0] != ".github/**" {
		t.Errorf("protectedPaths = %v", f.Spec.ProtectedPaths)
	}
	// An explicit empty list removes the default.
	f, err = ParseFactory([]byte(min + "  protectedPaths: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Spec.ProtectedPaths) != 0 {
		t.Errorf("explicit empty list must stay empty: %v", f.Spec.ProtectedPaths)
	}
}

func TestFixedProtectedPaths(t *testing.T) {
	tests := []struct {
		acceptance string
		want       []string
	}{
		{"acceptance", []string{".factory/**", "acceptance/**"}},
		{"tests/acceptance", []string{".factory/**", "tests/acceptance/**"}},
		{"", []string{".factory/**", "acceptance/**"}},
	}
	for _, tt := range tests {
		f := validFactory()
		f.Spec.AcceptancePath = tt.acceptance
		got := f.FixedProtectedPaths()
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("acceptancePath %q: got %v, want %v", tt.acceptance, got, tt.want)
		}
	}
}
