package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func TestHappyRun(t *testing.T) {
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"files":[{"path":"pkg/a.txt","content":"hello"}],
	  "events":[{"kind":"message","text":"working"},{"kind":"tool","text":"Write: pkg/a.txt"}],
	  "usage":{"billing":"plan","inputTokens":10,"outputTokens":5},
	  "denials":[{"request":"Bash: x","reason":"no"}],
	  "result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	o := f.do(spec)
	if o.Failed() || o.Reason != "" {
		t.Fatalf("failed: %s", o.Reason)
	}
	if o.Result == nil || o.Result.Status != api.StatusSucceeded || o.ExitCode != 0 || o.TimedOut || o.Cancelled {
		t.Fatalf("outcome = %+v", o)
	}
	if !o.Changed || o.Commit == "" || o.Commit == f.base {
		t.Errorf("commit = %q changed = %v", o.Commit, o.Changed)
	}
	if strings.Join(ev.texts, ",") != "working,Write: pkg/a.txt" || o.EventCount != 2 {
		t.Errorf("events = %v (%d)", ev.texts, o.EventCount)
	}
	if !o.Summary.Complete || o.Summary.Usage.InputTokens != 10 || len(o.Summary.Denials) != 1 {
		t.Errorf("summary = %+v", o.Summary)
	}
	if d := runDirs(f); len(d) != 0 {
		t.Errorf("run directory left behind: %v", d)
	}
	// The new commit holds the file and the old one, on the same parent.
	dest := filepath.Join(t.TempDir(), "x")
	if _, err := f.repo.Export(context.Background(), o.Commit, dest, repoLimits()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "pkg", "a.txt")); string(b) != "hello" {
		t.Errorf("pkg/a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dest, "main.go")); err != nil {
		t.Errorf("main.go: %v", err)
	}
	// Nothing from the runner's own directories is in the commit.
	for _, n := range []string{"in", "out", "tmp"} {
		if _, err := os.Stat(filepath.Join(dest, n)); err == nil {
			t.Errorf("%s leaked into the commit", n)
		}
	}
}

func TestNoChange(t *testing.T) {
	f := newFixture(t)
	o := f.do(f.spec(`{"result":` + goodResult + `}`))
	if o.Failed() || o.Changed || o.Commit != f.base {
		t.Errorf("outcome = %+v", o)
	}
}

