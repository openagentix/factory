package repo

import (
	"fmt"
	"testing"
	"time"
)

// maxImportProcesses is the most git processes one Import may start, however
// many files it reads. Reading the code, Import runs git a fixed number of
// times: resolve the parent commit, resolve its tree, and commit-tree (and the
// objects are written by a fixed number of calls, not one per file). It
// measures 4; 8 leaves room for a call added later but not for one per file.
const maxImportProcesses = 8

// importCounting imports n small files and returns the commit and the number
// of git processes that the one Import started.
func importCounting(t *testing.T, n int) (*importer, string, int64) {
	t.Helper()
	im := newImporter(t)
	ws := newWorkspace(t)
	for i := 0; i < n; i++ {
		ws.write(fmt.Sprintf("d%d/f%d.txt", i%50, i), fmt.Sprintf("file %d\n", i))
	}
	before := gitStarts.Load()
	start := time.Now()
	c, changed := im.mustImport(ws, im.head, ImportOptions{})
	procs := gitStarts.Load() - before
	t.Logf("%d files: %d git processes, %v (information only)", n, procs, time.Since(start))
	if !changed || c == "" {
		t.Fatal("no commit")
	}
	return im, c, procs
}

// An attacker picks the number of files. Import must not start a process for
// each one. This counts processes, so a busy machine cannot fail it.
func TestImport_ProcessCountDoesNotGrowWithFiles(t *testing.T) {
	_, _, small := importCounting(t, 50)
	im, c, large := importCounting(t, 5000)
	if small != large {
		t.Errorf("processes: %d for 50 files, %d for 5000 files, want the same", small, large)
	}
	if large > maxImportProcesses {
		t.Errorf("5000 files started %d processes, want at most %d", large, maxImportProcesses)
	}
	if got := im.git("show", c+":d7/f4057.txt"); got != "file 4057" {
		t.Errorf("content = %q", got)
	}
	// Git itself must accept every object we wrote.
	im.git("fsck", "--strict", "--no-dangling")
}
