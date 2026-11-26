package repo

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var testAuthor = Author{Name: "Factory Bot", Email: "bot@example.invalid"}

var fixedTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func fixedNow() time.Time { return fixedTime }

// workspace is a plain directory that stands for the agent's work.
type workspace struct {
	t   *testing.T
	dir string
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	return &workspace{t: t, dir: filepath.Join(t.TempDir(), "ws")}
}

func (w *workspace) path(rel string) string { return filepath.Join(w.dir, filepath.FromSlash(rel)) }

func (w *workspace) write(rel, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.path(rel)), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(w.path(rel), []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) mkdir(rel string) {
	w.t.Helper()
	if err := os.MkdirAll(w.path(rel), 0o755); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) chmod(rel string, mode os.FileMode) {
	w.t.Helper()
	if err := os.Chmod(w.path(rel), mode); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) symlink(rel, target string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.path(rel)), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Symlink(target, w.path(rel)); err != nil {
		w.t.Fatal(err)
	}
}

// importer is a trusted repository with one commit, and a way to import.
type importer struct {
	*trapRepo
	r *Repo
}

func newImporter(t *testing.T) *importer {
	t.Helper()
	tr := newTrapRepo(t)
	r, err := Open(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	return &importer{trapRepo: tr, r: r}
}

func (im *importer) imp(ws *workspace, parent string, opts ImportOptions) (string, bool, error) {
	im.t.Helper()
	if opts.Now == nil {
		opts.Now = fixedNow
	}
	return im.r.Import(context.Background(), ws.dir, parent, "import", testAuthor, opts)
}

// mustImport imports and fails the test on an error.
func (im *importer) mustImport(ws *workspace, parent string, opts ImportOptions) (string, bool) {
	im.t.Helper()
	c, changed, err := im.imp(ws, parent, opts)
	if err != nil {
		im.t.Fatalf("Import: %v", err)
	}
	return c, changed
}

// tree lists "mode path" lines of a commit.
func (im *importer) tree(commit string) []string {
	im.t.Helper()
	raw := im.git("ls-tree", "-r", "-t", commit)
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		meta, name, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		lines = append(lines, f[0]+" "+name)
	}
	return lines
}

func (im *importer) has(commit, want string) bool {
	for _, l := range im.tree(commit) {
		if l == want {
			return true
		}
	}
	return false
}

// refusedWith asserts an Import error that mentions every part.
func (im *importer) refusedWith(ws *workspace, parent string, opts ImportOptions, parts ...string) {
	im.t.Helper()
	c, changed, err := im.imp(ws, parent, opts)
	if err == nil {
		im.t.Fatalf("Import must be refused, got commit %s changed=%v", c, changed)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			im.t.Errorf("error %q does not mention %q", err, p)
		}
	}
	if changed || c != "" {
		im.t.Errorf("a refused import must return no commit: %q %v", c, changed)
	}
}

func TestImport_RecordsAFileAsOneCommitOnTheParent(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("main.go", "package a\n// changed\n")
	ws.write("new/file.txt", "new\n")
	c, changed := im.mustImport(ws, im.head, ImportOptions{})
	if !changed || c == "" || c == im.head {
		t.Fatalf("commit=%q changed=%v", c, changed)
	}
	if got := im.git("rev-parse", c+"^"); got != im.head {
		t.Errorf("parent = %s, want %s", got, im.head)
	}
	if n := im.git("rev-list", "--count", im.head+".."+c); n != "1" {
		t.Errorf("commits added = %s, want 1", n)
	}
	if got := im.git("show", c+":new/file.txt"); got != "new" {
		t.Errorf("content = %q", got)
	}
	// The workspace's tree replaces the parent's: .gitattributes is gone.
	if im.has(c, "100644 .gitattributes") {
		t.Error("a file that is not in the directory must not be in the commit")
	}
}