func TestNonZeroExitFailsTheRunAndKeepsItOnRequest(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"exitCode":3,"files":[{"path":"a","content":"x"}],"result":` + goodResult + `}`)
	spec.KeepFailed = true
	o := f.do(spec)
	if !o.Failed() || o.ExitCode != 3 || o.Commit != "" {
		t.Fatalf("outcome = %+v", o)
	}
	if o.RunDir == "" {
		t.Fatal("the failed run must be kept")
	}
	fi, err := os.Stat(o.RunDir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("run dir: %v %v", fi, err)
	}
	for _, d := range []string{"workspace", "in", "out"} {
		if _, err := os.Stat(filepath.Join(o.RunDir, d)); err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}
	if _, err := os.Stat(filepath.Join(o.RunDir, "workspace", ".git")); err == nil {
		t.Error("the workspace must have no .git")
	}
	// Not kept by default.
	f2 := newFixture(t)
	o = f2.do(f2.spec(`{"exitCode":3,"result":` + goodResult + `}`))
	if o.RunDir != "" || len(runDirs(f2)) != 0 {
		t.Errorf("a failed run was kept without the option: %+v", runDirs(f2))
	}
}

func TestInputFiles(t *testing.T) {
	f := newFixture(t)
	spec := f.spec(`{"dumpEnv":true,"result":` + goodResult + `}`)
	spec.KeepFailed = true
	spec.Harness.Commands = []string{"go test"}
	spec.Prompt.Trigger = "ticket text"
	// A failing exit keeps the run directory so the files can be read.
	spec = withScript(t, f, spec, `{"exitCode":1,"result":`+goodResult+`}`)
	o := f.do(spec)
	in := filepath.Join(o.RunDir, "in")
	for _, n := range []string{"task.json", "prompt.md", "trigger.json"} {
		fi, err := os.Stat(filepath.Join(in, n))
		if err != nil || fi.Mode().Perm() != 0o400 {
			t.Errorf("%s: %v %v", n, fi, err)
		}
	}
	if fi, _ := os.Stat(in); fi.Mode().Perm() != 0o500 {
		t.Errorf("in dir mode = %v", fi.Mode().Perm())
	}
	var task api.Task
	b, _ := os.ReadFile(filepath.Join(in, "task.json"))
	if err := json.Unmarshal(b, &task); err != nil || task.Kind != "Task" {
		t.Errorf("task.json: %v %s", err, b)
	}
	p, _ := os.ReadFile(filepath.Join(in, "prompt.md"))
	if !strings.Contains(string(p), "go test") || !strings.Contains(string(p), "ticket text") || !strings.Contains(string(p), filepath.Join(o.RunDir, "out")) {
		t.Errorf("prompt.md = %s", p)
	}
	if b, _ := os.ReadFile(filepath.Join(in, "trigger.json")); string(b) != `{"issue":7}` {
		t.Errorf("trigger.json = %s", b)
	}
}

// withScript replaces the stub script of a spec.
func withScript(t *testing.T, f *fixture, s Spec, script string) Spec {
	t.Helper()
	n := f.spec(script)
	s.Harness.StubScript = n.Harness.StubScript
	return s
}

func TestEnvironmentIsAnAllowlist(t *testing.T) {
	f := newFixture(t)
	var ev events
	spec := f.spec(`{"dumpEnv":true,"result":` + goodResult + `}`)
	spec.OnEvent = ev.on
	spec.Environ = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=/home/someone", "USER=someone", "LOGNAME=someone",
		"LANG=en_US.UTF-8", "TERM=xterm", "SHELL=/bin/zsh",
		"GITHUB_TOKEN=ghp_secret", "GH_TOKEN=x", "GIT_ASKPASS=/bin/x", "SSH_AUTH_SOCK=/tmp/agent",
		"ANTHROPIC_API_KEY=sk-secret", "OPENAI_API_KEY=sk-other", "MY_TOKEN=t", "AWS_SECRET_ACCESS_KEY=a",
		"TMPDIR=/should/not/pass", "FACTORY_OUT=/evil",
	}
	spec.KeepFailed = true
	o := f.do(spec)
	got := envSeen(ev.texts)
	want := []string{
		"HOME=/home/someone", "LANG=en_US.UTF-8", "LOGNAME=someone", "OXF_ALLOW_STUB=1", "OXF_STUB_ATTACK_ROOT=" + filepath.Dir(filepath.Dir(f.data)), "PATH=" + os.Getenv("PATH"),
		"SHELL=/bin/zsh", "TERM=xterm", "USER=someone",
	}
	var checked []string
	for _, kv := range got {
		switch {
		case strings.HasPrefix(kv, "TMPDIR="):
			if !strings.HasPrefix(strings.TrimPrefix(kv, "TMPDIR="), "/") || !strings.HasSuffix(kv, "/tmp") {
				t.Errorf("TMPDIR = %q", kv)
			}
		case strings.HasPrefix(kv, "FACTORY_IN="), strings.HasPrefix(kv, "FACTORY_OUT="), strings.HasPrefix(kv, "FACTORY_DEADLINE_SECONDS="):
			checked = append(checked, kv)
		default:
			checked = append(checked, kv)
		}
	}
	_ = o
	var plain []string
	for _, kv := range checked {
		if !strings.HasPrefix(kv, "FACTORY_") {
			plain = append(plain, kv)
		}
	}
	sort.Strings(plain)
	sort.Strings(want)
	if strings.Join(plain, "\n") != strings.Join(want, "\n") {
		t.Errorf("environment:\n%s\nwant:\n%s", strings.Join(plain, "\n"), strings.Join(want, "\n"))
	}
	var factory []string
	for _, kv := range checked {
		if strings.HasPrefix(kv, "FACTORY_") {
			name, _, _ := strings.Cut(kv, "=")
			factory = append(factory, name)
		}
	}
	sort.Strings(factory)
	if strings.Join(factory, ",") != "FACTORY_DEADLINE_SECONDS,FACTORY_IN,FACTORY_OUT" {
		t.Errorf("factory variables = %v", factory)
	}
	for _, kv := range got {
		if strings.Contains(kv, "secret") || strings.Contains(kv, "evil") {
			t.Errorf("a secret reached the agent: %q", kv)
		}
	}
}

func envSeen(texts []string) []string {
	var out []string
	for _, t := range texts {
		if kv, ok := strings.CutPrefix(t, "env "); ok {
			out = append(out, kv)
		}
	}
	return out
}

func TestMeteredRunPassesOnlyTheNamedKey(t *testing.T) {
	f := newFixture(t)
	environ := []string{"PATH=" + os.Getenv("PATH"), "ANTHROPIC_API_KEY=sk-named", "OPENAI_API_KEY=sk-other", "GITHUB_TOKEN=gh"}
	run := func(metered bool, keys []string) []string {
		var ev events
		spec := f.spec(`{"dumpEnv":true,"result":` + goodResult + `}`)
		spec.OnEvent = ev.on
		spec.Environ, spec.Metered, spec.KeyEnv = environ, metered, keys
		f.do(spec)
		return envSeen(ev.texts)
	}
	has := func(env []string, kv string) bool {
		for _, e := range env {
			if e == kv {
				return true
			}
		}
		return false
	}
	if env := run(false, []string{"ANTHROPIC_API_KEY"}); has(env, "ANTHROPIC_API_KEY=sk-named") {
		t.Error("a key was passed for a run that is not metered")
	}
	if env := run(true, []string{"ANTHROPIC_AUTH_TOKEN"}); len(env) == 0 {
		t.Error("a documented bearer token name must be accepted")
	}
	env := run(true, []string{"ANTHROPIC_API_KEY"})
	if !has(env, "ANTHROPIC_API_KEY=sk-named") || has(env, "OPENAI_API_KEY=sk-other") || has(env, "GITHUB_TOKEN=gh") {
		t.Errorf("metered env = %v", env)
	}
	// A key variable that names a git host secret is refused at the start.
	for _, bad := range []string{"GITHUB_TOKEN", "SSH_AUTH_SOCK", "GIT_ASKPASS", "PATH", "lower", "A=B", "FACTORY_OUT",
		"LD_PRELOAD", "BASH_ENV", "DYLD_INSERT_LIBRARIES", "AWS_SECRET_ACCESS_KEY", "NODE_OPTIONS", "HTTPS_PROXY"} {
		spec := f.spec(`{"result":` + goodResult + `}`)
		spec.Environ, spec.Metered, spec.KeyEnv = environ, true, []string{bad}
		if _, err := NewLocal().Start(context.Background(), spec); err == nil {
			t.Errorf("%q must be refused as a key variable", bad)
		}
	}
	if d := runDirs(f); len(d) != 0 {
		t.Errorf("a refused start left %v", d)
	}
}
