package repo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rp is a repository for Replay tests. Commits are made with git plumbing and
// a private index, never a work tree.
type rp struct {
	t   *testing.T
	tr  *testRepo
	r   *Repo
	idx string
}

func newRP(t *testing.T) *rp {
	t.Helper()
	tr := newTestRepo(t)
	r, err := Open(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	return &rp{t: t, tr: tr, r: r, idx: filepath.Join(t.TempDir(), "idx")}
}

// plumb runs git on the repository with stdin and returns trimmed output.
func (p *rp) plumb(stdin string, args ...string) string {
	p.t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir=" + p.tr.dir, "-c", "gc.auto=0", "-c", "maintenance.auto=false"}, args...)...)
	cmd.Dir = p.t.TempDir()
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_INDEX_FILE="+p.idx,
		"GIT_AUTHOR_NAME=Seed", "GIT_AUTHOR_EMAIL=seed@example.invalid", "GIT_COMMITTER_NAME=Seed", "GIT_COMMITTER_EMAIL=seed@example.invalid")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		p.t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// ent is one tree entry to set. A nil content with mode "" removes the path.
type ent struct {
	path, mode, content string
	remove              bool
}

func f(path, content string) ent { return ent{path: path, mode: "100644", content: content} }
func rm(path string) ent         { return ent{path: path, remove: true} }

// commit makes a commit on top of parent ("" for a root) with the entries
// applied to the parent's tree. More than one parent: use commitMerge.
func (p *rp) commit(parent string, es ...ent) string {
	p.t.Helper()
	var parents []string
	if parent != "" {
		parents = []string{parent}
	}
	return p.commitWith(parents, "c", es...)
}

func (p *rp) commitMerge(a, b string, es ...ent) string {
	return p.commitWith([]string{a, b}, "merge", es...)
}

func (p *rp) commitWith(parents []string, msg string, es ...ent) string {
	p.t.Helper()
	_ = os.Remove(p.idx)
	if len(parents) > 0 {
		p.plumb("", "read-tree", parents[0])
	}
	var info strings.Builder
	for _, e := range es {
		switch {
		case e.remove:
			info.WriteString("0 " + ZeroID + "\t" + e.path + "\n")
		case e.mode == "160000":
			info.WriteString("160000 " + e.content + "\t" + e.path + "\n")
		default:
			oid := p.plumb(e.content, "hash-object", "-w", "--stdin")
			info.WriteString(e.mode + " " + oid + "\t" + e.path + "\n")
		}
	}
	if info.Len() > 0 {
		p.plumb(info.String(), "update-index", "--add", "--replace", "--index-info")
	}
	tree := p.plumb("", "write-tree")
	args := []string{"commit-tree", tree, "-m", msg}
	for _, par := range parents {
		args = append(args, "-p", par)
	}
	return p.plumb("", args...)
}

func (p *rp) tree(commit string) string { return p.plumb("", "rev-parse", commit+"^{tree}") }

func (p *rp) show(commit, path string) string {
	p.t.Helper()
	return p.plumb("", "cat-file", "blob", commit+":"+path)
}

func (p *rp) refs() string {
	return p.plumb("", "for-each-ref") + "|" + p.plumb("", "symbolic-ref", "HEAD")
}

func (p *rp) raw(commit string) string { return p.plumb("", "cat-file", "commit", commit) }

var replayAuthor = Author{Name: "Staging Bot", Email: "bot@example.invalid"}

const lines3 = "1\n2\n3\n"

// threeWay makes base, onto (edits line 2 of f) and change (edits line 2 of f
// or whatever es says), the change being a child of base.
func (p *rp) threeWay(baseEs, ontoEs, changeEs []ent) (base, onto, change string) {
	p.t.Helper()
	base = p.commit("", baseEs...)
	onto = p.commit(base, ontoEs...)
	change = p.commit(base, changeEs...)
	return
}

func replay(t *testing.T, p *rp, onto, change, want string) (string, ReplayOutcome, error) {
	t.Helper()
	return p.r.Replay(context.Background(), onto, change, want, replayAuthor, "Replay the change")
}

func TestReplayOutcomeZeroValueIsNotApplied(t *testing.T) {
	var o ReplayOutcome
	if o == ReplayApplied || o == ReplayConflict || o == ReplayEmpty {
		t.Fatalf("the zero value is %v", o)
	}
	if o.String() == "applied" || o.String() == "" {
		t.Errorf("zero String = %q", o.String())
	}
	for _, c := range []ReplayOutcome{ReplayApplied, ReplayConflict, ReplayEmpty} {
		if c.String() == "" || c.String() == o.String() {
			t.Errorf("outcome %d has String %q", int(c), c.String())
		}
	}
}

// A7: a clean apply.
func TestReplay_CleanApply(t *testing.T) {
	p := newRP(t)
	base, onto, change := p.threeWay(
		[]ent{f("a.txt", lines3), f("b.txt", "b\n")},
		[]ent{f("a.txt", "1\n2\n3\nonto\n")},
		[]ent{f("b.txt", "b\nchange\n"), f("new/c.txt", "c\n")})
	before := p.refs()
	got, out, err := replay(t, p, onto, change, base)
	if err != nil || out != ReplayApplied || len(got) != 40 {
		t.Fatalf("got %q %v %v", got, out, err)
	}
	if parents, err := p.r.CommitParents(context.Background(), got); err != nil || len(parents) != 1 || parents[0] != onto {
		t.Fatalf("parents = %v %v", parents, err)
	}
	if p.show(got, "a.txt") != strings.TrimSpace("1\n2\n3\nonto") || p.show(got, "b.txt") != "b\nchange" || p.show(got, "new/c.txt") != "c" {
		t.Fatal("the tree is not onto plus the change")
	}
	// The same tree as a manual three-way apply.
	manual := p.commit(onto, f("b.txt", "b\nchange\n"), f("new/c.txt", "c\n"))
	if p.tree(got) != p.tree(manual) {
		t.Errorf("tree %s, manual apply gives %s", p.tree(got), p.tree(manual))
	}
	raw := p.raw(got)
	if !strings.Contains(raw, "author Staging Bot <bot@example.invalid> ") || !strings.Contains(raw, "committer Staging Bot <bot@example.invalid> ") {
		t.Errorf("author is not as given:\n%s", raw)
	}
	if p.refs() != before {
		t.Error("a ref moved")
	}
	// The commit is a commit of this repository, and the trusted state holds.
	if err := checkTrusted(context.Background(), p.tr.dir); err != nil {
		t.Errorf("repository no longer trusted: %v", err)
	}
	// A11: the same call twice gives the same outcome and the same tree.
	got2, out2, err := replay(t, p, onto, change, base)
	if err != nil || out2 != ReplayApplied || p.tree(got2) != p.tree(got) {
		t.Fatalf("second call: %q %v %v", got2, out2, err)
	}
}

// A6: conflicts, each one a ReplayConflict with no commit and no ref moved.
func TestReplay_Conflicts(t *testing.T) {
	tests := []struct {
		name               string
		base, onto, change []ent
	}{
		{"both edit one line", []ent{f("f", lines3)}, []ent{f("f", "1\nONTO\n3\n")}, []ent{f("f", "1\nCHANGE\n3\n")}},
		{"delete and modify", []ent{f("f", lines3)}, []ent{rm("f")}, []ent{f("f", "1\nCHANGE\n3\n")}},
		{"modify and delete", []ent{f("f", lines3)}, []ent{f("f", "1\nONTO\n3\n")}, []ent{rm("f")}},
		{"add and add, different", []ent{f("keep", "k\n")}, []ent{f("n", "onto\n")}, []ent{f("n", "change\n")}},
		{"file and directory", []ent{f("keep", "k\n")}, []ent{f("d", "a file\n")}, []ent{f("d/x", "in a dir\n")}},
		{"directory and file", []ent{f("keep", "k\n")}, []ent{f("d/x", "in a dir\n")}, []ent{f("d", "a file\n")}},
		{"mode conflict", []ent{f("f", lines3)}, []ent{{path: "f", mode: "100755", content: lines3}}, []ent{{path: "f", mode: "120000", content: "target"}}},
		{"rename and rename", []ent{f("a", strings.Repeat("some content line\n", 20))},
			[]ent{rm("a"), f("b", strings.Repeat("some content line\n", 20))},
			[]ent{rm("a"), f("c", strings.Repeat("some content line\n", 20))}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newRP(t)
			base, onto, change := p.threeWay(tc.base, tc.onto, tc.change)
			before := p.refs()
			got, out, err := replay(t, p, onto, change, base)
			if err != nil || out != ReplayConflict || got != "" {
				t.Fatalf("got %q %v %v, want a conflict", got, out, err)
			}
			if p.refs() != before {
				t.Error("a ref moved")
			}
			// Twice: no state is left behind.
			got, out, err = replay(t, p, onto, change, base)
			if err != nil || out != ReplayConflict || got != "" {
				t.Fatalf("second call: %q %v %v", got, out, err)
			}
		})
	}
}

