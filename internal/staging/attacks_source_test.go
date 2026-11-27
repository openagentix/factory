package staging

import (
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

// Attack 29: only the publish step of this package writes staging. The test
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
	stagingNames := map[string]bool{"StagingRef": true, "StagingBranch": true}
	stagingStrings := []string{"factory-staging"}
	type hit struct{ file, what string }
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
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		files++
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var name string
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			switch name {
			case "PushStaging":
				hits = append(hits, hit{rel, "PushStaging"})
			case "SetRef":
				for _, a := range call.Args {
					ast.Inspect(a, func(m ast.Node) bool {
						switch v := m.(type) {
						case *ast.SelectorExpr:
							if stagingNames[v.Sel.Name] {
								hits = append(hits, hit{rel, "SetRef(" + v.Sel.Name + ")"})
							}
						case *ast.Ident:
							if stagingNames[v.Name] {
								hits = append(hits, hit{rel, "SetRef(" + v.Name + ")"})
							}
						case *ast.BasicLit:
							for _, s := range stagingStrings {
								if strings.Contains(v.Value, s) {
									hits = append(hits, hit{rel, "SetRef(" + v.Value + ")"})
								}
							}
						}
						return true
					})
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 30 {
		t.Fatalf("only %d source files were parsed", files)
	}
	seen := map[string]bool{}
	for _, h := range hits {
		if h.file != "internal/staging/publish.go" {
			t.Errorf("%s calls %s", h.file, h.what)
		}
		seen[h.what] = true
	}
	if !seen["PushStaging"] || !seen["SetRef(StagingRef)"] {
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
	st := f.add(f.entry(1, f.commit(f.def, put("acceptance/1/manifest.json", "{}")), f.def))
	if len(st.Applied) != 0 || removedIDs(st.Removed)["run-1"] != RemovedProtectedPath {
		t.Fatalf("%+v", st)
	}
}
