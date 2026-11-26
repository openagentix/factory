package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// swapFixture is a run directory, and another run's directory that holds a
// marker file. The runner opens the first one, and the agent then swaps its
// path.
type swapFixture struct {
	base, ws, other string
}

func newSwapFixture(t *testing.T) *swapFixture {
	t.Helper()
	base := t.TempDir()
	f := &swapFixture{base: base, ws: filepath.Join(base, "run1"), other: filepath.Join(base, "run2")}
	for _, d := range []string{f.ws, f.other} {
		if err := os.MkdirAll(filepath.Join(d, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(f.ws, "src", "mine.txt"), []byte("mine\n"), 0o644)
	os.WriteFile(filepath.Join(f.other, "MARKER.txt"), []byte("another run's file\n"), 0o644)
	os.WriteFile(filepath.Join(f.other, "src", "MARKER2.txt"), []byte("another run's file\n"), 0o644)
	return f
}

// swapLoop swaps the path of dir for a link to target and back, until stop is
// set. It is the work of a process that runs as the same user.
func swapLoop(dir, target string, stop *atomic.Bool) {
	moved := dir + ".moved"
	for !stop.Load() {
		if os.Rename(dir, moved) != nil {
			continue
		}
		_ = os.Symlink(target, dir)
		_ = os.Remove(dir)
		_ = os.Rename(moved, dir)
	}
}

// leaks reports whether a commit holds a file of the other run.
func (im *importer) leaks(commit string) bool {
	for _, l := range im.tree(commit) {
		if strings.Contains(l, "MARKER") {
			return true
		}
	}
	return false
}

const swapRounds = 300

func (f *swapFixture) openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	return openTestRoot(t, dir)
}

// importMany imports through root n times and counts commits that hold a file
// of the other run, or that lack the run's own file.
func (im *importer) importMany(t *testing.T, root *os.Root, n int) (ok, leaked, lacking int) {
	t.Helper()
	for i := 0; i < n; i++ {
		c, _, err := im.r.ImportRoot(context.Background(), root, "", "m", testAuthor, ImportOptions{Now: fixedNow})
		if err != nil {
			continue
		}
		ok++
		if im.leaks(c) {
			leaked++
		}
		if !im.has(c, "100644 src/mine.txt") {
			lacking++
		}
	}
	return
}

func TestImportRoot_ASwappedPathNeverChangesWhatIsRead(t *testing.T) {
	im := newImporter(t)
	f := newSwapFixture(t)
	root := f.openRoot(t, f.ws) // opened before the swapping starts
	var stop atomic.Bool
	done := make(chan struct{})
	go func() { swapLoop(f.ws, f.other, &stop); close(done) }()
	ok, leaked, lacking := im.importMany(t, root, swapRounds)
	stop.Store(true)
	<-done
	t.Logf("goroutine swap: %d imports, %d leaked", ok, leaked)
	if ok < swapRounds || leaked != 0 || lacking != 0 {
		t.Errorf("ok=%d of %d, leaked=%d, lacking own file=%d", ok, swapRounds, leaked, lacking)
	}
}

func TestImportRoot_ASwappedPathFromAnotherProcessNeverChangesWhatIsRead(t *testing.T) {
	im := newImporter(t)
	f := newSwapFixture(t)
	root := f.openRoot(t, f.ws)
	script := `cd "$1" || exit 1
while :; do mv run1 run1.moved && ln -s run2 run1 && rm run1 ; mv run1.moved run1; done`
	cmd := exec.Command("sh", "-c", script, "sh", f.base)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	time.Sleep(50 * time.Millisecond)
	ok, leaked, lacking := im.importMany(t, root, swapRounds)
	t.Logf("process swap: %d imports, %d leaked", ok, leaked)
	if ok < swapRounds || leaked != 0 || lacking != 0 {
		t.Errorf("ok=%d of %d, leaked=%d, lacking own file=%d", ok, swapRounds, leaked, lacking)
	}
}

func TestImportRoot_ThePathReplacedByACopyOrAParentReplaced(t *testing.T) {
	im := newImporter(t)
	// The path becomes a copy with another inode.
	f := newSwapFixture(t)
	root := f.openRoot(t, f.ws)
	if err := os.Rename(f.ws, f.ws+".old"); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(f.ws, "src"), 0o755)
	os.WriteFile(filepath.Join(f.ws, "src", "mine.txt"), []byte("mine\n"), 0o644)
	os.WriteFile(filepath.Join(f.ws, "MARKER-COPY.txt"), []byte("x"), 0o644)
	ok, leaked, lacking := im.importMany(t, root, 5)
	if ok != 5 || leaked != 0 || lacking != 0 {
		t.Errorf("copy: ok=%d leaked=%d lacking=%d", ok, leaked, lacking)
	}
	// A parent component is replaced by a link to another tree.
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "p", "run1", "src"), 0o755)
	os.WriteFile(filepath.Join(base, "p", "run1", "src", "mine.txt"), []byte("mine\n"), 0o644)
	os.MkdirAll(filepath.Join(base, "q", "run1", "src"), 0o755)
	os.WriteFile(filepath.Join(base, "q", "run1", "MARKER.txt"), []byte("x"), 0o644)
	root2 := openTestRoot(t, filepath.Join(base, "p", "run1"))
	if err := os.Rename(filepath.Join(base, "p"), filepath.Join(base, "p.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "q"), filepath.Join(base, "p")); err != nil {
		t.Fatal(err)
	}
	ok, leaked, lacking = im.importMany(t, root2, 5)
	if ok != 5 || leaked != 0 || lacking != 0 {
		t.Errorf("parent: ok=%d leaked=%d lacking=%d", ok, leaked, lacking)
	}
	// The root is still open and usable: ImportRoot does not close it.
	if _, err := root2.Lstat("."); err != nil {
		t.Errorf("ImportRoot closed the root: %v", err)
	}
}

