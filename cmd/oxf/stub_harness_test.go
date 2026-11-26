package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStubHarnessSubcommand(t *testing.T) {
	t.Setenv("OXF_ALLOW_STUB", "1")
	d := t.TempDir()
	ws, out := filepath.Join(d, "ws"), filepath.Join(d, "out")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(d, "script.json")
	body := `{"files":[{"path":"a.txt","content":"hi"}],"exitCode":4,"events":[{"kind":"status","text":"go"}]}`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"stub-harness", "--script", script, "--workspace", ws, "--out", out}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(b) != "hi" {
		t.Errorf("a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(out, "result.json")); err != nil {
		t.Errorf("result.json: %v", err)
	}
	if !strings.Contains(stdout.String(), `"type":"final"`) {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestStubHarnessNeedsTheEnvironmentVariable(t *testing.T) {
	d := t.TempDir()
	script := filepath.Join(d, "s.json")
	if err := os.WriteFile(script, []byte(`{"files":[{"path":"a.txt","content":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"", "0", "yes"} {
		t.Setenv("OXF_ALLOW_STUB", v)
		var stdout, stderr bytes.Buffer
		code := run([]string{"stub-harness", "--script=" + script, "--workspace=" + d, "--out=" + d}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "OXF_ALLOW_STUB=1") {
			t.Errorf("value %q: code=%d stderr=%q", v, code, stderr.String())
		}
		if _, err := os.Stat(filepath.Join(d, "a.txt")); err == nil {
			t.Fatal("the stub acted without permission")
		}
	}
}

func TestStubHarnessBadArguments(t *testing.T) {
	t.Setenv("OXF_ALLOW_STUB", "1")
	tests := [][]string{
		{"stub-harness"},
		{"stub-harness", "--script", "/nonexistent/x.json", "--workspace", "/tmp", "--out", "/tmp"},
		{"stub-harness", "--nope"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: code = %d, want 2", args, code)
		}
	}
}

func TestStubHarnessIsHidden(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(nil, &stdout, &stderr)
	if strings.Contains(stderr.String(), "stub-harness") || strings.Contains(stdout.String(), "stub-harness") {
		t.Error("the stub-harness command must not show in usage")
	}
}
