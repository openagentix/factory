package gate

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The package is pure decisions over values. It imports nothing that can
// start a program, open a file, use the network or read the clock, and its
// source calls none of them.
func TestPackageStartsNoProgramAndReadsNoFile(t *testing.T) {
	allowed := map[string]bool{
		"errors": true, "fmt": true, "sort": true, "strings": true, "unicode": true, "unicode/utf8": true,
		"github.com/openagentix/factory/internal/api": true,
		// verify is imported only for its path matching, verify.Match. The
		// gate calls no other function of it.
		"github.com/openagentix/factory/internal/verify": true,
	}
	forbiddenCall := regexp.MustCompile(`\b(exec\.Command|exec\.LookPath|os\.Open|os\.ReadFile|os\.Create|os\.WriteFile|os\.Getenv|os\.Stat|net\.Dial|net\.Listen|http\.|time\.Now|ioutil\.)`)
	verifyCall := regexp.MustCompile(`\bverify\.([A-Za-z]+)`)
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	n := 0
	for _, name := range files {
		if len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		n++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[path] {
				t.Errorf("%s imports %q", name, path)
			}
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if m := forbiddenCall.Find(src); m != nil {
			t.Errorf("%s uses %q", name, m)
		}
		for _, m := range verifyCall.FindAllSubmatch(src, -1) {
			if string(m[1]) != "Match" {
				t.Errorf("%s calls verify.%s, only verify.Match is allowed", name, m[1])
			}
		}
	}
	if n < 5 {
		t.Fatalf("only %d source files were scanned", n)
	}
}