func TestImport_CommitHasTheGivenAuthorAndTimeAndNoSignature(t *testing.T) {
	// A hostile environment: identity, a signing setup and a global config.
	t.Setenv("GIT_AUTHOR_NAME", "Leaked Name")
	t.Setenv("GIT_COMMITTER_EMAIL", "leaked@example.invalid")
	t.Setenv("GIT_AUTHOR_DATE", "2001-01-01T00:00:00Z")
	im := newImporter(t)
	marker := filepath.Join(im.markers, "gpg")
	prog := im.script("gpg")
	global := filepath.Join(t.TempDir(), "gitconfig")
	cfg := "[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = " + prog + "\n[user]\n\tname = Global Name\n"
	if err := os.WriteFile(global, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("HOME", filepath.Dir(global))
	ws := newWorkspace(t)
	ws.write("a.txt", "a\n")
	c, _ := im.mustImport(ws, "", ImportOptions{})
	raw := im.git("cat-file", "commit", c)
	if !strings.Contains(raw, "author Factory Bot <bot@example.invalid> 1767323045 +0000") {
		t.Errorf("author line wrong:\n%s", raw)
	}
	if !strings.Contains(raw, "committer Factory Bot <bot@example.invalid> 1767323045 +0000") {
		t.Errorf("committer line wrong:\n%s", raw)
	}
	if strings.Contains(raw, "gpgsig") {
		t.Errorf("the commit is signed:\n%s", raw)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a signing program ran")
	}
	if !strings.HasSuffix(raw, "\n\nimport") {
		t.Errorf("message wrong:\n%s", raw)
	}
}

func TestImport_RejectsBadAuthorAndParent(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("a.txt", "a\n")
	ctx := context.Background()
	for _, a := range []Author{{"", "a@b.invalid"}, {"N", ""}, {"N\nM", "a@b.invalid"}, {"N", "a@b>.invalid"}, {"N<", "a@b.invalid"}, {"N", "a\x00@b"}} {
		if _, _, err := im.r.Import(ctx, ws.dir, im.head, "m", a, ImportOptions{}); err == nil {
			t.Errorf("author %+v must be refused", a)
		}
	}
	for _, p := range []string{"no-such-commit", "-x", im.git("rev-parse", im.head+"^{tree}")} {
		if _, _, err := im.r.Import(ctx, ws.dir, p, "m", testAuthor, ImportOptions{}); err == nil {
			t.Errorf("parent %q must be refused", p)
		}
	}
}

func TestImport_ModesAreOnly100644And100755(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("plain", "p\n")
	ws.write("run.sh", "#!/bin/sh\n")
	ws.chmod("run.sh", 0o755)
	ws.write("setuid", "s\n")
	ws.chmod("setuid", os.ModeSetuid|0o755)
	ws.write("setgid", "s\n")
	ws.chmod("setgid", os.ModeSetgid|0o644)
	ws.write("sticky", "s\n")
	ws.chmod("sticky", os.ModeSticky|0o644)
	ws.write("private", "s\n")
	ws.chmod("private", 0o600)
	c, _ := im.mustImport(ws, "", ImportOptions{})
	for _, w := range []string{"100644 plain", "100755 run.sh", "100755 setuid", "100644 setgid", "100644 sticky", "100644 private"} {
		if !im.has(c, w) {
			t.Errorf("tree lacks %q:\n%s", w, strings.Join(im.tree(c), "\n"))
		}
	}
}

func TestImport_RoundTrip(t *testing.T) {
	im := newImporter(t)
	ctx := context.Background()
	// Seed a commit with the odd things a tree can hold.
	ws0 := newWorkspace(t)
	ws0.write("src/main.go", "package a\n")
	ws0.write("bin/run.sh", "#!/bin/sh\n")
	ws0.chmod("bin/run.sh", 0o755)
	ws0.write("with space.txt", "s\n")
	ws0.write("-dash.txt", "d\n")
	ws0.symlink("src/link", "main.go")
	ws0.symlink("src/up", "../bin/run.sh")
	ws0.mkdir("empty/inner")
	c0, changed := im.mustImport(ws0, im.head, ImportOptions{})
	if !changed {
		t.Fatal("first import must change")
	}
	if im.has(c0, "040000 empty") || im.has(c0, "040000 empty/inner") {
		t.Error("an empty directory must not be recorded")
	}
	for _, w := range []string{"100755 bin/run.sh", "100644 with space.txt", "100644 -dash.txt", "120000 src/link", "120000 src/up"} {
		if !im.has(c0, w) {
			t.Errorf("tree lacks %q:\n%s", w, strings.Join(im.tree(c0), "\n"))
		}
	}
	if got := im.git("show", c0+":src/up"); got != "../bin/run.sh" {
		t.Errorf("link target = %q", got)
	}

	// Export it, import the unchanged directory: no change, no new commit.
	out := filepath.Join(t.TempDir(), "out")
	if _, err := im.r.Export(ctx, c0, out, Limits{}); err != nil {
		t.Fatal(err)
	}
	before := im.git("rev-list", "--all", "--count")
	c1, changed, err := im.r.Import(ctx, out, c0, "again", testAuthor, ImportOptions{Now: fixedNow})
	if err != nil || changed || c1 != c0 {
		t.Fatalf("c1=%q changed=%v err=%v, want %q false", c1, changed, err, c0)
	}
	if after := im.git("rev-list", "--all", "--count"); after != before {
		t.Errorf("a commit was made without a change: %s -> %s", before, after)
	}
	if _, err := os.Lstat(filepath.Join(out, ".git")); err == nil {
		t.Error("export made a .git")
	}

	// Change one file: one commit whose diff is that file.
	if err := os.WriteFile(filepath.Join(out, "src/main.go"), []byte("package a\n// two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c2, changed, err := im.r.Import(ctx, out, c0, "edit", testAuthor, ImportOptions{Now: fixedNow})
	if err != nil || !changed || c2 == c0 {
		t.Fatalf("c2=%q changed=%v err=%v", c2, changed, err)
	}
	ns, err := im.r.NameStatus(ctx, c0, c2)
	if err != nil {
		t.Fatal(err)
	}
	if string(ns) != "M\x00src/main.go\x00" {
		t.Errorf("diff = %q", ns)
	}
	if im.git("rev-parse", c2+"^") != c0 {
		t.Error("wrong parent")
	}
}

func TestImport_ARootCommitHasNoParent(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("a", "a")
	c, changed := im.mustImport(ws, "", ImportOptions{})
	if !changed || strings.Contains(im.git("cat-file", "commit", c), "parent ") {
		t.Errorf("changed=%v", changed)
	}
}

func TestImport_AFileNamedWithANewlineOrControlCharacterIsRefused(t *testing.T) {
	// The ticket asks for refusal of control characters in names. A newline is
	// one, and the spaces and the leading dash of the round trip are not.
	for _, name := range []string{"a\nb.txt", "tab\there", "esc\x1b[0m", "bell\x07"} {
		t.Run(name, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			ws.write("ok.txt", "ok")
			ws.write("dir/"+name, "x")
			im.refusedWith(ws, im.head, ImportOptions{}, "dir/", "control")
		})
	}
}

func TestImport_RefusesInvisibleAndBidiCharacters(t *testing.T) {
	for name, file := range map[string]string{
		"zero width space":  "a\u200bb.go",
		"bidi override":     "evil\u202egpj.exe",
		"bidi isolate":      "a\u2066b",
		"zero width joiner": "a\u200db",
		"byte order mark":   "\ufeffa",
		"line separator":    "a\u2028b",
	} {
		t.Run(name, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			ws.write("d/"+file, "x")
			im.refusedWith(ws, im.head, ImportOptions{}, "d/", "invisible")
		})
	}
}

func TestCheckComponent(t *testing.T) {
	bad := map[string]string{
		"": "empty", ".": "not allowed", "..": "not allowed", "\xff": "UTF-8", "a\xc0b": "UTF-8",
		"a/b": "slash", "a\\b": "backslash", ".git": ".git", ".GIT": ".git", ".Git": ".git", "a\x00b": "control",
	}
	for name, want := range bad {
		err := checkComponent(name)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("checkComponent(%q) = %v, want one mentioning %q", name, err, want)
		}
	}
	for _, ok := range []string{"a", "a b", "-dash", ".gitignore", ".github", "a.git", "git", "caf\u00e9", "...", "日本語"} {
		if err := checkComponent(ok); err != nil {
			t.Errorf("checkComponent(%q) = %v", ok, err)
		}
	}
}

