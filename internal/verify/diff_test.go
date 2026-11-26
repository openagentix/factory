package verify

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestChangedFiles(t *testing.T) {
	r := newTestRepo(t)
	r.write("keep.txt", "one\ntwo\nthree\n")
	r.write("edit.go", "a\nb\nc\nd\n")
	r.write("gone.txt", lines(5, "g"))
	r.write("old/name.go", lines(30, "same"))
	r.write("old/moved-edit.go", lines(30, "other"))
	r.write("bin.dat", "x\x00y")
	r.write("new/.keep", "")
	base := r.commit("base")

	r.write("edit.go", "a\nB\nc\nd\ne\n") // 2 added, 1 removed
	r.git("rm", "-q", "gone.txt")         // 5 removed
	r.write("new file.txt", "n1\nn2\n")   // 2 added, name with a space
	r.git("mv", "old/name.go", "new/name.go")
	r.git("mv", "old/moved-edit.go", "new/moved-edit.go")
	r.write("new/moved-edit.go", lines(30, "other")+"extra\n") // rename plus 1 added
	r.write("bin.dat", "x\x00z\x00")                           // binary change
	head := r.commit("head")

	cs, err := ChangedFiles(context.Background(), r.dir, base, head)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	want := []Change{
		{Path: "bin.dat", Status: StatusModified, Binary: true},
		{Path: "edit.go", Status: StatusModified, Added: 2, Removed: 1},
		{Path: "gone.txt", Status: StatusDeleted, Removed: 5},
		{Path: "new file.txt", Status: StatusAdded, Added: 2},
		{Path: "new/moved-edit.go", OldPath: "old/moved-edit.go", Status: StatusRenamed, Added: 1},
		{Path: "new/name.go", OldPath: "old/name.go", Status: StatusRenamed},
	}
	if !reflect.DeepEqual(cs.Changes, want) {
		t.Errorf("changes:\n got %+v\nwant %+v", cs.Changes, want)
	}
	if cs.Added != 5 || cs.Removed != 6 {
		t.Errorf("added/removed = %d/%d, want 5/6 (binary files count as 0)", cs.Added, cs.Removed)
	}
	wantPaths := []string{"bin.dat", "edit.go", "gone.txt", "new file.txt", "new/moved-edit.go", "new/name.go", "old/moved-edit.go", "old/name.go"}
	if got := cs.Paths(); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("Paths() = %v, want %v (a rename lists both paths)", got, wantPaths)
	}
}

func TestChangedFiles_AwkwardNames(t *testing.T) {
	r := newTestRepo(t)
	r.write("seed", "s\n")
	base := r.commit("base")
	names := []string{"café.txt", "tab\there.txt", "quote\".txt", "-dash.txt"}
	for _, n := range names {
		r.write(n, "x\n")
	}
	head := r.commit("head")
	cs, err := ChangedFiles(context.Background(), r.dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range cs.Changes {
		got[c.Path] = true
	}
	for _, n := range names {
		if !got[n] {
			t.Errorf("missing %q in %v", n, cs.Paths())
		}
	}
}

func TestChangedFiles_NoChange(t *testing.T) {
	r := newTestRepo(t)
	r.write("a", "a\n")
	c := r.commit("one")
	cs, err := ChangedFiles(context.Background(), r.dir, c, c)
	if err != nil || len(cs.Changes) != 0 || cs.Added != 0 {
		t.Fatalf("cs=%+v err=%v", cs, err)
	}
}

func TestChangedFiles_BadRefs(t *testing.T) {
	r := newTestRepo(t)
	r.write("a", "a\n")
	c := r.commit("one")
	for _, tt := range []struct{ name, base, head string }{
		{"unknown ref", "no-such-ref", c},
		{"option as ref", "--output=/tmp/x", c},
		{"empty", "", c},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ChangedFiles(context.Background(), r.dir, tt.base, tt.head)
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), "\n\n") {
				t.Errorf("error should be one tidy message: %q", err)
			}
		})
	}
}