func TestImportRoot_ASubdirectoryReplacedByALinkIsNotFollowed(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "MARKER.txt"), []byte("outside"), 0o644)
	// Once, between the listing and the reading.
	im := newImporter(t)
	f := newSwapFixture(t)
	root := f.openRoot(t, f.ws)
	importHook = func() {
		os.RemoveAll(filepath.Join(f.ws, "src"))
		os.Symlink(outside, filepath.Join(f.ws, "src"))
	}
	t.Cleanup(func() { importHook = nil })
	c, _, err := im.r.ImportRoot(context.Background(), root, "", "m", testAuthor, ImportOptions{})
	if err == nil && im.leaks(c) {
		t.Error("a swapped subdirectory was followed")
	}
	importHook = nil

	// And in a tight loop while the walk runs, with links to several places.
	for _, target := range []string{outside, "../" + filepath.Base(outside), "."} {
		f2 := newSwapFixture(t)
		root2 := f2.openRoot(t, f2.ws)
		var stop atomic.Bool
		done := make(chan struct{})
		sub := filepath.Join(f2.ws, "src")
		go func() { swapLoop(sub, target, &stop); close(done) }()
		_, leaked, _ := im.importMany(t, root2, 150)
		stop.Store(true)
		<-done
		if leaked != 0 {
			t.Errorf("link to %q: %d commits hold outside files", target, leaked)
		}
	}
}

func TestExportRoot_ASwappedPathNeverReceivesFiles(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	base := t.TempDir()
	dest, other := filepath.Join(base, "dest"), filepath.Join(base, "other")
	os.Mkdir(dest, 0o755)
	os.Mkdir(other, 0o755)
	root := openTestRoot(t, dest)
	var stop atomic.Bool
	done := make(chan struct{})
	go func() { swapLoop(dest, other, &stop); close(done) }()
	for i := 0; i < 100; i++ {
		if _, err := r.ExportRoot(context.Background(), tr.head, root, Limits{}); err != nil {
			t.Fatal(err)
		}
		if _, err := root.Lstat("main.go"); err != nil {
			t.Fatal("the export did not reach the opened directory")
		}
		if err := root.RemoveAll("main.go"); err != nil {
			t.Fatal(err)
		}
		root.RemoveAll(".gitattributes")
	}
	stop.Store(true)
	<-done
	if ents, _ := os.ReadDir(other); len(ents) != 0 {
		t.Errorf("files were written to the other directory: %v", ents)
	}
}
