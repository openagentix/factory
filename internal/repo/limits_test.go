package repo

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMaterializeLimits(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", strings.Repeat("a", 100))
	r.write("b.txt", strings.Repeat("b", 100))
	r.write("c.txt", "c")
	head := r.commit("files")
	tests := []struct {
		name string
		lim  Limits
		want string
	}{
		{"one file too big", Limits{MaxTotalBytes: 1000, MaxFileBytes: 99, MaxEntries: 10}, "a.txt"},
		{"total too big", Limits{MaxTotalBytes: 150, MaxFileBytes: 1000, MaxEntries: 10}, "total"},
		{"too many entries", Limits{MaxTotalBytes: 1000, MaxFileBytes: 1000, MaxEntries: 2}, "entries"},
		{"fits", Limits{MaxTotalBytes: 201, MaxFileBytes: 100, MaxEntries: 3}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			_, err := materialize(context.Background(), r.dir, head, dest, tt.lim)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if ents, _ := os.ReadDir(dest); len(ents) != 0 {
				t.Errorf("nothing may be written when a limit is broken, found %d entries", len(ents))
			}
		})
	}
}
