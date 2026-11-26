package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// runWith runs the stub with a stderr writer too.
func (e stubEnv) runWith(t *testing.T, script string, stderr *bytes.Buffer) (int, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	code, err := RunStub(context.Background(), StubOptions{
		Script: []byte(script), Workspace: e.ws, OutDir: e.out, Stdout: &stdout, Stderr: stderr,
		Now: func() time.Time { return time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC) },
	})
	return code, stdout.String(), err
}

func TestStubFloodsOutput(t *testing.T) {
	e := newStubEnv(t)
	var stderr bytes.Buffer
	script := `{"floodStdout":3000000,"floodStderr":2000000,"longLine":1500000,"result":` + goodResult + `}`
	code, stdout, err := e.runWith(t, script, &stderr)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if len(stdout) < 3000000+1500000 {
		t.Errorf("stdout bytes = %d", len(stdout))
	}
	if stderr.Len() < 2000000 {
		t.Errorf("stderr bytes = %d", stderr.Len())
	}
	longest := 0
	for _, l := range strings.Split(stdout, "\n") {
		longest = max(longest, len(l))
	}
	if longest < 1500000 {
		t.Errorf("longest line = %d", longest)
	}
	if !strings.Contains(stdout, `"type":"final"`) {
		t.Error("the final line must still come last")
	}
}

func TestStubResultAs(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		as    string
		check func(t *testing.T, p string, fi os.FileInfo)
	}{
		{"link", func(t *testing.T, p string, fi os.FileInfo) {
			if fi.Mode()&os.ModeSymlink == 0 {
				t.Error("want a symbolic link")
			}
		}},
		{"dir", func(t *testing.T, p string, fi os.FileInfo) {
			if !fi.IsDir() {
				t.Error("want a directory")
			}
		}},
		{"fifo", func(t *testing.T, p string, fi os.FileInfo) {
			if fi.Mode()&os.ModeNamedPipe == 0 {
				t.Error("want a named pipe")
			}
		}},
		{"huge", func(t *testing.T, p string, fi os.FileInfo) {
			if fi.Size() != 500<<20 {
				t.Errorf("size = %d", fi.Size())
			}
		}},
		{"hardlink", func(t *testing.T, p string, fi os.FileInfo) {
			if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Nlink < 2 {
				t.Error("want a second hard link")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.as, func(t *testing.T) {
			e := newStubEnv(t)
			script := `{"resultAs":"` + tt.as + `","resultTarget":"` + secret + `"}`
			if code, _, err := e.run(t, script); err != nil || code != 0 {
				t.Fatalf("code=%d err=%v", code, err)
			}
			p := filepath.Join(e.out, "result.json")
			fi, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, p, fi)
		})
	}
	if b, _ := os.ReadFile(secret); string(b) != "PRIVATE" {
		t.Errorf("the secret file was changed: %q", b)
	}
	e := newStubEnv(t)
	if code, _, err := e.run(t, `{"resultAs":"bogus"}`); err == nil || code == 0 {
		t.Errorf("an unknown resultAs must fail: %d %v", code, err)
	}
}

func TestStubOutAsLink(t *testing.T) {
	e := newStubEnv(t)
	other := t.TempDir()
	script := `{"outAsLink":"` + other + `","result":` + goodResult + `}`
	if code, _, err := e.run(t, script); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	fi, err := os.Lstat(e.out)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("out must be a link: %v %v", fi, err)
	}
	if dest, _ := os.Readlink(e.out); dest != other {
		t.Errorf("link target = %q", dest)
	}
}

