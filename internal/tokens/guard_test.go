package tokens

import (
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

// The packages that touch tokens and settings never read the environment, never
// search PATH and never start a program whose path is not absolute. A test
// file may do all of that; the rule is for product code.

var scanned = []string{".", "../settings", "../redact"}

func TestSourceNeverReadsEnvironmentOrPath(t *testing.T) {
	for _, dir := range scanned {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			n++
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, problem := range scanSource(t, name, string(src)) {
				t.Errorf("%s", problem)
			}
		}
		if n == 0 {
			t.Errorf("%s: no source files were scanned", dir)
		}
	}
}

func TestScannerSeesTheBadPatterns(t *testing.T) {
	bad := `package x
import ("os"; "os/exec")
func f(p string) {
	_ = os.Getenv("A")
	_ = os.Environ()
	_, _ = os.LookupEnv("A")
	_ = os.ExpandEnv("$A")
	_, _ = exec.LookPath("git")
	_ = exec.Command("git")
	_ = exec.Command(p)
	_ = exec.CommandContext(nil, "git")
}`
	if got := scanSource(t, "bad.go", bad); len(got) != 8 {
		t.Fatalf("found %d problems, want 8: %v", len(got), got)
	}
	good := `package x
import "os/exec"
func f(tool string) {
	_ = exec.Command("/usr/bin/security")
	_ = exec.CommandContext(nil, tool)
}`
	if got := scanSource(t, "good.go", good); len(got) != 0 {
		t.Fatalf("false alarm: %v", got)
	}
}

func scanSource(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		at := fset.Position(call.Pos()).String()
		switch pkg.Name + "." + sel.Sel.Name {
		case "os.Getenv", "os.Environ", "os.LookupEnv", "os.ExpandEnv", "exec.LookPath":
			out = append(out, at+": "+pkg.Name+"."+sel.Sel.Name)
		case "exec.Command", "exec.CommandContext":
			i := 0
			if sel.Sel.Name == "CommandContext" {
				i = 1
			}
			if len(call.Args) <= i || !absoluteProgram(call.Args[i]) {
				out = append(out, at+": a program that is not absolute")
			}
		}
		return true
	})
	sort.Strings(out)
	return out
}

// absoluteProgram accepts a string literal that starts with "/" and the
// variable "tool", which only checkTool can fill (run.go checks it first).
func absoluteProgram(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		s, err := strconv.Unquote(v.Value)
		return err == nil && strings.HasPrefix(s, "/")
	case *ast.Ident:
		return v.Name == "tool"
	}
	return false
}
