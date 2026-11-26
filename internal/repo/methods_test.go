package repo

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// allMethods calls every exported method of Repo that uses git, on a handle
// made without Open, and returns each error by name. Both tests below use it,
// so a new method that is left out of the list fails the second test.
func allMethods(t *testing.T, tr *trapRepo) map[string]error {
	t.Helper()
	ctx := context.Background()
	r := &Repo{dir: tr.dir}
	out := map[string]error{}
	_, out["Resolve"] = r.Resolve(ctx, tr.head)
	_, out["NameStatus"] = r.NameStatus(ctx, tr.base, tr.head)
	_, out["NumStat"] = r.NumStat(ctx, tr.base, tr.head)
	_, out["Export"] = r.Export(ctx, tr.head, filepath.Join(t.TempDir(), "x"), Limits{})
	out["SetRef"] = r.SetRef(ctx, "refs/heads/factory/run1", tr.head)
	out["SetDefaultMirror"] = r.SetDefaultMirror(ctx, tr.head)
	_, out["Open"] = Open(tr.dir)
	return out
}

// exemptFromGuard lists the exported names of this package that start no git
// command on a repository handle, and why.
var exemptFromGuard = map[string]string{
	"Init":         "creates the repository, then runs the guard on the result",
	"Dir":          "returns a path",
	"WithDefaults": "no repository",
	"Visible":      "no repository",
	"Suspicious":   "no repository",
}

func TestEveryExportedGitFunctionChecksTheGuardFirst(t *testing.T) {
	// An unknown key must stop every method even when the arguments are bad,
	// so the guard comes before any lookup. The marker script must not run.
	tr := newTrapRepo(t)
	tr.git("config", "core.fsmonitor", tr.script("fsmonitor"))
	for name, err := range allMethods(t, tr) {
		if err == nil || !strings.Contains(err.Error(), "core.fsmonitor") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("a program ran: %v", got)
	}
}

func TestEveryExportedNameIsCoveredByTheGuardTest(t *testing.T) {
	tr := newTrapRepo(t)
	covered := allMethods(t, tr)
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	var missing []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !fd.Name.IsExported() {
				continue
			}
			if fd.Recv != nil && !isRepoReceiver(fd.Recv) {
				continue // a method of another type, such as Limits
			}
			name := fd.Name.Name
			if _, ok := covered[name]; ok {
				continue
			}
			if _, ok := exemptFromGuard[name]; ok {
				continue
			}
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("exported names that the guard test does not cover: %v", missing)
	}
}

func isRepoReceiver(recv *ast.FieldList) bool {
	if len(recv.List) != 1 {
		return false
	}
	typ := recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	id, ok := typ.(*ast.Ident)
	return ok && id.Name == "Repo"
}
