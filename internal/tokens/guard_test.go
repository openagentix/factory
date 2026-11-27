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
// search PATH and never start a program, with two named exceptions (see
// allowed below). The scan is by import path, so a renamed import is caught,
// and it flags any use of a guarded name, not only a call. A test file may do
// all of that; the rule is for product code.
//
// One exception is honest to state: internal/settings calls os.UserConfigDir,
// which reads the environment (HOME, and XDG_CONFIG_HOME on Linux). Record
// 0003 allows that single read. The guard allows exactly that use, in the
// function Open of machine_unix.go, and nothing else.

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
	cases := map[string]string{
		"getenv":            `package x; import "os"; func f() { _ = os.Getenv("A") }`,
		"environ":           `package x; import "os"; func f() { _ = os.Environ() }`,
		"lookupenv":         `package x; import "os"; func f() { _, _ = os.LookupEnv("A") }`,
		"expandenv":         `package x; import "os"; func f() { _ = os.ExpandEnv("$A") }`,
		"renamed import":    `package x; import o "os"; func f() { _ = o.Getenv("A") }`,
		"function value":    `package x; import "os"; var g = os.Getenv`,
		"function argument": `package x; import "os"; func h(func(string) string) {}; func f() { h(os.Getenv) }`,
		"syscall getenv":    `package x; import "syscall"; func f() { _, _ = syscall.Getenv("A") }`,
		"syscall environ":   `package x; import "syscall"; func f() { _ = syscall.Environ() }`,
		"dot import":        `package x; import . "os"; func f() { _ = Getenv("A") }`,
		"home dir":          `package x; import "os"; func f() { _, _ = os.UserHomeDir() }`,
		"config dir":        `package x; import "os"; func f() { _, _ = os.UserConfigDir() }`,
		"look path":         `package x; import "os/exec"; func f() { _, _ = exec.LookPath("git") }`,
		"command literal":   `package x; import "os/exec"; func f() { _ = exec.Command("/bin/ls") }`,
		"command variable":  `package x; import "os/exec"; func f(tool string) { _ = exec.Command(tool) }`,
		"context command":   `package x; import "os/exec"; func f(tool string) { _ = exec.CommandContext(nil, tool) }`,
		"renamed exec":      `package x; import e "os/exec"; func f(p string) { _ = e.Command(p) }`,
		"cmd literal":       `package x; import "os/exec"; func f() { _ = &exec.Cmd{Path: "/bin/ls"} }`,
		"cmd value literal": `package x; import "os/exec"; var c = exec.Cmd{Path: "/bin/ls"}`,
		"cmd variable":      `package x; import "os/exec"; func f() { var c exec.Cmd; c.Path = "/bin/ls"; _ = c.Run() }`,
		"cmd new":           `package x; import "os/exec"; func f() { c := new(exec.Cmd); _ = c }`,
		"cmd parameter":     `package x; import "os/exec"; func f(c *exec.Cmd) { _ = c }`,
		"raw syscall":       `package x; import "syscall"; func f() { syscall.RawSyscall(syscall.SYS_EXECVE, 0, 0, 0) }`,
		"syscall":           `package x; import "syscall"; func f() { syscall.Syscall(59, 0, 0, 0) }`,
		"syscall6":          `package x; import "syscall"; func f() { syscall.Syscall6(59, 0, 0, 0, 0, 0, 0) }`,
		"raw syscall6":      `package x; import "syscall"; func f() { syscall.RawSyscall6(59, 0, 0, 0, 0, 0, 0) }`,
		"closure in a var":  `package x; import "os/exec"; var f = func(tool string) { _ = exec.CommandContext(nil, tool) }`,
	}
	for name, src := range cases {
		if got := scanSource(t, "bad.go", src); len(got) == 0 {
			t.Errorf("%s: not flagged", name)
		}
	}
	// The same calls are flagged in a file and function that are not on the list.
	run := `package x; import "os/exec"; func other(tool string) { _ = exec.CommandContext(nil, tool) }`
	if got := scanSource(t, "run.go", run); len(got) == 0 {
		t.Error("exec in the wrong function of run.go is not flagged")
	}
	method := `package x; import "os/exec"; type s struct{}; func (s) execTool(tool string) { _ = exec.CommandContext(nil, tool) }`
	if got := scanSource(t, "run.go", method); len(got) == 0 {
		t.Error("a method named like the allowed function is not flagged")
	}
	other := `package x; import "os/exec"; func execTool(tool string) { _ = exec.CommandContext(nil, tool) }`
	if got := scanSource(t, "other.go", other); len(got) == 0 {
		t.Error("the allowed function name in another file is not flagged")
	}
	cfg := `package x; import "os"; func Elsewhere() { _, _ = os.UserConfigDir() }`
	if got := scanSource(t, "machine_unix.go", cfg); len(got) == 0 {
		t.Error("os.UserConfigDir in the wrong function is not flagged")
	}
}

