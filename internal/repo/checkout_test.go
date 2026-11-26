package repo

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestMaterialize(t *testing.T) {
	r := newTestRepo(t)
	r.write("target.txt", "target\n")
	r.write("bin/run.sh", "#!/bin/sh\necho hi\n")
	if err := os.Chmod(filepath.Join(r.wt, "bin/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write("dir/deep/file.txt", "deep\n")
	r.write("weird name \u00e9.txt", "w\n")
	for link, target := range map[string]string{
		"ok-link":         "target.txt",
		"dir/up-link":     "../target.txt",
		"abs-link":        "/etc/passwd",
		"esc-link":        "../outside",
		"dir/esc2-link":   "../../outside",
		"dotdot-after":    "dir/../../outside",
		"dir/deep/climb":  "../../target.txt",
		"dir/deep/climb2": "../../../x",
		"backslash":       `a\b`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(r.wt, link)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(r.wt, link)); err != nil {
			t.Fatal(err)
		}
	}
	r.commit("seed") // a commit to point the gitlink at
	sub := r.git("rev-parse", "HEAD")
	r.git("update-index", "--add", "--cacheinfo", "160000,"+sub+",vendor/sub")
	r.git("commit", "-q", "-m", "with submodule")
	head := r.git("rev-parse", "HEAD")

	dest := t.TempDir()
	notes, err := materialize(context.Background(), r.dir, head, dest, Limits{MaxTotalBytes: DefaultMaxTotalBytes, MaxFileBytes: DefaultMaxFileBytes, MaxEntries: DefaultMaxEntries})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, ".git")); err == nil {
		t.Error("the checkout has a .git")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "dir/deep/file.txt")); string(b) != "deep\n" {
		t.Errorf("content = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "weird name \u00e9.txt")); string(b) != "w\n" {
		t.Errorf("content = %q", b)
	}
	fi, err := os.Stat(filepath.Join(dest, "bin/run.sh"))
	if err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("executable bit lost: %v %v", fi, err)
	}
	if fi, _ := os.Stat(filepath.Join(dest, "target.txt")); fi == nil || fi.Mode().Perm()&0o111 != 0 {
		t.Errorf("plain file must not be executable: %v", fi)
	}
	wantLinks := map[string]bool{"ok-link": true, "dir/up-link": true, "dir/deep/climb": true}
	for _, l := range []string{"ok-link", "dir/up-link", "abs-link", "esc-link", "dir/esc2-link", "dotdot-after", "dir/deep/climb", "dir/deep/climb2", "backslash"} {
		_, err := os.Lstat(filepath.Join(dest, l))
		if wantLinks[l] != (err == nil) {
			t.Errorf("link %s: written=%v, want %v", l, err == nil, wantLinks[l])
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "dir/up-link")); string(b) != "target\n" {
		t.Errorf("a safe link must work: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dest, "vendor/sub")); err == nil {
		t.Error("a submodule must be skipped")
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"submodule: \"vendor/sub\"", "abs-link", "esc-link", "dotdot-after", "dir/deep/climb2", "backslash"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes lack %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"ok-link", "dir/up-link"} {
		if strings.Contains(joined, bad) {
			t.Errorf("notes mention a link that was written: %s", bad)
		}
	}
}

func TestUnsafeEntryName(t *testing.T) {
	for n, want := range map[string]bool{
		"a/b": false, ".gitignore": false, "a/.github/x": false,
		".git/config": true, "a/.git/hooks/post-checkout": true, "a/.GIT/x": true, "../x": true, "/abs": true, "a//b": true, "a\\b": true, "": true,
	} {
		if unsafeEntryName(n) != want {
			t.Errorf("unsafeEntryName(%q) != %v", n, want)
		}
	}
}

func TestMaterialize_SameFileTwiceAndLinkParent(t *testing.T) {
	// A write must never go through a link, even when a tree is built by hand.
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "lnk")); err != nil {
		t.Fatal(err)
	}
	note, err := writeEntry(dest, "100644", "lnk/evil.txt", strings.NewReader("x"), 1)
	if err != nil || note == "" {
		t.Fatalf("note=%q err=%v", note, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Error("a file was written through a link")
	}
}

// TestOnlyGitCommandStartsGit parses the package and checks that the git
// program is started in one place (gitCommand in git.go), and that nothing
// else is started.
func TestOnlyGitCommandStartsGit(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	found := map[string][]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" && strings.HasPrefix(sel.Sel.Name, "Command") {
				prog := "?"
				for _, a := range call.Args {
					if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						prog, _ = strconv.Unquote(lit.Value)
						break
					}
				}
				found[f] = append(found[f], prog)
			}
			return true
		})
	}
	var got []string
	for f, progs := range found {
		for _, p := range progs {
			got = append(got, f+":"+p)
		}
	}
	sort.Strings(got)
	want := []string{"git.go:git"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("programs started: %v, want %v", got, want)
	}
}

func TestGitEnvIsAnAllowlist(t *testing.T) {
	r := newTestRepo(t)
	r.write("a", "a\n")
	r.commit("one")
	r.git("config", "alias.dump", "!env")
	t.Setenv("GITHUB_TOKEN", "ghp_secrettoken")
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	t.Setenv("GIT_EXTERNAL_DIFF", "/bin/evil")
	t.Setenv("GIT_ASKPASS", "/bin/evil")
	t.Setenv("EDITOR", "vi")
	env := strings.Join(gitEnv(), "\n")
	for _, bad := range []string{"ghp_", "sk-secret", "GIT_ASKPASS", "GIT_EXTERNAL_DIFF", "EDITOR"} {
		if strings.Contains(env, bad) {
			t.Errorf("git environment holds %s", bad)
		}
	}
	for _, want := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "PATH="} {
		if !strings.Contains(env, want) {
			t.Errorf("git environment lacks %s", want)
		}
	}

	// End to end: a program that git itself starts sees the same environment.
	out, err := runGit(context.Background(), r.dir, "dump")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ghp_") || strings.Contains(string(out), "sk-secret") || strings.Contains(string(out), "EDITOR") {
		t.Errorf("a program started by git saw the process environment:\n%s", out)
	}
}

func TestGitCommandIsHardened(t *testing.T) {
	cmd := gitCommand(context.Background(), ".", "status")
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"core.fsmonitor=false", "core.hooksPath=/dev/null", "core.pager=cat", "diff.external=", "protocol.ext.allow=never", "--no-replace-objects", "--git-dir="} {
		if !strings.Contains(args, want) {
			t.Errorf("git command lacks -c %s: %s", want, args)
		}
	}
}
