package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("acceptance/40/manifest.json", "{\"a\":1}\n")
	tr.write("bin/run.sh", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(tr.wt, "bin/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("manifest.json", filepath.Join(tr.wt, "acceptance/40/link")); err != nil {
		t.Fatal(err)
	}
	tr.write("big.txt", strings.Repeat("x", 100))
	first := tr.commit("first")
	tr.write("acceptance/40/manifest.json", "{\"a\":2}\n")
	second := tr.commit("second")

	r, err := Open(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := r.ReadFile(ctx, first, "acceptance/40/manifest.json", 1000)
	if err != nil || string(got) != "{\"a\":1}\n" {
		t.Fatalf("first = %q, %v", got, err)
	}
	got, err = r.ReadFile(ctx, second, "acceptance/40/manifest.json", 1000)
	if err != nil || string(got) != "{\"a\":2}\n" {
		t.Fatalf("second = %q, %v", got, err)
	}
	if got, err = r.ReadFile(ctx, second, "bin/run.sh", 1000); err != nil || string(got) != "#!/bin/sh\n" {
		t.Errorf("executable file = %q, %v", got, err)
	}

	bad := []struct{ name, path, want string }{
		{"directory", "acceptance/40", "not a regular file"},
		{"symlink", "acceptance/40/link", "not a regular file"},
		{"missing", "acceptance/41/manifest.json", "no such file"},
		{"over the cap", "big.txt", "over the limit"},
		{"empty path", "", "path"},
		{"leading slash", "/acceptance/40/manifest.json", "path"},
		{"dot dot", "acceptance/../big.txt", "path"},
		{"dot", "./big.txt", "path"},
		{"trailing slash", "acceptance/40/", "path"},
		{"git part", ".git/config", "path"},
		{"git part any case", "a/.GIT/config", "path"},
		{"glob is literal", "acceptance/*/manifest.json", "no such file"},
		{"pathspec magic", ":(top)big.txt", "no such file"},
		{"newline", "big.txt\nx", "path"},
		{"backslash", "bin\\run.sh", "path"},
	}
	for _, c := range bad {
		limit := int64(1000)
		if c.name == "over the cap" {
			limit = 10
		}
		if _, err := r.ReadFile(ctx, second, c.path, limit); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.want)
		}
	}
	if _, err := r.ReadFile(ctx, "-x", "big.txt", 10); err == nil {
		t.Error("a revision that starts with a dash must be refused")
	}
	if _, err := r.ReadFile(ctx, second, "big.txt", 0); err == nil {
		t.Error("a zero cap must be refused")
	}
	if _, err := r.ReadFile(ctx, "no-such-commit", "big.txt", 1000); err == nil {
		t.Error("a missing commit must be refused")
	}
}

func TestReadFile_GuardFirst(t *testing.T) {
	tr := newTrapRepo(t)
	r, err := Open(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	tr.git("config", "core.fsmonitor", tr.script("fsmonitor"))
	if _, err := r.ReadFile(context.Background(), tr.head, "main.go", 1000); err == nil {
		t.Fatal("a tampered repository must be refused")
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("a program ran: %v", got)
	}
}

func TestReadFile_NilHandle(t *testing.T) {
	var r *Repo
	if _, err := r.ReadFile(context.Background(), "HEAD", "x", 10); err == nil {
		t.Error("a nil handle must be refused")
	}
}
