package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package starts no program. Checks go through package verify and git
// goes through package repo.
func TestNoProgramIsStartedHere(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("files: %v, %v", files, err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"exec.Command", "\"os/exec\"", "os.StartProcess", "syscall.Exec", "\"os/signal\""} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s uses %s", f, bad)
			}
		}
	}
}
