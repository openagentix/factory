package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const testLink = "http://127.0.0.1:18765/#t=KEYMARK-abcdefghijklmnopqrstuvwxyz0123456789"

func privateDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDeliverLinkToATerminalPrintsIt(t *testing.T) {
	var w bytes.Buffer
	dir := privateDir(t)
	shown, err := DeliverLink(&w, true, dir, testLink)
	if err != nil {
		t.Fatal(err)
	}
	if w.String() != testLink+"\n" || shown != testLink {
		t.Errorf("terminal output %q shown %q", w.String(), shown)
	}
	if _, err := os.Stat(filepath.Join(dir, LinkFileName)); err == nil {
		t.Error("a terminal run also wrote a file")
	}
}

func TestDeliverLinkNotATerminalWritesAPrivateFile(t *testing.T) {
	var w bytes.Buffer
	dir := privateDir(t)
	shown, err := DeliverLink(&w, false, dir, testLink)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, LinkFileName)
	if shown != path {
		t.Errorf("shown %q, want the path", shown)
	}
	if strings.Contains(w.String(), "KEYMARK") || !strings.Contains(w.String(), path) {
		t.Errorf("output must hold the path and not the key: %q", w.String())
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode() != 0o600 {
		t.Errorf("file mode %v, want -rw-------", fi.Mode())
	}
	b, _ := os.ReadFile(path)
	if string(b) != testLink+"\n" {
		t.Errorf("file content %q", b)
	}
}

func TestDeliverLinkRefusesAWideOrBadDirectory(t *testing.T) {
	for _, mode := range []os.FileMode{0o755, 0o750, 0o770, 0o701, 0o777, 0o500, 0o000} {
		var w bytes.Buffer
		dir := privateDir(t)
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		shown, err := DeliverLink(&w, false, dir, testLink)
		if err == nil {
			t.Errorf("mode %v: no error", mode)
		}
		if strings.Contains(err.Error(), "KEYMARK") || shown != "" || w.Len() != 0 {
			t.Errorf("mode %v: leaked or wrote: %q %q", mode, shown, w.String())
		}
		_ = os.Chmod(dir, 0o700)
		if _, err := os.Lstat(filepath.Join(dir, LinkFileName)); err == nil {
			t.Errorf("mode %v: a file was written", mode)
		}
	}
	var w bytes.Buffer
	file := filepath.Join(t.TempDir(), "afile")
	_ = os.WriteFile(file, nil, 0o600)
	for _, d := range []string{"", filepath.Join(t.TempDir(), "missing"), file} {
		if _, err := DeliverLink(&w, false, d, testLink); err == nil || strings.Contains(err.Error(), "KEYMARK") {
			t.Errorf("dir %q: err %v", d, err)
		}
	}
	if _, err := DeliverLink(&w, false, privateDir(t), ""); err == nil {
		t.Error("an empty link was accepted")
	}
	if w.Len() != 0 {
		t.Errorf("wrote %q", w.String())
	}
}

func TestDeliverLinkReplacesAnOlderFile(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, LinkFileName)
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil { // not narrowed by the umask
		t.Fatal(err)
	}
	before, _ := os.Lstat(path)
	if _, err := DeliverLink(&bytes.Buffer{}, false, dir, testLink); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(path)
	if os.SameFile(before, fi) {
		t.Error("the old file was reused, not replaced")
	}
	b, _ := os.ReadFile(path)
	if string(b) != testLink+"\n" || fi.Mode() != 0o600 {
		t.Errorf("old file not replaced: %q %v", b, fi.Mode())
	}
}

func TestDeliverLinkDoesNotWriteThroughASymlink(t *testing.T) {
	dir := privateDir(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, LinkFileName)
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if _, err := DeliverLink(&bytes.Buffer{}, false, dir, testLink); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "precious" {
		t.Fatalf("wrote through the symlink: %q", b)
	}
	fi, _ := os.Lstat(path)
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Errorf("link file is %v", fi.Mode())
	}
	// a dangling symlink too
	_ = os.Remove(path)
	_ = os.Symlink(filepath.Join(t.TempDir(), "nothing"), path)
	if _, err := DeliverLink(&bytes.Buffer{}, false, dir, testLink); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveLinkFile(t *testing.T) {
	dir := privateDir(t)
	if err := RemoveLinkFile(dir); err != nil {
		t.Errorf("missing file: %v", err)
	}
	if _, err := DeliverLink(&bytes.Buffer{}, false, dir, testLink); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLinkFile(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, LinkFileName)); err == nil {
		t.Error("file still there")
	}
	// a symlink at the name is removed, not followed
	victim := filepath.Join(t.TempDir(), "victim")
	_ = os.WriteFile(victim, []byte("x"), 0o600)
	_ = os.Symlink(victim, filepath.Join(dir, LinkFileName))
	if err := RemoveLinkFile(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Error("the file behind the symlink was removed")
	}
}

// A directory name with a newline or an escape forges a line on a log or a
// terminal when the path is printed raw. Such a directory is refused, and
// the path is printed quoted.
func TestDeliverLinkRefusesControlCharactersInTheDirectory(t *testing.T) {
	for name, odd := range map[string]string{
		"newline":  "a\nforged line",
		"escape":   "a\x1b[31mred",
		"CR":       "a\rb",
		"tab":      "a\tb",
		"NUL-free": "a\x7fb",
		"bidi":     "a‮b",
	} {
		dir := filepath.Join(t.TempDir(), odd)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Skipf("this file system cannot make %s: %v", name, err)
		}
		_ = os.Chmod(dir, 0o700)
		var w bytes.Buffer
		shown, err := DeliverLink(&w, false, dir, testLink)
		if err == nil || shown != "" || w.Len() != 0 {
			t.Errorf("%s: err=%v shown=%q out=%q", name, err, shown, w.String())
			continue
		}
		if strings.ContainsAny(err.Error(), "\n\r\x1b\x7f‮") || strings.Contains(err.Error(), "KEYMARK") {
			t.Errorf("%s: the error repeats the raw name: %q", name, err.Error())
		}
		if _, err := os.Lstat(filepath.Join(dir, LinkFileName)); err == nil {
			t.Errorf("%s: a file was written", name)
		}
	}
}

func TestDeliverLinkPrintsExactlyOneLineWithTheQuotedPath(t *testing.T) {
	var w bytes.Buffer
	dir := privateDir(t)
	shown, err := DeliverLink(&w, false, dir, testLink)
	if err != nil {
		t.Fatal(err)
	}
	out := w.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Errorf("not one line: %q", out)
	}
	if want := strconv.Quote(shown); !strings.Contains(out, want) {
		t.Errorf("path not quoted: %q does not hold %s", out, want)
	}
}