func TestImport_RefusesInvalidUTF8NamesWhenTheFileSystemAllowsThem(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.mkdir("d")
	if err := os.WriteFile(filepath.Join(ws.dir, "d", "bad\xffname"), []byte("x"), 0o644); err != nil {
		t.Skipf("this file system refuses invalid UTF-8 names: %v", err)
	}
	im.refusedWith(ws, im.head, ImportOptions{}, "d/", "UTF-8")
}

func TestImport_RefusesGitAtAnyDepthAndLetterCase(t *testing.T) {
	for _, p := range []string{".git", ".GIT", ".Git", "a/.git", "a/b/c/.gIt", "a/.git/config", "a/.GIT/hooks/post-checkout"} {
		t.Run(p, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			ws.write("ok.txt", "ok")
			// As a file, or as a directory with a file in it.
			if strings.Contains(p, "/.") && !strings.HasSuffix(p, ".git") && !strings.HasSuffix(p, ".gIt") {
				ws.write(p, "x")
			} else {
				ws.write(p, "gitdir: /elsewhere\n")
			}
			im.refusedWith(ws, im.head, ImportOptions{}, ".git")
		})
	}
}

func TestImport_AnEmptyGitDirectoryIsRefusedToo(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("ok.txt", "ok")
	ws.mkdir("sub/.git")
	im.refusedWith(ws, im.head, ImportOptions{}, "sub/.git")
}

