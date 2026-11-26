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
			for _, cmds := range [][]string{nil, {"go test ./..."}, {"go test ./...", "go vet ./...", "ls -la"}, {"make test"}} {
				for _, model := range []string{"", "sonnet", "gpt-x"} {
					s := baseSpec()
					s.Method, s.Billing, s.Commands, s.Model = method, billing, cmds, model
					s.MaxCostUSD = 2.5
					s.PluginDirs = map[string]string{"superpowers": "/plugins/sp", "gsd": "/plugins/gsd"}
					s.Writable = []string{"/work/cache"}
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

func TestPolicyCommandsAreValidated(t *testing.T) {
	bad := []string{
		"git status", "git", "git commit -m x", "/usr/bin/git commit", "env git push",
		"git status --output=/tmp/x", "sh", "bash -c x", "zsh -c x", "/bin/dash", "fish", "env",
		"python3 -m pytest; rm -rf ~", "a | b", "a && b", "a & b", "echo $HOME", "echo `x`",
		"x > y", "x < y", "x \"y\"", "x 'y'", "x\\y", "x\ny", "x\x00", "x\ty\x1b", "(x)", "{x}",
		"nohup x", "time x",
		"./run-tests", "../x test", "/usr/local/bin/go test", "bin/tool x", ".hidden x", "tools/make test",
		"rm", "rm -rf x", "curl http://x", "wget x", "nc x 1", "ssh host", "scp a b", "busybox sh", "npx x",
		"find . -name x", "awk x", "sed x", "perl x", "ruby x", "node x", "deno x", "bun x", "node -e x", "perl -e x",
		"python3 -c x", "go test -c", "make -e x", "x --eval y", "find . -exec x", "find . -delete", "x -exec y", "x -delete",
		"go run main.go", "go generate ./...", "Bash", "Read x", "Edit x", "Write x", "Skill x", "Bash(x)", "x(y)", "sudo x", "xargs x", "eval x", "exec x", "/usr/bin/env x", "GIT status", "", "  ",
	}
	for _, a := range allAdapters() {
		for _, c := range bad {
			s := baseSpec()
			s.StubScript = "/in/script.json"
			s.Commands = []string{c}
			if _, err := a.Command(s); err == nil {
				t.Errorf("%s accepted %q", a.Name(), c)
			}
		}
		for _, c := range []string{"go test ./...", "gofmt -l .", "python3 -m pytest -q", "make test",
			"go build ./...", "go vet ./...", "pytest", "pytest -q tests", "npm test", "npm run lint", "pnpm test", "yarn test", "cargo test", "make build"} {
			s := baseSpec()
			s.StubScript = "/in/script.json"
			s.Commands = []string{c}
			if _, err := a.Command(s); err != nil {
				t.Errorf("%s refused %q: %v", a.Name(), c, err)
			}
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
			if strings.Contains(low, strings.ToLower(w)) {
				t.Errorf("%s mentions %q", f, w)
			}
		}
	}
}

func TestPolicyLimitIsStated(t *testing.T) {
	text := PolicyLimit()
	for _, w := range []string{"not a sandbox", "code the agent wrote"} {
		if !strings.Contains(text, w) {
			t.Errorf("PolicyLimit() lacks %q: %s", w, text)
		}
	}
}
