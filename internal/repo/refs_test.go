package repo

import (
	"context"
	"strings"
	"testing"
)

func TestSetRef(t *testing.T) {
	tr := newTrapRepo(t)
	r, err := Open(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, ok := range []string{"refs/heads/factory/run-1", "refs/heads/factory/a/b.c_d", "refs/heads/factory-staging"} {
		if err := r.SetRef(ctx, ok, tr.head); err != nil {
			t.Errorf("SetRef(%q): %v", ok, err)
			continue
		}
		if got, _ := r.Resolve(ctx, ok); got != tr.head {
			t.Errorf("%s = %q, want %q", ok, got, tr.head)
		}
	}
	for _, bad := range []string{
		"refs/heads/main", "refs/heads/master", "refs/heads/factory", "refs/heads/factory/", "refs/heads/factory-staging2",
		"refs/heads/factory-staging/x", "refs/heads/factoryx/a", "refs/tags/factory/a", "refs/heads/factory/../main",
		"refs/heads/factory/a..b", "refs/heads/factory//a", "refs/heads/factory/a.lock", "refs/heads/factory/.hidden",
		"refs/heads/factory/a b", "refs/heads/factory/a\nb", "refs/heads/factory/-x", "HEAD", "", "refs/mirror/default",
		"refs/replace/" + tr.head, "refs/heads/factory/a@{1}", "refs/heads/factory/a~1", "refs/heads/factory/a:b", "refs/heads/factory/a/",
	} {
		if err := r.SetRef(ctx, bad, tr.head); err == nil {
			t.Errorf("SetRef(%q) must be refused", bad)
		}
	}
	if err := r.SetRef(ctx, "refs/heads/factory/x", "no-such-commit"); err == nil {
		t.Error("a missing commit must be refused")
	}
	// A tree id is not a commit.
	tree := tr.git("rev-parse", tr.head+"^{tree}")
	if err := r.SetRef(ctx, "refs/heads/factory/x", tree); err == nil {
		t.Error("a tree must be refused")
	}
	if err := checkTrusted(ctx, tr.dir); err != nil {
		t.Errorf("the repository must stay trusted: %v", err)
	}
}

func TestSetDefaultMirror(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	ctx := context.Background()
	if err := r.SetDefaultMirror(ctx, tr.base); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Resolve(ctx, MirrorRef); err != nil || got != tr.base {
		t.Errorf("mirror = %q, %v", got, err)
	}
	if err := r.SetDefaultMirror(ctx, "-x"); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Errorf("err = %v", err)
	}
	// The mirror is not a name SetRef will take.
	if err := r.SetRef(ctx, MirrorRef, tr.head); err == nil {
		t.Error("SetRef must refuse the mirror ref")
	}
}
