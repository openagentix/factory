package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func objPath(dir, oid string) string { return filepath.Join(dir, "objects", oid[:2], oid[2:]) }

func TestObjectWriterNeverWritesAnExistingObjectAgain(t *testing.T) {
	tr := newTrapRepo(t)
	w, err := openObjWriter(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	ctx := context.Background()
	oid, err := w.write(ctx, "blob", 3, strings.NewReader("abc"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(objPath(tr.dir, oid))
	oid2, err := w.write(ctx, "blob", 3, strings.NewReader("abc"))
	if err != nil || oid2 != oid {
		t.Fatalf("%v %s", err, oid2)
	}
	after, _ := os.Stat(objPath(tr.dir, oid))
	if !os.SameFile(before, after) {
		t.Error("the object file was replaced")
	}
	if after.Mode().Perm() != 0o444 {
		t.Errorf("mode = %v", after.Mode())
	}
	if got := objectFiles(t, tr.dir); len(got) == 0 {
		t.Fatal("no objects")
	}
	for p := range objectFiles(t, tr.dir) {
		if filepath.Base(filepath.Dir(p)) == "info" {
			t.Errorf("a temporary file was left: %s", p)
		}
	}
}

func TestPrune(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	ctx := context.Background()
	ws := newWorkspace(t)
	ws.write("old.txt", "old unreferenced\n")
	oldC, _, err := r.Import(ctx, ws.dir, tr.head, "m", testAuthor, ImportOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	ws.write("fresh.txt", "fresh unreferenced\n")
	freshC, _, err := r.Import(ctx, ws.dir, tr.head, "m", testAuthor, ImportOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	// A third commit that is referenced, with an old object file.
	ws.write("kept.txt", "referenced\n")
	keptC, _, err := r.Import(ctx, ws.dir, tr.head, "m", testAuthor, ImportOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetRef(ctx, "refs/heads/factory/keep", keptC, ZeroID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	for _, oid := range []string{oldC, keptC, tr.git("rev-parse", keptC+":kept.txt")} {
		if err := os.Chtimes(objPath(tr.dir, oid), old, old); err != nil {
			t.Fatal(err)
		}
	}
	_ = freshC
	if err := r.Prune(ctx, 59*time.Minute); err == nil {
		t.Error("a short expiry must be refused")
	}
	if _, err := os.Stat(objPath(tr.dir, oldC)); err != nil {
		t.Fatal("a refused prune removed something")
	}
	if err := r.Prune(ctx, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(objPath(tr.dir, oldC)); err == nil {
		t.Error("an old unreferenced commit was kept")
	}
	for name, oid := range map[string]string{"fresh unreferenced": freshC, "referenced": keptC} {
		if _, err := os.Stat(objPath(tr.dir, oid)); err != nil {
			t.Errorf("%s object was removed", name)
		}
	}
	tr.git("fsck", "--strict", "--no-dangling", keptC)
	if err := checkTrusted(ctx, tr.dir); err != nil {
		t.Error(err)
	}
}
