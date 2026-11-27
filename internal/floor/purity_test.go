package floor

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The package is pure computation. It imports no package that can reach a
// file, a program, the network, a goroutine or the clock, and its source calls
// none of them. Test files may use more (os, time); the rule is for the
// package's own code.
func TestPackageIsPure(t *testing.T) {
	allowed := map[string]bool{
		"crypto/sha256": true, "encoding/hex": true, "encoding/json": true, "errors": true, "fmt": true,
		"regexp": true, "sort": true, "strconv": true, "strings": true, "time": true, "unicode": true, "unicode/utf8": true,
		"github.com/openagentix/factory/internal/api": true,
	}
	forbidden := regexp.MustCompile(`\b(exec\.|os\.|net\.|http\.|ioutil\.|syscall\.|rand\.|sync\.|signal\.)|time\.(Now|Since|Until|Sleep|After|Tick|NewTimer|NewTicker|AfterFunc|Local)\b|\bgo\s+func|\bgo\s+[a-zA-Z_.]+\(`)
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	n := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		n++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[p] {
				t.Errorf("%s imports %q", name, p)
			}
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if m := forbidden.Find(src); m != nil {
			t.Errorf("%s uses %q", name, m)
		}
		// No package-level variable that code could change: only regexp
		// values made once and read-only tables are allowed.
	}
	if n < 4 {
		t.Fatalf("only %d source files were scanned", n)
	}
}

// Package-level variables are limited to compiled patterns, which no code
// changes. Anything else is a global that could change.
func TestNoMutableGlobals(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`(?m)^var\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(name)
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if !strings.HasPrefix(m[2], "regexp.MustCompile(") {
				t.Errorf("%s: package variable %s is not a compiled pattern", name, m[1])
			}
		}
		if regexp.MustCompile(`(?m)^var\s*\(`).Match(src) {
			t.Errorf("%s: grouped package variables", name)
		}
	}
}
