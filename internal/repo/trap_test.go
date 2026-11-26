package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// trapRepo is a trusted repository with two commits. A test then makes the
// repository untrusted in one way, with a script that leaves a marker file
// when it runs. The markers live outside the repository.
type trapRepo struct {
	*testRepo
	base, head string
	markers    string // directory that holds the marker files
	scripts    string
}

func newTrapRepo(t *testing.T) *trapRepo {
	t.Helper()
	r := newTestRepo(t)
	r.write("main.go", "package a\n")
	r.write(".gitattributes", "* filter=x\n")
	base := r.commit("base")
	r.write("main.go", "package a\n// changed\n")
	head := r.commit("head")
	return &trapRepo{testRepo: r, base: base, head: head, markers: t.TempDir(), scripts: t.TempDir()}
}

// script writes an executable that leaves the marker called name.
func (tr *trapRepo) script(name string) string {
	tr.t.Helper()
	p := filepath.Join(tr.scripts, name+".sh")
	body := "#!/bin/sh\ntouch '" + filepath.Join(tr.markers, name) + "'\ncat\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	return p
}

func (tr *trapRepo) ran() []string {
	var out []string
	ents, _ := os.ReadDir(tr.markers)
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}
