package repo

import (
	"fmt"
	"testing"
	"time"
)

// An attacker picks the number of files. Import must not start a process for
// each one.
func TestImport_ManySmallFilesAreFast(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	for i := 0; i < 5000; i++ {
		ws.write(fmt.Sprintf("d%d/f%d.txt", i%50, i), fmt.Sprintf("file %d\n", i))
	}
	start := time.Now()
	c, changed := im.mustImport(ws, im.head, ImportOptions{})
	took := time.Since(start)
	t.Logf("5000 files imported in %v", took)
	if !changed || c == "" {
		t.Fatal("no commit")
	}
	if took > 20*time.Second {
		t.Errorf("import took %v, want under 20s", took)
	}
	if got := im.git("show", c+":d7/f4057.txt"); got != "file 4057" {
		t.Errorf("content = %q", got)
	}
	// Git itself must accept every object we wrote.
	im.git("fsck", "--strict", "--no-dangling")
}
