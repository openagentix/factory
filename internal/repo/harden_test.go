package repo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A .git planted in the temporary directory must not break or steer the
// commands that need no repository.
func TestNoRepositoryCommandsIgnoreAGitInTheTemporaryDirectory(t *testing.T) {
	planted := t.TempDir()
	for name, content := range map[string]string{
		"HEAD":   "ref: refs/heads/main\n",
		"config": "[core\n\tbroken = \n[core]\n\tfsmonitor = /bin/evil\n",
	} {
		if err := os.MkdirAll(filepath.Join(planted, ".git", "objects"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(planted, ".git", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(planted, ".git", "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	where := filepath.Join(t.TempDir(), "r.git")
	t.Setenv("TMPDIR", planted)
	if err := Init(where); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := Open(where); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ents, _ := os.ReadDir(planted); len(ents) != 1 {
		t.Errorf("the temporary working directory was not removed: %v", ents)
	}
}

func TestGitCommandsAreTiedDownByCeilingAndProcessGroup(t *testing.T) {
	cmd := gitCommand(context.Background(), t.TempDir(), "status")
	if !strings.Contains(strings.Join(cmd.Env, "\n"), "GIT_CEILING_DIRECTORIES=") {
		t.Error("no ceiling directory")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.Cancel == nil {
		t.Error("the command must run in its own process group and be killed as a group")
	}
}

func TestGuard_RefusesAnIndexAndAnotherFormatVersion(t *testing.T) {
	tr := newTrapRepo(t)
	put(t, filepath.Join(tr.dir, "index"), "DIRC", 0o644)
	if err := checkTrusted(context.Background(), tr.dir); err == nil || !strings.Contains(err.Error(), "index") {
		t.Errorf("index: %v", err)
	}
	tr2 := newTrapRepo(t)
	tr2.git("config", "core.repositoryformatversion", "1")
	if err := checkTrusted(context.Background(), tr2.dir); err == nil || !strings.Contains(err.Error(), "repositoryformatversion") {
		t.Errorf("version: %v", err)
	}
	out, err := exec.Command("git", "--git-dir="+tr.dir, "rev-parse", "--show-object-format").CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != "sha1" {
		t.Errorf("object format = %s", out)
	}
}

func TestExport_MakesTheDestinationPrivate(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	dest := filepath.Join(t.TempDir(), "x", "y")
	if _, err := r.Export(context.Background(), tr.head, dest, Limits{}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dest, filepath.Dir(dest)} {
		if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %v %v", d, fi.Mode().Perm(), err)
		}
	}
}

func TestZeroValueRepoFailsEveryMethodAndRunsNothing(t *testing.T) {
	// A marker program on PATH named git would show a command was started.
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx := context.Background()
	for name, r := range map[string]*Repo{"zero value": {}, "nil": nil} {
		_, err1 := r.Resolve(ctx, "HEAD")
		_, err2 := r.NameStatus(ctx, "a", "b")
		_, err3 := r.NumStat(ctx, "a", "b")
		_, err4 := r.Export(ctx, "HEAD", filepath.Join(t.TempDir(), "d"), Limits{})
		_, _, err5 := r.Import(ctx, t.TempDir(), "", "m", testAuthor, ImportOptions{})
		err6 := r.SetRef(ctx, "refs/heads/factory/x", ZeroID, ZeroID)
		err7 := r.SetDefaultMirror(ctx, ZeroID, ZeroID)
		for i, err := range []error{err1, err2, err3, err4, err5, err6, err7} {
			if err == nil || !strings.Contains(err.Error(), "not open") {
				t.Errorf("%s: method %d: %v", name, i, err)
			}
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a git command was started")
	}
}

func objectFiles(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(dir, "objects"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out[p] = true
		}
		return nil
	})
	return out
}

func TestImport_AFailedImportRemovesTheObjectsItWrote(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	for i := 0; i < 20; i++ {
		ws.write(fmt.Sprintf("a%02d.txt", i), fmt.Sprintf("unique content %d\n", i))
	}
	ws.write("zz.txt", "last")
	// The last file is replaced after the others were written.
	importHook = func() {
		os.WriteFile(ws.path("new"), []byte("other"), 0o644)
		os.Rename(ws.path("new"), ws.path("zz.txt"))
	}
	t.Cleanup(func() { importHook = nil })
	before := objectFiles(t, im.dir)
	im.refusedWith(ws, im.head, ImportOptions{}, "zz.txt", "changed")
	after := objectFiles(t, im.dir)
	if len(after) != len(before) {
		t.Errorf("a failed import left %d objects", len(after)-len(before))
	}
	for p := range before {
		if !after[p] {
			t.Errorf("an object that was already there was removed: %s", p)
		}
	}
	if err := checkTrusted(context.Background(), im.dir); err != nil {
		t.Errorf("repository no longer trusted: %v", err)
	}
}

func TestImport_ObjectsAreValidForGit(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("a.txt", "a\n")
	ws.write("a/b.txt", "b\n") // a directory and a file whose names sort close
	ws.write("a.b", "ab\n")
	ws.write("a-b/c", "c\n")
	ws.write("empty.txt", "")
	ws.write("run.sh", "#!/bin/sh\n")
	ws.chmod("run.sh", 0o755)
	ws.symlink("lnk", "a.txt")
	c, _ := im.mustImport(ws, im.head, ImportOptions{})
	im.git("fsck", "--strict", "--no-dangling")
	if got := im.git("show", c+":a/b.txt"); got != "b" {
		t.Errorf("content = %q", got)
	}
	if got := im.git("cat-file", "-s", c+":empty.txt"); got != "0" {
		t.Errorf("empty size = %s", got)
	}
	// The tree must be the one git would make.
	out := im.git("ls-tree", c)
	if !strings.Contains(out, "a-b") {
		t.Error(out)
	}
	fi, _ := os.Stat(filepath.Join(im.dir, "objects", im.git("rev-parse", c+":a.txt")[:2], im.git("rev-parse", c+":a.txt")[2:]))
	if fi == nil || fi.Mode().Perm() != 0o444 {
		t.Errorf("object file mode = %v", fi)
	}
	// The same directory again: the same tree, so no change, and nothing new.
	c2, changed, err := im.imp(ws, c, ImportOptions{})
	if err != nil || changed || c2 != c {
		t.Errorf("c2=%s changed=%v err=%v", c2, changed, err)
	}
}

func TestImport_ExcludedEntriesCountAgainstTheEntryLimit(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	for i := 0; i < 10; i++ {
		ws.write(fmt.Sprintf("junk%d.o", i), "x")
	}
	ws.write("keep.txt", "k")
	im.refusedWith(ws, im.head, ImportOptions{Exclude: []string{"*.o"}, Limits: Limits{MaxEntries: 5}}, "entries", "5")
}

func TestImport_ADirectoryIsReadInChunks(t *testing.T) {
	if listChunk >= 1000 {
		t.Fatalf("listChunk = %d", listChunk)
	}
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.mkdir("d")
	for i := 0; i < listChunk*3+7; i++ {
		ws.write(fmt.Sprintf("d/f%04d", i), "x")
	}
	c, _ := im.mustImport(ws, im.head, ImportOptions{})
	if n := strings.Count(im.git("ls-tree", c+":d"), "\n") + 1; n != listChunk*3+7 {
		t.Errorf("files = %d", n)
	}
	// The limit stops the listing: it is checked as names are read.
	im.refusedWith(ws, im.head, ImportOptions{Limits: Limits{MaxEntries: listChunk + 5}}, "entries")
}

func TestImport_StopsAtTheTimeLimitWithATypedError(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	for i := 0; i < 50; i++ {
		ws.write(fmt.Sprintf("f%02d", i), "x")
	}
	importHook = func() { time.Sleep(300 * time.Millisecond) }
	t.Cleanup(func() { importHook = nil })
	before := objectFiles(t, im.dir)
	_, _, err := im.imp(ws, im.head, ImportOptions{Limits: Limits{MaxDuration: 100 * time.Millisecond}})
	var te *TimeoutError
	if !errors.As(err, &te) || te.Op != "import" {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if len(objectFiles(t, im.dir)) != len(before) {
		t.Error("objects were left behind")
	}
	// A caller's own cancel is not reported as a time limit.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = im.r.Import(ctx, ws.dir, im.head, "m", testAuthor, ImportOptions{})
	if err == nil {
		t.Fatal(err)
	}
	if errors.As(err, &te) {
		t.Errorf("a cancelled context must not be a TimeoutError: %v", err)
	}
}

func TestExport_StopsAtTheTimeLimitWithATypedError(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	_, err := r.Export(context.Background(), tr.head, filepath.Join(t.TempDir(), "d"), Limits{MaxDuration: time.Nanosecond})
	var te *TimeoutError
	if !errors.As(err, &te) || te.Op != "export" {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestRunGitKillsTheProcessGroupWhenTheContextEnds(t *testing.T) {
	// A fake git that starts a child and waits. When the context ends, both
	// must be gone.
	bin := t.TempDir()
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	script := "#!/bin/sh\nsleep 30 &\necho $! > '" + pidfile + "'\nwait\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() { // end the context once the child exists
		for ctx.Err() == nil {
			if b, _ := os.ReadFile(pidfile); strings.Contains(string(b), "\n") {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	start := time.Now()
	_, err := runGit(ctx, t.TempDir(), "status")
	if err == nil || time.Since(start) > 10*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
	b, _ := os.ReadFile(pidfile)
	var pid int
	fmt.Sscan(strings.TrimSpace(string(b)), &pid)
	if pid == 0 {
		t.Fatal("the child did not start")
	}
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("the child process %d is still running", pid)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestExport_SkipsSuspiciousNamesAndSaysSo(t *testing.T) {
	tr := newTrapRepo(t)
	// A tree the factory did not make: names git itself accepts.
	tr.write("ok.txt", "ok\n")
	names := []string{"bidi\u202egpj.exe", "zero\u200bwidth", "ctl\x01x", "back\\slash"}
	for _, n := range names {
		tr.write("d/"+n, "x")
	}
	head := tr.commit("odd names")
	r, _ := Open(tr.dir)
	dest := filepath.Join(t.TempDir(), "out")
	notes, err := r.Export(context.Background(), head, dest, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "ok.txt")); err != nil {
		t.Error("ok.txt missing")
	}
	ents, _ := os.ReadDir(filepath.Join(dest, "d"))
	if len(ents) != 0 {
		t.Errorf("suspicious names were written: %v", ents)
	}
	if got := strings.Count(strings.Join(notes, "\n"), "skipped path"); got != len(names) {
		t.Errorf("notes = %d, want %d:\n%s", got, len(names), strings.Join(notes, "\n"))
	}
}