func TestImport_ExcludeCannotHideAGitDirectory(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("ok.txt", "ok")
	ws.write(".git/config", "[core]\n")
	im.refusedWith(ws, im.head, ImportOptions{Exclude: []string{".git", ".git/**", "**"}}, ".git")
}

func TestImport_RefusesCaseCollisions(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.mkdir("d")
	for _, n := range []string{"README.md", "readme.md"} {
		if err := os.WriteFile(filepath.Join(ws.dir, "d", n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(ws.dir, "d")); len(ents) < 2 {
		t.Skip("this file system folds letter case, so the two names cannot exist together")
	}
	im.refusedWith(ws, im.head, ImportOptions{}, "d/", "letter case")
}

func TestFoldKeyFindsCaseDifferences(t *testing.T) {
	if foldKey("README") != foldKey("readme") {
		t.Error("foldKey must unify letter case")
	}
	if foldKey("a") == foldKey("b") {
		t.Error("different names must differ")
	}
}

func TestImport_SymbolicLinks(t *testing.T) {
	good := map[string]string{"link": "a.txt", "d/up": "../a.txt", "d/same": "x.txt", "d/e/up2": "../../a.txt"}
	bad := map[string]string{
		"abs":          "/etc/passwd",
		"esc":          "../outside",
		"d/esc2":       "../../outside",
		"dotdot-after": "d/../../outside",
		"dot":          "./a.txt",
		"empty-part":   "d//x",
		"trailing":     "a.txt/",
		"backslash":    `a\b`,
		"ctl":          "a\x01b",
		"inside-dots":  "d/../a.txt", // inside, but not the plain form export writes
	}
	for rel, target := range bad {
		t.Run("refuse "+rel, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			ws.write("a.txt", "a")
			ws.symlink(rel, target)
			im.refusedWith(ws, im.head, ImportOptions{}, rel, "link")
		})
	}
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("a.txt", "a")
	ws.write("d/x.txt", "x")
	ws.mkdir("d/e")
	for rel, target := range good {
		ws.symlink(rel, target)
	}
	c, _ := im.mustImport(ws, im.head, ImportOptions{})
	for rel, target := range good {
		if !im.has(c, "120000 "+rel) {
			t.Errorf("link %s not recorded", rel)
		}
		if got := im.git("show", c+":"+rel); got != target {
			t.Errorf("link %s = %q, want %q", rel, got, target)
		}
	}
}

func TestImport_NeverFollowsALink(t *testing.T) {
	// A link to a directory outside, and a link to a secret file outside. If
	// Import followed either, the secret would be in the commit.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOP-SECRET-VALUE"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"dirlink": outside, "filelink": filepath.Join(outside, "secret.txt"), "relfile": "../../../../../../../../.." + filepath.Join(outside, "secret.txt")} {
		t.Run(name, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			ws.write("ok.txt", "ok")
			ws.symlink(name, target)
			im.refusedWith(ws, im.head, ImportOptions{}, name)
		})
	}
	// A link to an inside directory is recorded as a link, and the files
	// behind it are recorded once, under their own name.
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("real/f.txt", "f")
	ws.symlink("alias", "real")
	c, _ := im.mustImport(ws, im.head, ImportOptions{})
	if !im.has(c, "120000 alias") || im.has(c, "100644 alias/f.txt") {
		t.Errorf("link to a directory must stay a link:\n%s", strings.Join(im.tree(c), "\n"))
	}
}

