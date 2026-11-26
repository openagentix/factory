package harness

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Fixtures may be public. This fails if one holds account or machine data.
func TestFixturesHoldNoAccountOrMachineData(t *testing.T) {
	words := []string{"rate_limit", "credential_expires", "inference_geo", "utilization"}
	re := regexp.MustCompile(`(/Users/|/home/|/private/)|[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+\.[A-Za-z]{2,}`)
	n := 0
	err := filepath.WalkDir("testdata", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s := string(b)
		for _, w := range words {
			if strings.Contains(s, w) {
				t.Errorf("%s contains %q", p, w)
			}
		}
		if m := re.FindString(s); m != "" {
			t.Errorf("%s contains a path or email: %q", p, m)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("walk: %v, files=%d", err, n)
	}
}
