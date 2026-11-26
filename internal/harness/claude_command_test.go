package harness

import (
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func baseSpec() Spec {
	return Spec{
		Workspace:  "/work",
		InDir:      "/factory/in",
		OutDir:     "/factory/out",
		PromptFile: "/factory/in/prompt.md",
		Commands:   []string{"go test ./...", "gofmt -l ."},
		Method:     api.MethodBuiltin,
		Deadline:   10 * time.Minute,
	}
}

func index(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestClaudeCommand(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Spec)
		has      []string
		hasNot   []string
		allowed  []string
		wantEnv  []string
		wantFail bool
	}{
		{
			name:   "defaults are print mode, stream json and dontAsk",
			mutate: func(s *Spec) {},
			has: []string{"-p", "--output-format=stream-json", "--verbose",
				"--permission-mode=dontAsk", "--add-dir=/factory/out", "--no-session-persistence"},
			hasNot:  []string{"--max-budget-usd=", "--plugin-dir=", "--model="},
			allowed: []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash(go test ./...:*)", "Bash(gofmt -l .:*)"},
			wantEnv: []string{"FACTORY_DEADLINE_SECONDS=600", "FACTORY_IN=/factory/in", "FACTORY_OUT=/factory/out"},
		},
		{
			name: "budget flag only when metered",
			mutate: func(s *Spec) {
				s.Billing = api.BillingMetered
				s.MaxCostUSD = 1.5
			},
			has:     []string{"--max-budget-usd=1.5"},
			wantEnv: []string{"FACTORY_MAX_COST_USD=1.5"},
		},
		{
			name: "no budget flag for plan billing",
			mutate: func(s *Spec) {
				s.Billing = api.BillingPlan
				s.MaxCostUSD = 1.5
			},
			hasNot: []string{"--max-budget-usd=1.5"},
		},
		{
			name: "no budget flag when billing unknown",
			mutate: func(s *Spec) {
				s.MaxCostUSD = 1.5
			},
			hasNot: []string{"--max-budget-usd=1.5"},
		},
		{
			name: "plugin method with a path loads the plugin and allows Skill",
			mutate: func(s *Spec) {
				s.Method = "plugin:superpowers"
				s.PluginDirs = map[string]string{"superpowers": "/plugins/superpowers"}
			},
			has:     []string{"--plugin-dir=/plugins/superpowers"},
			allowed: []string{"Skill"},
		},
		{
			name: "plugin method without a path adds no flag",
			mutate: func(s *Spec) {
				s.Method = "plugin:superpowers"
			},
			hasNot: []string{"--plugin-dir=/plugins/superpowers", "--plugin-dir=/plugins/other"},
		},
		{
			name: "a path for another plugin is not used",
			mutate: func(s *Spec) {
				s.Method = "plugin:superpowers"
				s.PluginDirs = map[string]string{"other": "/plugins/other"}
			},
			hasNot: []string{"--plugin-dir=/plugins/superpowers", "--plugin-dir=/plugins/other"},
		},
		{
			name:   "model",
			mutate: func(s *Spec) { s.Model = "sonnet" },
			has:    []string{"--model=sonnet"},
		},
		{
			name:   "extra writable path",
			mutate: func(s *Spec) { s.Writable = []string{"/cache"} },
			has:    []string{"--add-dir=/cache"},
		},
		{name: "missing workspace", mutate: func(s *Spec) { s.Workspace = "" }, wantFail: true},
		{name: "missing out dir", mutate: func(s *Spec) { s.OutDir = "" }, wantFail: true},
		{name: "empty command", mutate: func(s *Spec) { s.Commands = []string{" "} }, wantFail: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := baseSpec()
			tc.mutate(&s)
			cmd, err := NewClaude().Command(s)
			if tc.wantFail {
				if err == nil {
					t.Fatalf("want an error, got command %v", cmd.Args)
				}
				return
			}
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
			if cmd.Program != "claude" {
				t.Errorf("program = %q", cmd.Program)
			}
			if cmd.Dir != "/work" {
				t.Errorf("dir = %q", cmd.Dir)
			}
			if cmd.StdinFile != "/factory/in/prompt.md" {
				t.Errorf("stdin file = %q", cmd.StdinFile)
			}
			for _, h := range tc.has {
				if index(cmd.Args, h) < 0 {
					t.Errorf("args %v lack %q", cmd.Args, h)
				}
			}
			for _, h := range tc.hasNot {
				if index(cmd.Args, h) >= 0 {
					t.Errorf("args %v must not have %q", cmd.Args, h)
				}
			}
			for _, a := range tc.allowed {
				if index(cmd.Args, a) < 0 {
					t.Errorf("allowlist lacks %q in %v", a, cmd.Args)
				}
			}
			for _, e := range tc.wantEnv {
				if index(cmd.Env, e) < 0 {
					t.Errorf("env %v lacks %q", cmd.Env, e)
				}
			}
		})
	}
}

func TestClaudeAllowlistFollowsItsFlag(t *testing.T) {
	cmd, err := NewClaude().Command(baseSpec())
	if err != nil {
		t.Fatal(err)
	}
	i := index(cmd.Args, "--allowedTools")
	if i < 0 || i+1 >= len(cmd.Args) || strings.HasPrefix(cmd.Args[i+1], "-") {
		t.Fatalf("--allowedTools must be followed by tools: %v", cmd.Args)
	}
}

func TestBudgetUsesTheEqualsForm(t *testing.T) {
	s := baseSpec()
	s.Billing, s.MaxCostUSD = api.BillingMetered, 2
	cmd, _ := NewClaude().Command(s)
	if index(cmd.Args, "--max-budget-usd=2") < 0 {
		t.Fatalf("budget args wrong: %v", cmd.Args)
	}
}