func TestScannerLetsTheKnownPlacesPass(t *testing.T) {
	good := map[string][2]string{
		"the one exec call":    {"run.go", `package x; import "os/exec"; func execTool(tool string) { _ = exec.CommandContext(nil, tool) }`},
		"exec type in prepare": {"exec_unix.go", `package x; import "os/exec"; func prepare(c *exec.Cmd) { _ = c }`},
		"the one config dir":   {"machine_unix.go", `package x; import "os"; func Open() { _ = os.UserConfigDir }`},
		"a local named os":     {"a.go", `package x; func f() { os := map[string]string{}; _ = os["Getenv"]; type T struct{ Getenv int }; var o struct{ Getenv int }; _ = o.Getenv }`},
		"harmless calls":       {"a.go", `package x; import "os"; func f() { _, _ = os.Open("/etc/passwd") }`},
	}
	for name, c := range good {
		if got := scanSource(t, c[0], c[1]); len(got) != 0 {
			t.Errorf("%s: false alarm: %v", name, got)
		}
	}
}

// guarded lists, by import path, the names that read the environment, search
// PATH or start a program. Any use is a problem, a call or not.
var guarded = map[string]map[string]bool{
	"os": {
		"Getenv": true, "Environ": true, "LookupEnv": true, "ExpandEnv": true,
		"UserHomeDir": true, "UserCacheDir": true, "UserConfigDir": true,
		"StartProcess": true,
	},
	"syscall": {
		"Getenv": true, "Environ": true, "StartProcess": true, "Exec": true, "ForkExec": true,
		"Syscall": true, "Syscall6": true, "RawSyscall": true, "RawSyscall6": true,
	},
	// Any use of the type Cmd is guarded, not only a literal.
	"os/exec": {"LookPath": true, "Command": true, "CommandContext": true, "Cmd": true},
}

// allowed are the only places that may use a guarded name. Key: the file name,
// a colon, the name of a top-level function that has no receiver. A method, a
// closure outside such a function and a package variable never match.
//
//   - run.go:execTool starts the credential tool. Its path is checked by checkTool
//     in the same function, and it is the only place that starts a program.
//   - machine_unix.go:Open calls os.UserConfigDir. That reads the environment
//     (XDG_CONFIG_HOME on Linux, HOME on both). Record 0003 allows this one
//     read, to find the config directory. It is the single exception.
var allowed = map[string]map[string]bool{
	"run.go:execTool":       {"os/exec.CommandContext": true},
	"exec_unix.go:prepare":  {"os/exec.Cmd": true}, // sets process attributes only
	"exec_other.go:prepare": {"os/exec.Cmd": true},
	"machine_unix.go:Open":  {"os.UserConfigDir": true},
}

func scanSource(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	// Local name of each import, by the import path, never by the name used.
	local := map[string]string{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if _, g := guarded[path]; !g {
			continue
		}
		n := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			n = imp.Name.Name
		}
		switch n {
		case ".":
			if len(guarded[path]) > 0 {
				out = append(out, fset.Position(imp.Pos()).String()+": dot import of "+path)
			}
		case "_":
		default:
			local[n] = path
		}
	}
	check := func(fn string, root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Obj != nil { // Obj is set for a local name that hides the import
				return true
			}
			path, ok := local[id.Name]
			if !ok || !guarded[path][sel.Sel.Name] {
				return true
			}
			use := path + "." + sel.Sel.Name
			if allowed[filepath.Base(name)+":"+fn][use] {
				return true
			}
			out = append(out, fset.Position(sel.Pos()).String()+": "+use)
			return true
		})
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			if fd.Recv != nil {
				check("(method) "+fd.Name.Name, fd)
			} else {
				check(fd.Name.Name, fd)
			}
		} else {
			check("", d)
		}
	}
	sort.Strings(out)
	return out
}
