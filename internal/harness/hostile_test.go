package harness

import (
	"math"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// Every value that enters an argument list is checked. A value that starts
// with a dash would otherwise be read as a flag of the harness.
func TestHostileValuesAreRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"writable flag", func(s *Spec) { s.Writable = []string{"--permission-mode=acceptEdits"} }},
		{"writable settings", func(s *Spec) { s.Writable = []string{"--settings"} }},
		{"writable short flag", func(s *Spec) { s.Writable = []string{"-x"} }},
		{"writable relative", func(s *Spec) { s.Writable = []string{"cache"} }},
		{"writable dotdot", func(s *Spec) { s.Writable = []string{"/a/../b"} }},
		{"writable dash component", func(s *Spec) { s.Writable = []string{"/a/-b"} }},
		{"writable newline", func(s *Spec) { s.Writable = []string{"/a\nb"} }},
		{"writable unclean", func(s *Spec) { s.Writable = []string{"/a//b"} }},
		{"model flag", func(s *Spec) { s.Model = "--dangerously-skip-permissionz" }},
		{"model short flag", func(s *Spec) { s.Model = "-m" }},
		{"model space", func(s *Spec) { s.Model = "a b" }},
		{"model newline", func(s *Spec) { s.Model = "a\nb" }},
		{"model equals", func(s *Spec) { s.Model = "a=b" }},
		{"workspace flag", func(s *Spec) { s.Workspace = "-C" }},
		{"workspace relative", func(s *Spec) { s.Workspace = "work" }},
		{"out dir flag", func(s *Spec) { s.OutDir = "--add-dir=/" }},
		{"out dir relative", func(s *Spec) { s.OutDir = "out" }},
		{"in dir dash", func(s *Spec) { s.InDir = "/x/-y" }},
		{"prompt file relative", func(s *Spec) { s.PromptFile = "prompt.md" }},
		{"plugin path flag", func(s *Spec) {
			s.Method = "plugin:superpowers"
			s.PluginDirs = map[string]string{"superpowers": "--settings"}
		}},
		{"plugin name flag", func(s *Spec) { s.Method = "plugin:--settings" }},
		{"plugin name space", func(s *Spec) { s.Method = "plugin:a b" }},
		{"plugin name newline", func(s *Spec) { s.Method = "plugin:a\nb" }},
		{"method control char", func(s *Spec) { s.Method = "prompts/x\x00.md" }},
		{"plugin map key", func(s *Spec) { s.PluginDirs = map[string]string{"--x": "/p"} }},
		{"billing unknown word", func(s *Spec) { s.Billing = "free" }},
		{"cost NaN", func(s *Spec) { s.Billing, s.MaxCostUSD = api.BillingMetered, math.NaN() }},
		{"cost infinite", func(s *Spec) { s.Billing, s.MaxCostUSD = api.BillingMetered, math.Inf(1) }},
		{"cost negative", func(s *Spec) { s.MaxCostUSD = -1 }},
		{"stub script flag", func(s *Spec) { s.StubScript = "--script" }},
	}
	for _, a := range allAdapters() {
		for _, tc := range tests {
			t.Run(a.Name()+"/"+tc.name, func(t *testing.T) {
				s := baseSpec()
				s.StubScript = "/in/script.json"
				tc.mutate(&s)
				if tc.name == "stub script flag" && a.Name() != "stub" {
					t.Skip("only the stub uses a script")
				}
				if cmd, err := a.Command(s); err == nil {
					t.Fatalf("accepted: %v", cmd.Args)
				}
			})
		}
	}
}

func TestModelNamesThatAreAllowed(t *testing.T) {
	for _, m := range []string{"sonnet", "claude-sonnet-5-5", "gpt-5.1", "anthropic/claude:v1", "opus[1m]", "a_b.c"} {
		s := baseSpec()
		s.Model = m
		for _, a := range []Adapter{NewClaude(), NewCodex()} {
			if _, err := a.Command(s); err != nil {
				t.Errorf("%s refused model %q: %v", a.Name(), m, err)
			}
		}
	}
}

// Every argument that starts with a dash must be one of the flags the adapter
// is designed to emit. This catches a value read as a flag.
var designedFlags = map[string][]string{
	"claude-code": {"-p", "--output-format", "--verbose", "--permission-mode", "--allowedTools",
		"--add-dir", "--no-session-persistence", "--max-budget-usd", "--plugin-dir", "--model"},
	"codex": {"exec", "--json", "--sandbox", "--add-dir", "--ephemeral", "--output-last-message", "--model", "-"},
	"stub":  {"--script", "--workspace", "--out"},
}

func TestEveryDashedArgumentIsADesignedFlag(t *testing.T) {
	hostile := []string{"--permission-mode=acceptEdits", "--settings", "-x", "--dangerously-skip-permissionz", "--yolo2"}
	var specs []Spec
	specs = append(specs, manySpecs()...)
	for _, h := range hostile {
		for _, mut := range []func(*Spec){
			func(s *Spec) { s.Writable = []string{h} },
			func(s *Spec) { s.Model = h },
			func(s *Spec) { s.Commands = []string{h} },
			func(s *Spec) { s.Method = "plugin:" + h },
			func(s *Spec) { s.PluginDirs = map[string]string{"superpowers": h}; s.Method = "plugin:superpowers" },
			func(s *Spec) { s.Workspace = h },
			func(s *Spec) { s.OutDir = h },
		} {
			s := baseSpec()
			s.StubScript = "/in/script.json"
			mut(&s)
			specs = append(specs, s)
		}
	}
	var checked int
	for _, a := range allAdapters() {
		ok := map[string]bool{}
		for _, f := range designedFlags[a.Name()] {
			ok[f] = true
		}
		for _, s := range specs {
			cmd, err := a.Command(s)
			if err != nil {
				continue
			}
			checked++
			for _, arg := range cmd.Args {
				if !strings.HasPrefix(arg, "-") && arg != "exec" {
					continue
				}
				name, value, hasEq := strings.Cut(arg, "=")
				if !ok[name] {
					t.Fatalf("%s emitted %q, which is not a designed flag (spec %+v)", a.Name(), arg, s)
				}
				if hasEq && strings.HasPrefix(value, "-") {
					t.Fatalf("%s emitted %q: value looks like a flag", a.Name(), arg)
				}
			}
		}
	}
	if checked < 300 {
		t.Fatalf("only %d commands checked", checked)
	}
}

func TestValuesUseTheEqualsForm(t *testing.T) {
	s := baseSpec()
	s.Model, s.Writable = "sonnet", []string{"/cache"}
	s.Billing, s.MaxCostUSD = api.BillingMetered, 2
	s.Method, s.PluginDirs = "plugin:p", map[string]string{"p": "/plugins/p"}
	for _, a := range []Adapter{NewClaude(), NewCodex()} {
		cmd, err := a.Command(s)
		if err != nil {
			t.Fatal(err)
		}
		for i, arg := range cmd.Args {
			switch arg {
			case "--model", "--add-dir", "--max-budget-usd", "--plugin-dir", "--permission-mode",
				"--output-format", "--sandbox", "--output-last-message":
				t.Errorf("%s: %q at %d must be written as --flag=value", a.Name(), arg, i)
			}
		}
	}
}
