package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

type stubEnv struct {
	ws, out string
}

func newStubEnv(t *testing.T) stubEnv {
	t.Helper()
	d := t.TempDir()
	e := stubEnv{ws: filepath.Join(d, "work"), out: filepath.Join(d, "out")}
	for _, p := range []string{e.ws, e.out, filepath.Join(e.ws, "old")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.ws, "old", "gone.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e stubEnv) run(t *testing.T, script string) (int, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	code, err := RunStub(context.Background(), StubOptions{
		Script: []byte(script), Workspace: e.ws, OutDir: e.out, Stdout: &stdout,
		Now: func() time.Time { return time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC) },
	})
	return code, stdout.String(), err
}

const goodResult = `{"schemaVersion":1,"status":"succeeded","summary":"Added a file.","harness":{"name":"stub"}}`

func TestStubActsAsAWorker(t *testing.T) {
	e := newStubEnv(t)
	script := `{
	  "files": [{"path":"pkg/a.txt","content":"hello"}],
	  "delete": ["old/gone.txt"],
	  "events": [{"kind":"message","text":"working"},{"kind":"tool","text":"Write: pkg/a.txt"}],
	  "result": ` + goodResult + `
	}`
	code, stdout, err := e.run(t, script)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.ws, "pkg", "a.txt")); string(b) != "hello" {
		t.Errorf("file = %q", b)
	}
	if _, err := os.Stat(filepath.Join(e.ws, "old", "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("file should be deleted: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(e.out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.ParseResult(data); err != nil {
		t.Errorf("result.json must parse strictly: %v", err)
	}
	ev, _ := os.ReadFile(filepath.Join(e.out, "events.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(ev)), "\n")
	if len(lines) != 2 {
		t.Fatalf("events.jsonl lines = %d", len(lines))
	}
	for _, l := range lines {
		var x api.Event
		if err := json.Unmarshal([]byte(l), &x); err != nil || !x.ValidKind() || x.Time.IsZero() {
			t.Errorf("bad event line %q: %v", l, err)
		}
	}
	if _, err := os.Stat(filepath.Join(e.ws, ".git")); !os.IsNotExist(err) {
		t.Error("the stub must never create git state")
	}
	// The adapter reads what the stub printed.
	sum, err := NewStub("oxf").Read(strings.NewReader(stdout), func(api.Event) {})
	if err != nil || !sum.Complete || sum.Failed {
		t.Errorf("read: %+v %v", sum, err)
	}
}

func TestStubDefaultsAndStreamRoundTrip(t *testing.T) {
	e := newStubEnv(t)
	script := `{
	  "events": [{"kind":"message","text":"hi"}],
	  "denials": [{"request":"Bash: sed -i x","reason":"not allowed"}],
	  "usage": {"billing":"plan","inputTokens":10,"outputTokens":5}
	}`
	code, stdout, err := e.run(t, script)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	data, _ := os.ReadFile(filepath.Join(e.out, "result.json"))
	res, err := api.ParseResult(data)
	if err != nil || res.Status != api.StatusSucceeded && res.Status != api.StatusNoChange {
		t.Fatalf("default result = %+v, %v", res, err)
	}
	if res.Usage.InputTokens != 10 || res.Harness.Name != "stub" {
		t.Errorf("result = %+v", res)
	}
	var events []api.Event
	sum, err := NewStub("oxf").Read(strings.NewReader(stdout), func(e api.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if sum.Usage.Billing != api.BillingPlan || sum.Usage.OutputTokens != 5 || len(sum.Denials) != 1 || !sum.Complete {
		t.Errorf("summary = %+v", sum)
	}
	if len(events) == 0 || events[0].Text != "hi" {
		t.Errorf("events = %+v", events)
	}
}

func TestStubBehaviours(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		wantCode   int
		wantErr    bool
		wantResult bool
		wantFile   string // relative to workspace
	}{
		{"exit non-zero still writes outputs", `{"exitCode":3,"result":` + goodResult + `}`, 3, false, true, ""},
		{"touch a protected path", `{"touchProtected":".factory/task.yaml","result":` + goodResult + `}`, 0, false, true, ".factory/task.yaml"},
		{"no result file", `{"omitResult":true}`, 0, false, false, ""},
		{"unknown script field", `{"surprise":1}`, 2, true, false, ""},
		{"not json", `nope`, 2, true, false, ""},
		{"path escapes the workspace", `{"files":[{"path":"../evil","content":"x"}]}`, 2, true, false, ""},
		{"absolute path", `{"files":[{"path":"/etc/evil","content":"x"}]}`, 2, true, false, ""},
		{"git state is off limits", `{"files":[{"path":".git/index.lock","content":"x"}]}`, 2, true, false, ""},
		{"delete outside", `{"delete":["../x"]}`, 2, true, false, ""},
		{"invalid result is rejected", `{"result":{"schemaVersion":9,"status":"x"}}`, 2, true, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newStubEnv(t)
			code, _, err := e.run(t, tc.script)
			if (err != nil) != tc.wantErr || code != tc.wantCode {
				t.Fatalf("code=%d err=%v", code, err)
			}
			_, statErr := os.Stat(filepath.Join(e.out, "result.json"))
			if (statErr == nil) != tc.wantResult {
				t.Errorf("result.json present = %v, want %v", statErr == nil, tc.wantResult)
			}
			if tc.wantFile != "" {
				if _, err := os.Stat(filepath.Join(e.ws, tc.wantFile)); err != nil {
					t.Errorf("expected %s: %v", tc.wantFile, err)
				}
			}
		})
	}
}

func TestStubDelayStopsWhenCancelled(t *testing.T) {
	e := newStubEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	code, err := RunStub(ctx, StubOptions{
		Script:    []byte(`{"delayMs":60000,"files":[{"path":"late.txt","content":"x"}],"result":` + goodResult + `}`),
		Workspace: e.ws, OutDir: e.out, Stdout: &bytes.Buffer{},
	})
	if err == nil || code == 0 {
		t.Errorf("code=%d err=%v", code, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the delay ignored the context")
	}
	if _, err := os.Stat(filepath.Join(e.ws, "late.txt")); !os.IsNotExist(err) {
		t.Error("a cancelled stub must not act")
	}
}

func TestStubDelayWaits(t *testing.T) {
	e := newStubEnv(t)
	start := time.Now()
	code, _, err := e.run(t, `{"delayMs":40,"result":`+goodResult+`}`)
	if err != nil || code != 0 || time.Since(start) < 40*time.Millisecond {
		t.Errorf("code=%d err=%v elapsed=%v", code, err, time.Since(start))
	}
}

func TestStubCommand(t *testing.T) {
	s := baseSpec()
	s.StubScript = "/in/script.json"
	cmd, err := NewStub("/bin/oxf").Command(s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stub-harness", "--script=/in/script.json", "--workspace=/work", "--out=/factory/out"}
	if cmd.Program != "/bin/oxf" || strings.Join(cmd.Args, " ") != strings.Join(want, " ") || cmd.StdinFile != "" {
		t.Errorf("command = %+v", cmd)
	}
	s.StubScript = ""
	if _, err := NewStub("oxf").Command(s); err == nil {
		t.Error("want an error with no script")
	}
}

func TestStubReadIncompleteWithoutFinal(t *testing.T) {
	in := `{"type":"event","event":{"time":"2026-11-26T09:00:00Z","kind":"message","text":"a"}}` + "\nnot json\n"
	sum, err := NewStub("oxf").Read(strings.NewReader(in), func(api.Event) {})
	if err != nil || sum.Complete || sum.Skipped != 1 {
		t.Errorf("sum=%+v err=%v", sum, err)
	}
}

func TestStubRefusesWritesThroughASymlink(t *testing.T) {
	e := newStubEnv(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(e.ws, "link")); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		`{"files":[{"path":"link/evil.txt","content":"x"}]}`,
		`{"files":[{"path":"link/deeper/evil.txt","content":"x"}]}`,
		`{"touchProtected":"link/evil.txt"}`,
	} {
		code, _, err := e.run(t, script)
		if err == nil || code == 0 {
			t.Errorf("%s: code=%d err=%v", script, code, err)
		}
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("something was written outside the workspace: %v", ents)
	}
}

func TestStubDeleteNeverFollowsALink(t *testing.T) {
	e := newStubEnv(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.ws, "link")); err != nil {
		t.Fatal(err)
	}
	if code, _, err := e.run(t, `{"delete":["link"],"result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, err := os.Lstat(filepath.Join(e.ws, "link")); !os.IsNotExist(err) {
		t.Error("the link itself should be removed")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Errorf("the target of the link must survive: %v", err)
	}
}

func TestStubDeleteThroughALinkedParentIsRefused(t *testing.T) {
	e := newStubEnv(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.ws, "link")); err != nil {
		t.Fatal(err)
	}
	if code, _, err := e.run(t, `{"delete":["link/keep.txt"]}`); err == nil || code == 0 {
		t.Errorf("code=%d err=%v", code, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Errorf("file outside was deleted: %v", err)
	}
}

func TestStubRefusesTheWorkspaceRootAndGitInAnyCase(t *testing.T) {
	for _, script := range []string{
		`{"delete":["."]}`, `{"delete":[""]}`, `{"delete":["a/.."]}`, `{"files":[{"path":".","content":"x"}]}`,
		`{"files":[{"path":".GIT/config","content":"x"}]}`, `{"files":[{"path":"a/.Git/x","content":"x"}]}`,
		`{"delete":[".gIt"]}`, `{"touchProtected":".GIT/hooks/pre-commit"}`,
	} {
		e := newStubEnv(t)
		code, _, err := e.run(t, script)
		if err == nil || code == 0 {
			t.Errorf("%s: code=%d err=%v", script, code, err)
		}
		if _, err := os.Stat(filepath.Join(e.ws, "old", "gone.txt")); err != nil {
			t.Errorf("%s: the workspace was damaged: %v", script, err)
		}
		if _, err := os.Stat(filepath.Join(e.ws, ".GIT")); err == nil {
			t.Errorf("%s: .GIT was written", script)
		}
	}
}

func TestStubOutputFilesDoNotFollowLinks(t *testing.T) {
	e := newStubEnv(t)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.out, "result.json")); err != nil {
		t.Fatal(err)
	}
	if code, _, err := e.run(t, `{"result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Errorf("a link in the out directory was followed: %q", b)
	}
}

func TestStubDirectoriesMustBeAbsoluteAndExist(t *testing.T) {
	e := newStubEnv(t)
	for _, o := range []StubOptions{
		{Script: []byte(`{}`), Workspace: "rel", OutDir: e.out},
		{Script: []byte(`{}`), Workspace: e.ws, OutDir: "rel"},
		{Script: []byte(`{}`), Workspace: filepath.Join(e.ws, "missing"), OutDir: e.out},
		{Script: []byte(`{}`), Workspace: "/", OutDir: e.out},
	} {
		if code, err := RunStub(context.Background(), o); err == nil || code == 0 {
			t.Errorf("%+v: code=%d err=%v", o, code, err)
		}
	}
}

func TestStubCommandAllowsTheStub(t *testing.T) {
	s := baseSpec()
	s.StubScript = "/in/script.json"
	cmd, err := NewStub("oxf").Command(s)
	if err != nil {
		t.Fatal(err)
	}
	if index(cmd.Env, "OXF_ALLOW_STUB=1") < 0 {
		t.Errorf("env = %v", cmd.Env)
	}
}

func TestStubRefusesALinkIntoGitState(t *testing.T) {
	e := newStubEnv(t)
	hooks := filepath.Join(e.ws, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.ws, ".git"), filepath.Join(e.ws, "g")); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		`{"files":[{"path":"g/hooks/pre-commit","content":"evil"}]}`,
		`{"delete":["g/hooks"]}`,
		`{"touchProtected":"g/config"}`,
	} {
		if code, _, err := e.run(t, script); err == nil || code == 0 {
			t.Errorf("%s: code=%d err=%v", script, code, err)
		}
	}
	if _, err := os.Stat(filepath.Join(hooks, "pre-commit")); err == nil {
		t.Error("a hook was written into .git")
	}
	if _, err := os.Stat(hooks); err != nil {
		t.Error("git state was deleted")
	}
}

func TestStubRefusesAHardLinkedFile(t *testing.T) {
	e := newStubEnv(t)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(e.ws, "hl")); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	if code, _, err := e.run(t, `{"files":[{"path":"hl","content":"overwritten"}]}`); err == nil || code == 0 {
		t.Errorf("code=%d err=%v", code, err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Errorf("the file outside was changed: %q", b)
	}
}

func TestStubWriteReplacesALinkInsteadOfFollowingIt(t *testing.T) {
	e := newStubEnv(t)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.ws, "sl")); err != nil {
		t.Fatal(err)
	}
	if code, _, err := e.run(t, `{"files":[{"path":"sl","content":"new"}],"result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Errorf("the link was followed: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(e.ws, "sl")); string(b) != "new" {
		t.Errorf("sl = %q", b)
	}
	if ents, _ := filepath.Glob(filepath.Join(e.ws, ".stub-*")); len(ents) != 0 {
		t.Errorf("temporary files left behind: %v", ents)
	}
}