func TestImport_RefusesTheDirectoryItselfBeingALink(t *testing.T) {
	im := newImporter(t)
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	ln := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, ln); err != nil {
		t.Fatal(err)
	}
	if _, _, err := im.r.Import(context.Background(), ln, im.head, "m", testAuthor, ImportOptions{}); err == nil {
		t.Error("a link as the directory must be refused")
	}
	if _, _, err := im.r.Import(context.Background(), filepath.Join(t.TempDir(), "absent"), im.head, "m", testAuthor, ImportOptions{}); err == nil {
		t.Error("a missing directory must be refused")
	}
}

func TestImport_RefusesSpecialFiles(t *testing.T) {
	t.Run("named pipe", func(t *testing.T) {
		im := newImporter(t)
		ws := newWorkspace(t)
		ws.write("ok.txt", "ok")
		if err := syscall.Mkfifo(ws.path("d-pipe"), 0o644); err != nil {
			t.Skip(err)
		}
		done := make(chan struct{})
		go func() { defer close(done); im.refusedWith(ws, im.head, ImportOptions{}, "d-pipe", "special") }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("Import blocked on a named pipe")
		}
	})
	t.Run("socket", func(t *testing.T) {
		im := newImporter(t)
		short, err := os.MkdirTemp("/tmp", "oxs")
		if err != nil {
			t.Skip(err)
		}
		t.Cleanup(func() { os.RemoveAll(short) })
		ws := &workspace{t: t, dir: short}
		ws.write("ok.txt", "ok")
		l, err := net.Listen("unix", filepath.Join(short, "sock"))
		if err != nil {
			t.Skip(err)
		}
		defer l.Close()
		im.refusedWith(ws, im.head, ImportOptions{}, "sock", "special")
	})
	t.Run("device and the other kinds", func(t *testing.T) {
		// A device cannot be made without privilege, so the test checks the
		// rule that classifies a file mode.
		for _, m := range []os.FileMode{os.ModeDevice, os.ModeDevice | os.ModeCharDevice, os.ModeNamedPipe, os.ModeSocket, os.ModeIrregular} {
			if specialKind(m) == "" {
				t.Errorf("mode %v must be refused", m)
			}
		}
		for _, m := range []os.FileMode{0, 0o755, os.ModeSetuid, os.ModeSticky} {
			if k := specialKind(m); k != "" {
				t.Errorf("mode %v is a plain file, got %q", m, k)
			}
		}
	})
}