func TestStubForgesEvents(t *testing.T) {
	e := newStubEnv(t)
	if code, _, err := e.run(t, `{"forgeEvents":"FORGED\n","result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.out, "events.jsonl")); string(b) != "FORGED\n" {
		t.Errorf("events.jsonl = %q", b)
	}
	e = newStubEnv(t)
	target := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, err := e.run(t, `{"eventsLink":"`+target+`","result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if dest, err := os.Readlink(filepath.Join(e.out, "events.jsonl")); err != nil || dest != target {
		t.Errorf("events.jsonl link = %q %v", dest, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Errorf("the stub wrote through its own link: %q", b)
	}
}

func TestStubReplacesTheWorkspace(t *testing.T) {
	e := newStubEnv(t)
	other := t.TempDir()
	if code, _, err := e.run(t, `{"replaceWorkspace":"link","workspaceTarget":"`+other+`","result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if fi, err := os.Lstat(e.ws); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("workspace must be a link: %v %v", fi, err)
	}
	e = newStubEnv(t)
	if code, _, err := e.run(t, `{"replaceWorkspace":"remove","result":`+goodResult+`}`); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, err := os.Lstat(e.ws); !os.IsNotExist(err) {
		t.Errorf("workspace must be gone: %v", err)
	}
}

func TestStubReportsEnvironmentAndInputWrites(t *testing.T) {
	e := newStubEnv(t)
	in := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "task.json"), []byte("{}"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(in, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(in, 0o700) })
	t.Setenv("FACTORY_IN", in)
	t.Setenv("STUB_PROBE", "seen")
	code, stdout, err := e.run(t, `{"dumpEnv":true,"writeIn":true,"result":`+goodResult+`}`)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	var texts []string
	if _, err := NewStub("oxf").Read(strings.NewReader(stdout), func(ev api.Event) { texts = append(texts, ev.Text) }); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "STUB_PROBE=seen") {
		t.Errorf("the environment was not reported: %q", joined)
	}
	if !strings.Contains(joined, "write-in: task.json: refused") || !strings.Contains(joined, "write-in: new file: refused") {
		t.Errorf("input writes: %q", joined)
	}
}

func TestStubSpawnsAChild(t *testing.T) {
	e := newStubEnv(t)
	code, stdout, err := e.run(t, `{"child":{"sleepSeconds":60},"result":`+goodResult+`}`)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	var pid int
	if _, err := NewStub("oxf").Read(strings.NewReader(stdout), func(ev api.Event) {
		if v, ok := strings.CutPrefix(ev.Text, "child-pid="); ok {
			pid, _ = strconv.Atoi(v)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if pid <= 1 {
		t.Fatalf("no child pid in %q", stdout)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("the child must be running: %v", err)
	}
}

func TestStubScriptRejectsBadAttackFields(t *testing.T) {
	e := newStubEnv(t)
	for _, s := range []string{
		`{"floodStdout":-1}`, `{"longLine":-5}`, `{"resultAs":"link"}`,
		`{"replaceWorkspace":"link"}`, `{"replaceWorkspace":"bogus"}`, `{"outAsLink":"relative"}`,
		`{"child":{"sleepSeconds":0}}`, `{"eventsLink":"relative"}`,
	} {
		if code, _, err := e.run(t, s); err == nil || code == 0 {
			t.Errorf("%s: code=%d err=%v", s, code, err)
		}
	}
	_ = json.Valid
}

// Each hostile behaviour that touches the disk outside the stub's own
// directories needs OXF_STUB_ATTACK_ROOT, and only acts under it.
func TestStubHostileBehavioursNeedTheAttackRoot(t *testing.T) {
	other := t.TempDir()
	scripts := map[string]string{
		"outAsLink":        `{"outAsLink":"` + other + `"}`,
		"replaceWorkspace": `{"replaceWorkspace":"remove"}`,
		"writeIn":          `{"writeIn":true}`,
		"swapRunDir":       `{"swapRunDir":{"mode":"link","target":"` + other + `"}}`,
	}
	for name, script := range scripts {
		t.Run(name+" unset", func(t *testing.T) {
			e := newStubEnv(t)
			t.Setenv("OXF_STUB_ATTACK_ROOT", "")
			t.Setenv("FACTORY_IN", e.out)
			code, _, err := e.run(t, script)
			if err == nil || code == 0 {
				t.Errorf("code=%d err=%v", code, err)
			}
			if _, err := os.Lstat(e.ws); err != nil {
				t.Errorf("the workspace was touched: %v", err)
			}
			if fi, err := os.Lstat(e.out); err != nil || fi.Mode()&os.ModeSymlink != 0 {
				t.Errorf("out was touched: %v", err)
			}
		})
		t.Run(name+" outside the root", func(t *testing.T) {
			e := newStubEnv(t)
			t.Setenv("OXF_STUB_ATTACK_ROOT", t.TempDir())
			t.Setenv("FACTORY_IN", e.out)
			code, _, err := e.run(t, script)
			if err == nil || code == 0 {
				t.Errorf("code=%d err=%v", code, err)
			}
			if _, err := os.Lstat(e.ws); err != nil {
				t.Errorf("the workspace was touched: %v", err)
			}
		})
	}
}

func TestStubSwapsTheRunDirectory(t *testing.T) {
	e := newStubEnv(t)
	run := filepath.Dir(e.ws)
	other := t.TempDir()
	script := `{"swapRunDir":{"mode":"link","target":"` + other + `"},"result":` + goodResult + `}`
	if code, _, err := e.run(t, script); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if dest, err := os.Readlink(run); err != nil || dest != other {
		t.Errorf("run dir must be a link to the other directory: %q %v", dest, err)
	}
	if _, err := os.Stat(run + ".moved"); err != nil {
		t.Errorf("the real directory must be kept beside it: %v", err)
	}
}

func TestStubSwapLoopEndsRestored(t *testing.T) {
	e := newStubEnv(t)
	run := filepath.Dir(e.ws)
	other := t.TempDir()
	script := `{"swapRunDir":{"mode":"loop","target":"` + other + `","loopMs":100},"result":` + goodResult + `}`
	if code, _, err := e.run(t, script); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if fi, err := os.Lstat(run); err != nil || !fi.IsDir() {
		t.Errorf("the run directory must be back: %v %v", fi, err)
	}
}

func TestStubCommandPassesTheAttackRoot(t *testing.T) {
	s := &Stub{Program: "/bin/oxf", AttackRoot: "/tmp/root"}
	cmd, err := s.Command(Spec{Workspace: "/work", OutDir: "/out", StubScript: "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, kv := range cmd.Env {
		if kv == "OXF_STUB_ATTACK_ROOT=/tmp/root" {
			found = true
		}
	}
	if !found {
		t.Errorf("env = %v", cmd.Env)
	}
}
