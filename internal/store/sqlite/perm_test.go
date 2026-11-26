//go:build !windows

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFileModes(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "state", "factory")
	path := filepath.Join(dir, "factory.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateRun(ctx, newRun("r1", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLevel(ctx, "implement", 1, 2); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Dir(dir)} {
		if m := modeOf(t, d); m != 0o700 {
			t.Errorf("directory %s has mode %o, want 700", d, m)
		}
	}
	found := 0
	for _, suffix := range []string{"", "-wal", "-shm"} {
		fi, err := os.Stat(path + suffix)
		if err != nil {
			continue
		}
		found++
		if m := fi.Mode().Perm(); m != 0o600 {
			t.Errorf("%s has mode %o, want 600", filepath.Base(path+suffix), m)
		}
	}
	if found == 0 {
		t.Fatal("no database file found")
	}
}

func TestExistingDirectoryIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), filepath.Join(dir, "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if m := modeOf(t, dir); m != 0o755 {
		t.Errorf("an existing directory was changed to %o", m)
	}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}
