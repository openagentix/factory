package repo

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// Two imports of the same files into one repository at once, where one fails
// late. The failed one must not take away an object the other one needs.
func TestImport_ConcurrentImportsOfTheSameFilesOneFailingLate(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a file with mode 000")
	}
	rounds := 3
	for round := 0; round < rounds; round++ {
		im := newImporter(t)
		good, bad := newWorkspace(t), newWorkspace(t)
		for _, ws := range []*workspace{good, bad} {
			for i := 0; i < 300; i++ {
				ws.write(fmt.Sprintf("d%d/f%03d.txt", i%10, i), fmt.Sprintf("round %d file %d\n", round, i))
			}
			ws.write("zzz-last.txt", "last\n")
		}
		bad.chmod("zzz-last.txt", 0) // listed fine, cannot be opened: fails after the others
		var wg sync.WaitGroup
		var commit string
		var goodErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			commit, _, goodErr = im.imp(good, im.head, ImportOptions{})
		}()
		go func() {
			defer wg.Done()
			if _, _, err := im.imp(bad, im.head, ImportOptions{}); err == nil {
				t.Error("the import with an unreadable file must fail")
			}
		}()
		wg.Wait()
		if goodErr != nil {
			t.Fatalf("round %d: %v", round, goodErr)
		}
		im.git("fsck", "--strict", "--no-dangling", commit)
	}
}
