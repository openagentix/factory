package verify

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
	"time"

	"github.com/openagentix/factory/internal/api"
)

// refused checks that both exported git functions refuse the repository with
// an error that mentions want, run nothing, and return usable zero values.
func refused(t *testing.T, tr *trapRepo, want string) {
	t.Helper()
	ctx := context.Background()
	cs, err := ChangedFiles(ctx, tr.dir, tr.base, tr.head)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ChangedFiles error = %v, want one mentioning %q", err, want)
	}
	if cs == nil || len(cs.Paths()) != 0 {
		t.Errorf("ChangedFiles must return a non-nil empty change set with an error, got %v", cs)
	}
	out, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "t", Run: "true"}}, CheckOptions{Deadline: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("RunChecks error = %v, want one mentioning %q", err, want)
	}
	if len(out) != 0 {
		t.Errorf("RunChecks ran checks in an untrusted repository: %v", out)
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("a program from the repository ran: %v", got)
	}
}

// The full list of untrusted layouts and keys is tested in package repo. These
// cases show that both exported functions of this package use the guard.
func TestGuard_RefusesUntrustedRepositories(t *testing.T) {
	tests := []struct {
		name  string
		setup func(tr *trapRepo)
		want  string
	}{
		{"unknown config key", func(tr *trapRepo) { tr.git("config", "foo.bar", "x") }, "foo.bar"},
		{"fsmonitor", func(tr *trapRepo) { tr.git("config", "core.fsmonitor", tr.script("fsmonitor")) }, "core.fsmonitor"},
		{"smudge filter", func(tr *trapRepo) { tr.git("config", "filter.x.smudge", tr.script("smudge")) }, "filter.x.smudge"},
		{"replace ref", func(tr *trapRepo) { tr.git("replace", tr.head, tr.base) }, "refs/replace"},
		{"alternates", func(tr *trapRepo) {
			put(tr.t, filepath.Join(tr.dir, "objects/info/alternates"), "/some/other/objects\n", 0o644)
		}, "alternates"},
		{"symlinked objects", func(tr *trapRepo) { moveAndLink(tr, "objects") }, "objects"},
		{"executable hook", func(tr *trapRepo) {
			put(tr.t, filepath.Join(tr.dir, "hooks/post-checkout"), "#!/bin/sh\ntouch '"+filepath.Join(tr.markers, "hook")+"'\n", 0o755)
		}, "hooks/post-checkout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTrapRepo(t)
			tt.setup(tr)
			refused(t, tr, tt.want)
		})
	}
}

func put(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestEveryExportedGitFunctionChecksTheGuardFirst(t *testing.T) {
	// A repository with an unknown key must stop both functions even when the
	// revision is bad, so the guard comes before any revision lookup.
	tr := newTrapRepo(t)
	tr.git("config", "foo.bar", "x")
	_, err := ChangedFiles(context.Background(), tr.dir, "no-such-ref", "no-such-ref")
	if err == nil || !strings.Contains(err.Error(), "foo.bar") {
		t.Errorf("ChangedFiles: %v", err)
	}
	_, err = RunChecks(context.Background(), tr.dir, "no-such-ref", nil, CheckOptions{})
	if err == nil || !strings.Contains(err.Error(), "foo.bar") {
		t.Errorf("RunChecks: %v", err)
	}
}

func TestZeroValuesOnError(t *testing.T) {
	var nilSet *ChangeSet
	if len(nilSet.Paths()) != 0 {
		t.Error("nil change set must have no paths")
	}
	f, task := testFactoryFile(), taskNamed("implement")
	if rep, err := CheckProtected(f, task, nil); err != nil || !rep.OK {
		t.Errorf("nil change set: %+v %v", rep, err)
	}
	if tf, err := ClassifyTestFiles(nil, nil); err != nil || tf.Added == nil {
		t.Errorf("nil change set: %+v %v", tf, err)
	}
	if got := UncoveredFiles(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("nil change set: %v", got)
	}
	if _, err := ComputeRisk(RiskInput{RiskPaths: testRiskPaths}); err != nil {
		t.Errorf("nil change set: %v", err)
	}

	// Errors come with usable values, so a caller that forgets to check does
	// not crash.
	tr := newTrapRepo(t)
	tr.git("config", "foo.bar", "x")
	cs, err := ChangedFiles(context.Background(), tr.dir, tr.base, tr.head)
	if err == nil || cs == nil || cs.Paths() == nil {
		t.Fatalf("cs=%v err=%v", cs, err)
	}
	b, err := BuildEvidence(EvidenceInput{})
	if err == nil || b == nil || b.Evidence == nil {
		t.Errorf("BuildEvidence must return a non-nil value with its error: %v %v", b, err)
	}
	in := baseInput(modified("a.go"))
	in.Run = ""
	b, err = BuildEvidence(in)
	if err == nil || b == nil || b.Evidence == nil {
		t.Errorf("BuildEvidence: %v %v", b, err)
	}
}

func TestRunChecks_LimitsComeFromOptionsWithDefaults(t *testing.T) {
	r, head := repoWithHead(t)
	_, err := RunChecks(context.Background(), r.dir, head, []api.Check{{Name: "t", Run: "true"}}, CheckOptions{MaxFileBytes: 2})
	if err == nil || !strings.Contains(err.Error(), "committed.txt") {
		t.Errorf("err = %v", err)
	}
	l := CheckOptions{}.limits()
	if l.MaxTotalBytes != 2<<30 || l.MaxFileBytes != 200<<20 || l.MaxEntries != 50000 {
		t.Errorf("defaults = %+v", l)
	}
}

// moveAndLink replaces the file or directory rel of the repository with a
// symbolic link to where it was moved.
func moveAndLink(tr *trapRepo, rel string) {
	tr.t.Helper()
	from := filepath.Join(tr.dir, filepath.FromSlash(rel))
	to := filepath.Join(tr.t.TempDir(), "moved")
	if err := os.Rename(from, to); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.Symlink(to, from); err != nil {
		tr.t.Fatal(err)
	}
}

// TestNoGitCallSiteInVerify parses the package and checks that it starts one
// program only, sh, for the checks, and never git. Every git command is
// started by the helper in package repo, which has its own test for that.
func TestNoGitCallSiteInVerify(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	var got []string
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
				got = append(got, f+":"+prog)
			}
			return true
		})
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "checks.go:sh" {
		t.Errorf("programs started: %v, want only checks.go:sh", got)
	}
}