func TestImport_RefusesAHardLinkedFile(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("a.txt", "a")
	outside := filepath.Join(t.TempDir(), "o.txt")
	if err := os.WriteFile(outside, []byte("shared with something outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, ws.path("linked.txt")); err != nil {
		t.Skip(err)
	}
	im.refusedWith(ws, im.head, ImportOptions{}, "linked.txt", "hard link")

	// Two names for one file inside the directory: both are refused.
	im2 := newImporter(t)
	ws2 := newWorkspace(t)
	ws2.write("one.txt", "1")
	if err := os.Link(ws2.path("one.txt"), ws2.path("two.txt")); err != nil {
		t.Skip(err)
	}
	im2.refusedWith(ws2, im2.head, ImportOptions{}, "hard link")
}

func TestImport_AttributesIgnoreFilesAndFiltersChangeNothing(t *testing.T) {
	// With "git add" these files would change what is recorded: "ident"
	// expands $Id$, "eol=crlf" rewrites line ends, a filter would run a
	// program, and an ignore file would drop files. Import reads the
	// directory itself, so none of it applies.
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write(".gitattributes", "* ident\n*.txt text eol=crlf\n*.bin filter=evil\n* -text\n")
	ws.write(".gitignore", "*.log\nbuild/\nsecret*\n")
	ws.write("a.txt", "line1\nline2 $Id$\n")
	ws.write("data.bin", "bin\x00data")
	ws.write("run.log", "log\n")
	ws.write("build/out", "out\n")
	ws.write("secret.key", "k\n")
	ws.write(".gitkeep", "x") // a name that only starts like .git
	c, _ := im.mustImport(ws, im.head, ImportOptions{})
	if got := im.git("show", c+":a.txt"); got != "line1\nline2 $Id$" {
		t.Errorf("content changed: %q", got)
	}
	// cat-file keeps bytes exactly.
	if got := im.git("cat-file", "-s", c+":a.txt"); got != "17" {
		t.Errorf("size = %s", got)
	}
	for _, w := range []string{"100644 run.log", "100644 build/out", "100644 secret.key", "100644 .gitignore", "100644 .gitattributes", "100644 data.bin"} {
		if !im.has(c, w) {
			t.Errorf("%q must be recorded: ignore files are not honoured\n%s", w, strings.Join(im.tree(c), "\n"))
		}
	}
	if got := im.ran(); len(got) != 0 {
		t.Errorf("a program ran: %v", got)
	}
}

func TestImport_ExcludePatterns(t *testing.T) {
	ws := func(t *testing.T) *workspace {
		w := newWorkspace(t)
		w.write("main.go", "m")
		w.write("build/out.o", "o")
		w.write("build/deep/x", "x")
		w.write("pkg/a.o", "o")
		w.write("pkg/sub/b.o", "o")
		w.write("pkg/sub/keep.go", "k")
		w.write("node_modules/x/index.js", "j")
		w.write("src/node_modules/y.js", "j")
		w.write("notes.txt", "n")
		return w
	}
	tests := []struct {
		name    string
		exclude []string
		want    []string
		not     []string
	}{
		{"none by default", nil, []string{"100644 build/out.o", "100644 node_modules/x/index.js", "100644 pkg/a.o"}, nil},
		{"a directory", []string{"build"}, []string{"100644 main.go", "100644 pkg/a.o"}, []string{"100644 build/out.o", "100644 build/deep/x"}},
		{"any depth", []string{"**/node_modules"}, []string{"100644 main.go"}, []string{"100644 node_modules/x/index.js", "100644 src/node_modules/y.js"}},
		{"extension at any depth", []string{"**/*.o"}, []string{"100644 pkg/sub/keep.go"}, []string{"100644 pkg/a.o", "100644 pkg/sub/b.o", "100644 build/out.o"}},
		{"star does not cross a slash", []string{"*.o"}, []string{"100644 pkg/a.o", "100644 build/out.o"}, nil},
		{"anchored at the root", []string{"node_modules"}, []string{"100644 src/node_modules/y.js"}, []string{"100644 node_modules/x/index.js"}},
		{"one file", []string{"notes.txt"}, []string{"100644 main.go"}, []string{"100644 notes.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			im := newImporter(t)
			c, _ := im.mustImport(ws(t), im.head, ImportOptions{Exclude: tt.exclude})
			for _, w := range tt.want {
				if !im.has(c, w) {
					t.Errorf("tree lacks %q:\n%s", w, strings.Join(im.tree(c), "\n"))
				}
			}
			for _, n := range tt.not {
				if im.has(c, n) {
					t.Errorf("tree has %q", n)
				}
			}
		})
	}
	im := newImporter(t)
	for _, bad := range []string{"", "/abs", "../x", "a/../b", "a\\b"} {
		if _, _, err := im.imp(ws(t), im.head, ImportOptions{Exclude: []string{bad}}); err == nil {
			t.Errorf("pattern %q must be refused", bad)
		}
	}
}

func TestImport_ExcludedSpecialFilesAreNotAProblem(t *testing.T) {
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write("ok.txt", "ok")
	ws.mkdir("build")
	if err := syscall.Mkfifo(ws.path("build/pipe"), 0o644); err != nil {
		t.Skip(err)
	}
	ws.symlink("build/abs", "/etc/passwd")
	im.mustImport(ws, im.head, ImportOptions{Exclude: []string{"build"}})
}

func TestImport_Limits(t *testing.T) {
	big := func(w *workspace) {
		w.write("a.txt", strings.Repeat("a", 100))
		w.write("b.txt", strings.Repeat("b", 100))
		w.write("c.txt", "c")
	}
	tests := []struct {
		name  string
		setup func(w *workspace)
		lim   Limits
		want  []string
	}{
		{"one file too big", big, Limits{MaxFileBytes: 99}, []string{"a.txt", "over the limit of 99"}},
		{"total too big", big, Limits{MaxTotalBytes: 150}, []string{"total", "150"}},
		{"too many entries", big, Limits{MaxEntries: 2}, []string{"entries", "2"}},
		{"directories count as entries", func(w *workspace) { w.write("a/b/c/d.txt", "x") }, Limits{MaxEntries: 3}, []string{"entries"}},
		{"path too long", func(w *workspace) { w.write("dir/"+strings.Repeat("n", 40), "x") }, Limits{MaxPathBytes: 30}, []string{"path", "30"}},
		{"too deep", func(w *workspace) { w.write("a/b/c/d/e.txt", "x") }, Limits{MaxDepth: 3}, []string{"deep", "3"}},
		{"link target counts as bytes", func(w *workspace) { w.write("a.txt", "a"); w.symlink("l", "a.txt") }, Limits{MaxTotalBytes: 5}, []string{"total"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			tt.setup(ws)
			before := im.git("rev-list", "--all", "--count")
			im.refusedWith(ws, im.head, ImportOptions{Limits: tt.lim}, tt.want...)
			if after := im.git("rev-list", "--all", "--count"); after != before {
				t.Error("a commit was made")
			}
		})
	}
	// Exactly at the limit is fine.
	im := newImporter(t)
	ws := newWorkspace(t)
	big(ws)
	im.mustImport(ws, im.head, ImportOptions{Limits: Limits{MaxTotalBytes: 201, MaxFileBytes: 100, MaxEntries: 3, MaxDepth: 1, MaxPathBytes: 5}})
}

func TestImport_LimitsDefault(t *testing.T) {
	l := Limits{}.WithDefaults()
	if l.MaxTotalBytes != 2<<30 || l.MaxFileBytes != 200<<20 || l.MaxEntries != 200000 || l.MaxPathBytes != 4096 || l.MaxDepth != 64 {
		t.Errorf("defaults = %+v", l)
	}
	if got := (Limits{MaxDepth: 7}).WithDefaults(); got.MaxDepth != 7 || got.MaxEntries != 200000 {
		t.Errorf("a set field must stay: %+v", got)
	}
}

// A file swapped for a link between the listing and the reading must not be
// followed. importHook runs once the directory is listed and before any file
// is read.
func TestImport_AFileSwappedForALinkIsNotFollowed(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-VALUE"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, swap := range map[string]func(ws *workspace){
		"link to a file outside": func(ws *workspace) {
			os.Remove(ws.path("a.txt"))
			os.Symlink(secret, ws.path("a.txt"))
		},
		"link to a file inside": func(ws *workspace) {
			os.Remove(ws.path("a.txt"))
			os.Symlink("b.txt", ws.path("a.txt"))
		},
		"a directory": func(ws *workspace) {
			os.Remove(ws.path("a.txt"))
			os.Mkdir(ws.path("a.txt"), 0o755)
		},
		"another file with the same name": func(ws *workspace) {
			os.WriteFile(ws.path("new"), []byte("different"), 0o644)
			os.Rename(ws.path("new"), ws.path("a.txt"))
		},
		"a hard link to a file outside": func(ws *workspace) {
			os.Remove(ws.path("a.txt"))
			os.Link(secret, ws.path("a.txt"))
		},
		"a directory swapped for a link": func(ws *workspace) {
			os.RemoveAll(ws.path("d"))
			os.Symlink(filepath.Dir(secret), ws.path("d"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			im := newImporter(t)
			ws := newWorkspace(t)
			ws.write("a.txt", "a")
			ws.write("b.txt", "b")
			ws.write("d/inner.txt", "inner")
			importHook = func() { swap(ws) }
			t.Cleanup(func() { importHook = nil })
			before := im.git("rev-list", "--all", "--count")
			c, changed, err := im.imp(ws, im.head, ImportOptions{})
			if err == nil {
				t.Fatalf("a swapped entry must fail the import, got %s changed=%v", c, changed)
			}
			if after := im.git("rev-list", "--all", "--count"); after != before {
				t.Error("a commit was made")
			}
			if strings.Contains(err.Error(), "TOP-SECRET") {
				t.Error("the error leaks content")
			}
		})
	}
}

func TestImport_NoMarkerProgramRunsFromTheWorkspace(t *testing.T) {
	// Everything a workspace can hold that git might act on, all at once.
	im := newImporter(t)
	ws := newWorkspace(t)
	ws.write(".gitattributes", "* filter=x diff=x merge=x\n")
	ws.write(".gitmodules", "[submodule \"s\"]\n\tpath = s\n\turl = https://example.invalid/s\n")
	ws.write(".gitignore", "*\n")
	ws.write("HEAD", "ref: refs/heads/x\n")
	ws.write("config", "[core]\n\tfsmonitor = "+im.script("wscfg")+"\n")
	ws.write("hooks/post-commit", "#!/bin/sh\ntouch "+filepath.Join(im.markers, "wshook")+"\n")
	ws.chmod("hooks/post-commit", 0o755)
	ws.write("objects/x", "x")
	im.mustImport(ws, im.head, ImportOptions{})
	if got := im.ran(); len(got) != 0 {
		t.Errorf("programs ran: %v", got)
	}
	if err := checkTrusted(context.Background(), im.dir); err != nil {
		t.Errorf("the repository is no longer trusted: %v", err)
	}
}
