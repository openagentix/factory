package staging

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// Attack 29, strengthened by fix 10: only the publish step of this package writes staging. The test
// parses every non-test source file of the module. Nothing outside
// internal/staging/publish.go calls PushStaging or hands the staging ref to
// SetRef, and publish.go does both, so the test cannot pass by finding
// nothing.
func TestAttack29_OnlyPublishWritesStaging(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("not the module root: %v", err)
	}
	type hit = stagingHit
	var hits []hit
	files := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", ".planning", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files++
		rel, _ := filepath.Rel(root, p)
		found, err := scanStagingUse(filepath.ToSlash(rel), src)
		hits = append(hits, found...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 30 {
		t.Fatalf("only %d source files were parsed", files)
	}
	seen := map[string]bool{}
	for _, h := range hits {
		// Package repo defines the staging ref and checks it in SetRef.
		if h.file != "internal/staging/publish.go" && !(h.what == "StagingRef" && strings.HasPrefix(h.file, "internal/repo/")) {
			t.Errorf("%s uses %s", h.file, h.what)
		}
		seen[h.what] = true
	}
	if !seen["PushStaging"] || !seen["StagingRef"] {
		t.Errorf("the publish step was not found, so this test proves nothing: %v", hits)
	}
}

// Decision 1: the goal's checks on staging must be the reviewed ones. An
// acceptance change that applies is kept. When the default branch has put
// another file into the goal's directory, the replay differs from its
// source and the change is taken out.
func TestAcceptanceChangeMustEqualItsSource(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T, extra bool) (*fx, Entry, State) {
		f := newFx(t, withAcceptance)
		src := f.commit(f.def, manifestFile(1, crit(1, "test -e a.txt")), put("acceptance/1/check.sh", "true\n"))
		f.goalSource[1] = src
		base := f.def
		if extra {
			f.setDefault(f.commit(f.def, put("acceptance/1/extra.sh", "evil\n")))
		}
		e := f.entry(1, src, base)
		e.Task = api.TaskAcceptance
		st, err := f.s.Add(f.ctx, e, f.decision(e))
		if err != nil {
			t.Fatal(err)
		}
		return f, e, st
	}
	_, _, st := mk(t, false)
	if len(st.Applied) != 1 || len(st.Removed) != 0 {
		t.Fatalf("a clean acceptance change: %+v", st)
	}
	_, e, st := mk(t, true)
	if len(st.Applied) != 0 || removedIDs(st.Removed)[e.ID] != RemovedAcceptance || !strings.Contains(st.Removed[0].Detail, "reviewed source") {
		t.Fatalf("%+v", st)
	}
}

// A change of another task may not touch the acceptance path.
func TestImplementChangeMayNotTouchAcceptancePath(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	e := f.entry(1, f.commit(f.def, put("acceptance/1/manifest.json", "{}")), f.def)
	if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("Add: %v", err)
	}
	// Queued without admission, the step takes it out.
	f.queue(e)
	st := f.rebuild()
	if len(st.Applied) != 0 || removedIDs(st.Removed)["run-1"] != RemovedProtectedPath {
		t.Fatalf("%+v", st)
	}
}

type stagingHit struct{ file, what string }

// scanStagingUse finds every use of the identifier PushStaging and of the
// identifier StagingRef in one source file: a call, a method value, a value
// held in a variable, a slice or an alias. The names that declare them (a
// function, an interface method, a constant) are not uses.
func scanStagingUse(rel string, src []byte) ([]stagingHit, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, err
	}
	declared := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			declared[v.Name] = true
		case *ast.ValueSpec:
			for _, id := range v.Names {
				declared[id] = true
			}
		case *ast.InterfaceType:
			for _, m := range v.Methods.List {
				for _, id := range m.Names {
					declared[id] = true
				}
			}
		}
		return true
	})
	var hits []stagingHit
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !declared[id] && (id.Name == "PushStaging" || id.Name == "StagingRef") {
			hits = append(hits, stagingHit{rel, id.Name})
		}
		return true
	})
	return hits, nil
}

// Fix 10: the scan sees a method value, the staging ref held in a variable and
// an alias, and does not see a declaration. A staging ref name built from
// pieces of string ("factory-" + "staging") cannot be caught this way: only
// the guard in package repo and the host stand behind that.
func TestFix10_ScanSeesMethodValuesAndVariables(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"method value":   "package x\nfunc f(h interface{ PushStaging() }) { g := h.PushStaging; g() }\n",
		"ref variable":   "package x\nimport \"github.com/openagentix/factory/internal/repo\"\nvar ref = repo.StagingRef\n",
		"ref in a slice": "package x\nimport \"github.com/openagentix/factory/internal/repo\"\nvar refs = []string{repo.StagingRef}\n",
		"function value": "package x\nfunc f(h interface{ PushStaging() }) { _ = h.PushStaging }\n",
	}
	for name, src := range bad {
		hits, err := scanStagingUse("x/x.go", []byte(src))
		if err != nil || len(hits) == 0 {
			t.Errorf("%s: not seen: %v %v", name, hits, err)
		}
	}
	fine := "package x\ntype H interface{ PushStaging() }\ntype T struct{}\nfunc (T) PushStaging() {}\nconst StagingRef = \"x\"\n"
	if hits, err := scanStagingUse("x/x.go", []byte(fine)); err != nil || len(hits) != 0 {
		t.Errorf("declarations were taken for uses: %v %v", hits, err)
	}
}
