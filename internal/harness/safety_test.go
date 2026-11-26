package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

var forbiddenStrings = []string{
	"--dangerously-skip-permissions",
	"bypassPermissions",
	"--dangerously-bypass-approvals-and-sandbox",
	"danger-full-access",
}

func allAdapters() []Adapter {
	return []Adapter{NewClaude(), NewCodex(), NewStub("oxf")}
}

func manySpecs() []Spec {
	var out []Spec
	for _, method := range []string{"", api.MethodBuiltin, "plugin:superpowers", "plugin:gsd", "prompts/build.md"} {
		for _, billing := range []string{"", api.BillingMetered, api.BillingPlan, api.BillingUnknown} {
			for _, cmds := range [][]string{nil, {"go test ./..."}, {"go test ./...", "go vet ./...", "git diff", "git status"}, {"make test:*"}} {
				for _, model := range []string{"", "sonnet", "gpt-x"} {
					s := baseSpec()
					s.Method, s.Billing, s.Commands, s.Model = method, billing, cmds, model
					s.MaxCostUSD = 2.5
					s.PluginDirs = map[string]string{"superpowers": "/plugins/sp", "gsd": "/plugins/gsd"}
					s.Writable = []string{"/cache"}
					s.Deadline = time.Hour
					s.StubScript = "/in/script.json"
					out = append(out, s)
				}
			}
		}
	}
	return out
}

func TestNoCommandSkipsPermissionChecks(t *testing.T) {
	specs := manySpecs()
	var built int
	for _, a := range allAdapters() {
		for _, s := range specs {
			cmd, err := a.Command(s)
			if err != nil {
				t.Fatalf("%s: %v", a.Name(), err)
			}
			built++
			all := append([]string{cmd.Program, cmd.Dir, cmd.StdinFile}, cmd.Args...)
			all = append(all, cmd.Env...)
			for _, x := range all {
				for _, f := range forbiddenStrings {
					if strings.Contains(x, f) {
						t.Fatalf("%s built %q with %q", a.Name(), x, f)
					}
				}
			}
		}
	}
	if built < 300 {
		t.Fatalf("only %d commands built", built)
	}
}

func TestPolicyCommandWithAForbiddenFlagIsRefused(t *testing.T) {
	for _, a := range allAdapters() {
		for _, f := range forbiddenStrings {
			s := baseSpec()
			s.StubScript = "/in/script.json"
			s.Commands = []string{"claude " + f}
			cmd, err := a.Command(s)
			if a.Name() == "stub" {
				continue // the stub never puts policy text on a command line
			}
			if !errors.Is(err, ErrForbidden) && err == nil {
				t.Errorf("%s accepted %q: %v", a.Name(), f, cmd.Args)
			}
		}
	}
}

func TestPolicyCannotLetTheWorkerChangeGit(t *testing.T) {
	bad := []string{"git commit -m x", "git push", "git add .", "git", "git checkout main", "git reset --hard", "git config user.name x"}
	for _, a := range allAdapters() {
		if a.Name() == "stub" {
			continue
		}
		for _, c := range bad {
			s := baseSpec()
			s.Commands = []string{c}
			if _, err := a.Command(s); err == nil {
				t.Errorf("%s accepted %q", a.Name(), c)
			}
		}
	}
	for _, c := range []string{"git status", "git diff --stat", "git log --oneline"} {
		s := baseSpec()
		s.Commands = []string{c}
		if _, err := NewClaude().Command(s); err != nil {
			t.Errorf("read-only %q refused: %v", c, err)
		}
	}
}

func TestAdaptersAddNoGitCommand(t *testing.T) {
	s := baseSpec()
	s.Commands = nil
	s.Method = "plugin:superpowers"
	s.PluginDirs = map[string]string{"superpowers": "/p"}
	for _, a := range []Adapter{NewClaude(), NewCodex()} {
		cmd, err := a.Command(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, arg := range cmd.Args {
			if strings.Contains(arg, "git") {
				t.Errorf("%s added %q with an empty policy", a.Name(), arg)
			}
			if strings.Contains(arg, "Bash") {
				t.Errorf("%s allowed a shell with an empty policy: %q", a.Name(), arg)
			}
		}
	}
}

// The adapters start the official programs and nothing else. They must
// never name a login file or a credential store.
func TestNoSourceFileNamesACredentialStore(t *testing.T) {
	banned := []string{
		".claude/.credentials", ".credentials", ".codex/auth", "auth.json",
		"keychain", "find-generic-password", "userhomedir", "$home", "os.getenv(\"home\")",
		"oauth", "api_key", "anthropic_api_key", "openai_api_key",
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %v", files, err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // tests name the banned words, in order to ban them
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		low := strings.ToLower(string(b))
		for _, w := range banned {
			if strings.Contains(low, w) {
				t.Errorf("%s mentions %q", f, w)
			}
		}
	}
}
