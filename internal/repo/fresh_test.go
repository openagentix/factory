package repo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// An object that already exists is reused by an import. Its file must look
// new afterwards, or a prune before the commit exists would delete it.
func TestObjectWriter_ReusingAnObjectFreshensIt(t *testing.T) {
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
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(objPath(tr.dir, oid), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(ctx, "blob", 3, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(objPath(tr.dir, oid))
	if time.Since(fi.ModTime()) > time.Minute {
		t.Errorf("a reused object is still %v old", time.Since(fi.ModTime()))
	}
	if fi.Mode().Perm() != 0o444 {
		t.Errorf("mode = %v", fi.Mode())
	}
	r, _ := Open(tr.dir)
	if err := r.Prune(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(objPath(tr.dir, oid)); err != nil {
		t.Error("prune removed a blob that was just reused")
	}
}

func TestPrune_AfterAnImportThatReusedAnOldObject(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	ctx := context.Background()
	ws := newWorkspace(t)
	ws.write("reused.txt", "reused content\n")
	c1, _, err := r.Import(ctx, ws.dir, tr.head, "m", testAuthor, ImportOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	blob := tr.git("rev-parse", c1+":reused.txt")
	old := time.Now().Add(-3 * time.Hour)
	for _, oid := range []string{blob, tr.git("rev-parse", c1+"^{tree}"), c1} {
		if err := os.Chtimes(objPath(tr.dir, oid), old, old); err != nil {
			t.Fatal(err)
		}
	}
	// A second import reuses the old blob and tree, and sets no ref.
	c2, _, err := r.Import(ctx, ws.dir, tr.head, "again", testAuthor, ImportOptions{Now: func() time.Time { return fixedTime.Add(time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Prune(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	tr.git("fsck", "--strict", "--no-dangling", c2)
	if _, err := os.Stat(objPath(tr.dir, blob)); err != nil {
		t.Error("the reused blob was pruned")
	}
}