// A8: the change is already in onto.
func TestReplay_Empty(t *testing.T) {
	p := newRP(t)
	base := p.commit("", f("f", lines3))
	onto := p.commit(base, f("f", "1\nX\n3\n"))
	change := p.commit(base, f("f", "1\nX\n3\n"))
	got, out, err := replay(t, p, onto, change, base)
	if err != nil || out != ReplayEmpty || got != "" {
		t.Fatalf("got %q %v %v", got, out, err)
	}
	// A change with no edit at all.
	noop := p.commit(base)
	got, out, err = replay(t, p, onto, noop, base)
	if err != nil || out != ReplayEmpty || got != "" {
		t.Fatalf("no-op change: %q %v %v", got, out, err)
	}
}

// A1 and A2: the parent of the change.
func TestReplay_ParentRules(t *testing.T) {
	p := newRP(t)
	base := p.commit("", f("f", lines3))
	other := p.commit(base, f("g", "g\n"))
	onto := p.commit(base, f("h", "h\n"))
	change := p.commit(other, f("f", "1\nCHANGE\n3\n")) // really a child of other
	before := p.refs()
	for name, c := range map[string]struct{ change, want string }{
		"wrong parent":      {change, base},
		"merge commit":      {p.commitMerge(base, other, f("m", "m\n")), base},
		"merge, other side": {p.commitMerge(base, other, f("m", "m\n")), other},
		"root commit":       {p.commit("", f("r", "r\n")), base},
	} {
		got, out, err := replay(t, p, onto, c.change, c.want)
		if !errors.Is(err, ErrReplayParent) || got != "" || out == ReplayApplied || out == ReplayConflict || out == ReplayEmpty {
			t.Errorf("%s: got %q %v %v, want ErrReplayParent", name, got, out, err)
		}
	}
	// It is an error, not a conflict, and the right parent works.
	if _, out, err := replay(t, p, onto, change, other); err != nil || out != ReplayApplied {
		t.Errorf("right parent: %v %v", out, err)
	}
	if p.refs() != before {
		t.Error("a ref moved")
	}
}

