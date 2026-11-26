package runner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// The package starts one process, the harness, and runs no git command. Git
// goes only through internal/repo.
func TestOnlyOneProcessIsStarted(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			c, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" && strings.HasPrefix(sel.Sel.Name, "Command") {
				calls++
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "syscall" && strings.Contains(sel.Sel.Name, "Exec") {
				t.Errorf("%s: %s starts a process", n, sel.Sel.Name)
			}
			return true
		})
	}
	if calls != 1 {
		t.Errorf("found %d exec.Command calls, want exactly 1 (the harness)", calls)
	}
}

func TestStartRefusesAnIncompleteSpec(t *testing.T) {
	f := newFixture(t)
	good := f.spec(`{}`)
	for name, mut := range map[string]func(*Spec){
		"no repo":     func(s *Spec) { s.Repo = nil },
		"no adapter":  func(s *Spec) { s.Adapter = nil },
		"no data dir": func(s *Spec) { s.DataDir = "" },
		"no deadline": func(s *Spec) { s.Deadline = 0 },
		"no commit":   func(s *Spec) { s.Commit = "" },
		"bad trigger": func(s *Spec) { s.TriggerJSON = []byte("{") },
		"bad commit":  func(s *Spec) { s.Commit = "no-such-ref" },
		"bad author":  func(s *Spec) { s.Author.Name = "" },
	} {
		s := good
		mut(&s)
		if _, err := NewLocal().Start(testCtx(), s); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if d := runDirs(f); len(d) != 0 {
		t.Errorf("failed starts left %v", d)
	}
}