// A3: ids.
func TestReplay_RefusesAnythingButFullIds(t *testing.T) {
	p := newRP(t)
	base, onto, change := p.threeWay([]ent{f("f", lines3)}, []ent{f("g", "g\n")}, []ent{f("h", "h\n")})
	if err := p.r.SetRef(context.Background(), StagingRef, onto, ZeroID); err != nil {
		t.Fatal(err)
	}
	tree := p.tree(change)
	blob := p.plumb("x", "hash-object", "-w", "--stdin")
	out := filepath.Join(t.TempDir(), "x")
	bad := []string{
		"", base[:12], strings.ToUpper(base), "HEAD", "factory-staging", "refs/heads/factory-staging", change + "~1", change + "^{tree}", change + "^",
		"--output=" + out, "-p", "-", change + "\n", change + " ", " " + change, "\n" + change, change + "\x00", change[:39], change + "0",
		tree, blob, strings.Repeat("0", 40), strings.Repeat("a", 40), strings.Repeat("f", 64), "main", "@", "@{0}",
	}
	before := p.refs()
	for _, id := range bad {
		for pos, args := range map[string][3]string{
			"onto": {id, change, base}, "change": {onto, id, base}, "want": {onto, change, id},
		} {
			got, o, err := p.r.Replay(context.Background(), args[0], args[1], args[2], replayAuthor, "m")
			if err == nil || got != "" || o == ReplayApplied || o == ReplayConflict || o == ReplayEmpty {
				t.Errorf("Replay %s=%q: got %q %v %v", pos, id, got, o, err)
			}
		}
		if _, err := p.r.IsAncestor(context.Background(), id, change); err == nil {
			t.Errorf("IsAncestor(%q, change) must fail", id)
		}
		if _, err := p.r.IsAncestor(context.Background(), change, id); err == nil {
			t.Errorf("IsAncestor(change, %q) must fail", id)
		}
		if _, err := p.r.CommitParents(context.Background(), id); err == nil {
			t.Errorf("CommitParents(%q) must fail", id)
		}
		if _, err := p.r.TreeOf(context.Background(), id); err == nil {
			t.Errorf("TreeOf(%q) must fail", id)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("an option was read by git: a file was written")
	}
	if p.refs() != before {
		t.Error("a ref moved")
	}
	// An annotated tag object is not a commit either.
	tagOID := p.plumb("object "+change+"\ntype commit\ntag t\ntagger T <t@example.invalid> 1 +0000\n\nm\n", "mktag")
	if _, _, err := p.r.Replay(context.Background(), onto, tagOID, base, replayAuthor, "m"); err == nil {
		t.Error("a tag object was taken as a commit")
	}
}

// A4 and A5: attributes that the change, or onto, brings must not steer the
// merge.
func TestReplay_TreeAttributesDoNotSteerTheMerge(t *testing.T) {
	for _, attr := range []string{
		"* merge=union\n", "* -merge\n", "* binary\n", "* merge=evil\n", "* merge=ours\n", "* -text\n", "* text eol=crlf\n",
		"* filter=evil\n", "* diff=evil\n", "f merge=union\n", "* conflict-marker-size=1\n", "* merge\n", "* merge=binary\n",
	} {
		t.Run("change adds "+strings.TrimSpace(attr), func(t *testing.T) {
			p := newRP(t)
			base, onto, change := p.threeWay(
				[]ent{f("f", lines3)},
				[]ent{f("f", "1\nONTO\n3\n")},
				[]ent{f("f", "1\nCHANGE\n3\n"), f(".gitattributes", attr)})
			got, out, err := replay(t, p, onto, change, base)
			if err != nil || out != ReplayConflict || got != "" {
				t.Fatalf("got %q %v %v, want a conflict", got, out, err)
			}
		})
		t.Run("onto has "+strings.TrimSpace(attr), func(t *testing.T) {
			p := newRP(t)
			base, onto, change := p.threeWay(
				[]ent{f("f", lines3)},
				[]ent{f("f", "1\nONTO\n3\n"), f(".gitattributes", attr)},
				[]ent{f("f", "1\nCHANGE\n3\n")})
			got, out, err := replay(t, p, onto, change, base)
			if err != nil || out != ReplayConflict || got != "" {
				t.Fatalf("got %q %v %v, want a conflict", got, out, err)
			}
		})
		t.Run("base has "+strings.TrimSpace(attr), func(t *testing.T) {
			p := newRP(t)
			base, onto, change := p.threeWay(
				[]ent{f("f", lines3), f(".gitattributes", attr)},
				[]ent{f("f", "1\nONTO\n3\n")},
				[]ent{f("f", "1\nCHANGE\n3\n")})
			got, out, err := replay(t, p, onto, change, base)
			if err != nil || out != ReplayConflict || got != "" {
				t.Fatalf("got %q %v %v, want a conflict", got, out, err)
			}
		})
	}
}

// The repository's own HEAD can name a branch whose tree has attributes. That
// must not steer the merge either.
func TestReplay_HeadAttributesDoNotSteerTheMerge(t *testing.T) {
	p := newRP(t)
	base, onto, change := p.threeWay(
		[]ent{f("f", lines3)},
		[]ent{f("f", "1\nONTO\n3\n")},
		[]ent{f("f", "1\nCHANGE\n3\n")})
	withAttr := p.commit(base, f(".gitattributes", "* merge=union\n"))
	p.plumb("", "update-ref", "refs/heads/master", withAttr)
	p.plumb("", "symbolic-ref", "HEAD", "refs/heads/master")
	got, out, err := replay(t, p, onto, change, base)
	if err != nil || out != ReplayConflict || got != "" {
		t.Fatalf("got %q %v %v, want a conflict", got, out, err)
	}
}

// A5: a custom merge driver is never run. The guard refuses the configuration
// that would name one, before any git command.
func TestReplay_NoMergeDriverRuns(t *testing.T) {
	tr := newTrapRepo(t)
	p := &rp{t: t, tr: tr.testRepo, idx: filepath.Join(t.TempDir(), "idx")}
	base, onto, change := p.threeWay(
		[]ent{f("f", lines3)},
		[]ent{f("f", "1\nONTO\n3\n")},
		[]ent{f("f", "1\nCHANGE\n3\n"), f(".gitattributes", "* merge=evil\n")})
	tr.git("config", "merge.evil.driver", tr.script("driver")+" %O %A %B")
	r := &Repo{dir: tr.dir}
	got, out, err := r.Replay(context.Background(), onto, change, base, replayAuthor, "m")
	if err == nil || !strings.Contains(err.Error(), "merge.evil.driver") || got != "" || out == ReplayApplied {
		t.Fatalf("got %q %v %v, want the guard to refuse", got, out, err)
	}
	if ran := tr.ran(); len(ran) != 0 {
		t.Errorf("a program ran: %v", ran)
	}
}

// A9: the commit message.
func TestReplay_MessageIsStoredSafely(t *testing.T) {
	p := newRP(t)
	base, onto, change := p.threeWay([]ent{f("f", lines3)}, []ent{f("g", "g\n")}, []ent{f("h", "h\n")})
	good := []string{
		"-p", "--amend", "-F /etc/passwd", "plain subject\n\nbody line\n", "Signed-off-by: A <a@example.invalid>\nCo-Authored-By: B <b@example.invalid>",
		"gpgsig -----BEGIN PGP SIGNATURE-----", "subject\ngpgsig x\nparent " + base + "\n", "tree " + p.tree(change) + "\nauthor X <x@y> 1 +0000",
		"\n\n\nleading blank lines", "tab\tinside", strings.Repeat("a", 60<<10),
	}
	for _, m := range good {
		got, out, err := p.r.Replay(context.Background(), onto, change, base, replayAuthor, m)
		if err != nil || out != ReplayApplied {
			t.Fatalf("message %.40q: %q %v %v", m, got, out, err)
		}
		raw := p.raw(got)
		head, body, _ := strings.Cut(raw, "\n\n")
		var names []string
		for _, l := range strings.Split(head, "\n") {
			names = append(names, strings.SplitN(l, " ", 2)[0])
		}
		if strings.Join(names, ",") != "tree,parent,author,committer" {
			t.Errorf("message %.40q: commit headers = %v", m, names)
		}
		if want := strings.TrimRight(m, "\n"); body != want {
			t.Errorf("message %.40q stored as %.80q", m, body)
		}
	}
	for name, m := range map[string]string{
		"empty": "", "blank": " \n\t\n", "NUL": "a\x00b", "escape": "a\x1b[31mred", "bell": "a\ab", "carriage return": "a\rb",
		"too long": strings.Repeat("a", 64<<10+1), "not UTF-8": "a\xffb", "line separator": "a b", "bidi override": "a‮b",
	} {
		got, out, err := p.r.Replay(context.Background(), onto, change, base, replayAuthor, m)
		if err == nil || got != "" || out == ReplayApplied {
			t.Errorf("%s: got %q %v %v, want a refusal", name, got, out, err)
		}
	}
	for name, a := range map[string]Author{
		"no name": {Email: "a@example.invalid"}, "no email": {Name: "A"}, "newline": {Name: "A\nB", Email: "a@example.invalid"},
		"angle": {Name: "A <x>", Email: "a@example.invalid"}, "space in email": {Name: "A", Email: "a b@example.invalid"},
	} {
		if got, _, err := p.r.Replay(context.Background(), onto, change, base, a, "m"); err == nil || got != "" {
			t.Errorf("author %s: got %q %v", name, got, err)
		}
	}
}

// A10: gitlinks and symlinks.
func TestReplay_GitlinksAndSymlinks(t *testing.T) {
	p := newRP(t)
	seed := p.commit("", f("seed", "s\n"))
	t.Run("a change that adds a gitlink is refused", func(t *testing.T) {
		base, onto, _ := p.threeWay([]ent{f("f", lines3)}, []ent{f("g", "g\n")}, nil)
		change := p.commit(base, ent{path: "vendor/sub", mode: "160000", content: seed}, f(".gitmodules", "[submodule \"s\"]\n\tpath = vendor/sub\n\turl = https://example.invalid/x\n"))
		got, out, err := replay(t, p, onto, change, base)
		if err == nil || !errors.Is(err, ErrReplayGitlink) || got != "" || out == ReplayApplied {
			t.Fatalf("got %q %v %v", got, out, err)
		}
	})
	t.Run("a change that changes a gitlink is refused", func(t *testing.T) {
		other := p.commit("", f("o", "o\n"))
		base := p.commit("", ent{path: "vendor/sub", mode: "160000", content: seed})
		onto := p.commit(base, f("g", "g\n"))
		change := p.commit(base, ent{path: "vendor/sub", mode: "160000", content: other})
		if _, _, err := replay(t, p, onto, change, base); !errors.Is(err, ErrReplayGitlink) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a gitlink that the change leaves alone is carried", func(t *testing.T) {
		base := p.commit("", f("f", lines3), ent{path: "vendor/sub", mode: "160000", content: seed})
		onto := p.commit(base, f("g", "g\n"))
		change := p.commit(base, f("f", "1\nCHANGE\n3\n"))
		got, out, err := replay(t, p, onto, change, base)
		if err != nil || out != ReplayApplied {
			t.Fatalf("got %q %v %v", got, out, err)
		}
		if !strings.Contains(p.plumb("", "ls-tree", "-r", got), "160000 commit "+seed+"\tvendor/sub") {
			t.Error("the gitlink was not carried")
		}
	})
	t.Run("a symlink is carried as an entry and never followed", func(t *testing.T) {
		base, onto, _ := p.threeWay([]ent{f("f", lines3)}, []ent{f("g", "g\n")}, nil)
		target := filepath.Join(t.TempDir(), "outside")
		change := p.commit(base, ent{path: "link", mode: "120000", content: target}, ent{path: "d/link2", mode: "120000", content: "../f"})
		got, out, err := replay(t, p, onto, change, base)
		if err != nil || out != ReplayApplied {
			t.Fatalf("got %q %v %v", got, out, err)
		}
		ls := p.plumb("", "ls-tree", "-r", got)
		if !strings.Contains(ls, "120000 blob") || p.show(got, "link") != target {
			t.Errorf("symlink not carried: %s", ls)
		}
		if _, err := os.Lstat(target); err == nil {
			t.Error("something was made at the link target")
		}
	})
}

// A12: a tampered repository is refused before anything runs. See also
// TestEveryExportedGitFunctionChecksTheGuardFirst, which covers these methods.
func TestReplay_TamperedRepositoryIsRefused(t *testing.T) {
	p := newRP(t)
	base, onto, change := p.threeWay([]ent{f("f", lines3)}, []ent{f("g", "g\n")}, []ent{f("h", "h\n")})
	cfg := filepath.Join(p.tr.dir, "config")
	b, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, append(b, []byte("[attr]\n\ttree = HEAD\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Repo{dir: p.tr.dir}
	ctx := context.Background()
	if _, _, err := r.Replay(ctx, onto, change, base, replayAuthor, "m"); err == nil || !strings.Contains(err.Error(), "attr.tree") {
		t.Errorf("Replay: %v", err)
	}
	if _, err := r.IsAncestor(ctx, base, change); err == nil || !strings.Contains(err.Error(), "attr.tree") {
		t.Errorf("IsAncestor: %v", err)
	}
	if _, err := r.CommitParents(ctx, change); err == nil || !strings.Contains(err.Error(), "attr.tree") {
		t.Errorf("CommitParents: %v", err)
	}
	if _, err := r.TreeOf(ctx, change); err == nil || !strings.Contains(err.Error(), "attr.tree") {
		t.Errorf("TreeOf: %v", err)
	}
}

func TestIsAncestorCommitParentsTreeOf(t *testing.T) {
	p := newRP(t)
	ctx := context.Background()
	a := p.commit("", f("f", "a\n"))
	b := p.commit(a, f("f", "b\n"))
	c := p.commit(a, f("f", "c\n"))
	m := p.commitMerge(b, c, f("f", "m\n"))
	for _, tc := range []struct {
		x, y string
		want bool
	}{{a, b, true}, {a, m, true}, {b, m, true}, {b, c, false}, {b, a, false}, {a, a, true}, {m, a, false}} {
		got, err := p.r.IsAncestor(ctx, tc.x, tc.y)
		if err != nil || got != tc.want {
			t.Errorf("IsAncestor(%.6s, %.6s) = %v %v, want %v", tc.x, tc.y, got, err, tc.want)
		}
	}
	for commit, want := range map[string][]string{a: nil, b: {a}, m: {b, c}} {
		got, err := p.r.CommitParents(ctx, commit)
		if err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("CommitParents(%.6s) = %v %v, want %v", commit, got, err, want)
		}
	}
	for _, commit := range []string{a, b, m} {
		got, err := p.r.TreeOf(ctx, commit)
		if err != nil || got != p.tree(commit) {
			t.Errorf("TreeOf(%.6s) = %q %v", commit, got, err)
		}
	}
}

// rawCommit writes a commit object with exactly these header lines, in this
// order, as git hash-object --literally does. It is how a hostile or buggy
// writer could make a commit that git reads differently from a naive parser.
func (p *rp) rawCommit(headers ...string) string {
	p.t.Helper()
	body := strings.Join(headers, "\n") + "\n\nmessage\n"
	return p.plumb(body, "hash-object", "-t", "commit", "-w", "--literally", "--stdin")
}

const (
	hdrAuthor    = "author A <a@example.invalid> 1 +0000"
	hdrCommitter = "committer A <a@example.invalid> 1 +0000"
)

func TestCommitHeadersAreReadInGitsOwnOrder(t *testing.T) {
	p := newRP(t)
	ctx := context.Background()
	base := p.commit("", f("f", lines3))
	other := p.commit("", f("o", "o\n"))
	tree := p.tree(base)
	onto := p.commit(base, f("g", "g\n"))

	// What git itself says about the crafted commit: no parent.
	crafted := p.rawCommit("tree "+tree, hdrAuthor, hdrCommitter, "parent "+base)
	if got := p.plumb("", "rev-list", "--parents", "-n1", crafted); got != crafted {
		t.Fatalf("setup: git says %q", got)
	}
	bad := map[string]string{
		"parent after committer":  crafted,
		"parent after author":     p.rawCommit("tree "+tree, hdrAuthor, "parent "+base, hdrCommitter),
		"duplicate tree":          p.rawCommit("tree "+tree, "tree "+tree, hdrAuthor, hdrCommitter),
		"duplicate author":        p.rawCommit("tree "+tree, hdrAuthor, hdrAuthor, hdrCommitter),
		"duplicate committer":     p.rawCommit("tree "+tree, hdrAuthor, hdrCommitter, hdrCommitter),
		"tree not first":          p.rawCommit(hdrAuthor, "tree "+tree, hdrCommitter),
		"unknown header early":    p.rawCommit("tree "+tree, "x-evil foo", hdrAuthor, hdrCommitter),
		"unknown header late":     p.rawCommit("tree "+tree, hdrAuthor, hdrCommitter, "x-evil foo"),
		"no author":               p.rawCommit("tree "+tree, hdrCommitter),
		"no committer":            p.rawCommit("tree "+tree, hdrAuthor),
		"committer before author": p.rawCommit("tree "+tree, hdrCommitter, hdrAuthor),
		"parent is not an id":     p.rawCommit("tree "+tree, "parent HEAD", hdrAuthor, hdrCommitter),
	}
	for name, c := range bad {
		if got, err := p.r.CommitParents(ctx, c); err == nil {
			t.Errorf("%s: CommitParents = %v, want an error", name, got)
		}
		if got, err := p.r.TreeOf(ctx, c); err == nil {
			t.Errorf("%s: TreeOf = %q, want an error", name, got)
		}
	}
	// As the change of a replay the crafted commit is refused, not replayed
	// as a child of base.
	if got, out, err := replay(t, p, onto, crafted, base); err == nil || got != "" || out != ReplayUnknown {
		t.Errorf("Replay of the crafted commit: %q %v %v", got, out, err)
	}

	// Normal, root and merge commits read as before.
	if ps, err := p.r.CommitParents(ctx, onto); err != nil || len(ps) != 1 || ps[0] != base {
		t.Errorf("normal: %v %v", ps, err)
	}
	if ps, err := p.r.CommitParents(ctx, base); err != nil || len(ps) != 0 {
		t.Errorf("root: %v %v", ps, err)
	}
	m := p.commitMerge(base, other, f("m", "m\n"))
	if ps, err := p.r.CommitParents(ctx, m); err != nil || len(ps) != 2 || ps[0] != base || ps[1] != other {
		t.Errorf("merge: %v %v", ps, err)
	}

	// Headers after the committer that git itself writes: a signed commit.
	// They do not change what git calls a parent, so a signed onto or a signed
	// commit can be read. A change with one is refused: the factory's own
	// import never writes one.
	sig := "gpgsig -----BEGIN PGP SIGNATURE-----\n \n abc\n -----END PGP SIGNATURE-----"
	signed := p.rawCommit("tree "+tree, "parent "+other, hdrAuthor, hdrCommitter, sig)
	if ps, err := p.r.CommitParents(ctx, signed); err != nil || len(ps) != 1 || ps[0] != other {
		t.Errorf("signed: %v %v", ps, err)
	}
	if tr, err := p.r.TreeOf(ctx, signed); err != nil || tr != tree {
		t.Errorf("signed TreeOf: %q %v", tr, err)
	}
	for _, extra := range []string{sig, "encoding ISO-8859-1", "mergetag object " + base + "\n type commit"} {
		c := p.rawCommit("tree "+p.tree(onto), "parent "+base, hdrAuthor, hdrCommitter, extra)
		if got, out, err := replay(t, p, onto, c, base); err == nil || got != "" || out != ReplayUnknown {
			t.Errorf("Replay of a change with %.12q: %q %v %v", extra, got, out, err)
		}
	}
	// A signed onto is fine as the base to replay on.
	signedOnto := p.rawCommit("tree "+p.tree(onto), "parent "+base, hdrAuthor, hdrCommitter, sig)
	change := p.commit(base, f("h", "h\n"))
	if _, out, err := replay(t, p, signedOnto, change, base); err != nil || out != ReplayApplied {
		t.Errorf("signed onto: %v %v", out, err)
	}
}
