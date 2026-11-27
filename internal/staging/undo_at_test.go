package staging

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// threeChanges builds three changes: ticket 2 waits for ticket 1, ticket 3 stands
// alone. The last state is published.
func threeChanges(t *testing.T, edit ...func(*fx, *Config)) (*fx, []Entry, State) {
	t.Helper()
	f := newFx(t, edit...)
	var es []Entry
	var st State
	for i, deps := range [][]int{nil, {1}, nil} {
		var e Entry
		e, st = f.addFile(i+1, string(rune('a'+i))+".txt", deps...)
		es = append(es, e)
	}
	return f, es, st
}

// The preview names the change, what leaves with it, in the order they stand,
// and the ticket of each. It changes nothing.
func TestUndoPreviewListsWhatLeaves(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t)
	before := f.snap()
	p, err := f.s.UndoPreview(f.ctx, es[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Seq != st.Seq || p.Commit != st.Commit || p.Change != es[0].ID || !reflect.DeepEqual(p.Also, []string{es[1].ID}) {
		t.Fatalf("%+v", p)
	}
	if p.Tickets[es[0].ID] != 1 || p.Tickets[es[1].ID] != 2 || len(p.Tickets) != 2 {
		t.Fatalf("tickets %v", p.Tickets)
	}
	if !reflect.DeepEqual(before, f.snap()) {
		t.Error("the preview changed staging")
	}
	// A change nothing depends on leaves alone, and the list is empty, not nil.
	p, err = f.s.UndoPreview(f.ctx, es[2].ID)
	if err != nil || p.Also == nil || len(p.Also) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestUndoPreviewRefusals(t *testing.T) {
	t.Parallel()
	f, es, _ := threeChanges(t)
	if _, err := f.s.UndoPreview(f.ctx, "nope"); !errors.Is(err, ErrUnknownEntry) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := f.s.UndoPreview(f.ctx, ""); !errors.Is(err, ErrUnknownEntry) {
		t.Errorf("blank: %v", err)
	}
	// A state that was recorded and not pushed is not the holding area.
	g := newFx(t)
	e, _ := g.addFile(1, "a.txt")
	g.host.failPush = 1
	e2 := g.entry(2, g.commit(g.def, put("b.txt", "b\n")), g.def)
	if _, err := g.s.Add(g.ctx, e2, g.decision(e2)); err == nil {
		t.Fatal("setup: the push should fail")
	}
	if _, err := g.s.UndoPreview(g.ctx, e.ID); !errors.Is(err, ErrNotPublished) {
		t.Errorf("unpublished: %v", err)
	}
	_ = es
}

func TestUndoAtTakesTheChangeAndWhatDependsOnIt(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t)
	st2, removed, err := f.s.UndoAt(f.ctx, st.Seq, st.Commit, es[0].ID, []string{es[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{es[0].ID, es[1].ID}) || !st2.Published || st2.Seq != st.Seq+1 {
		t.Fatalf("%+v %v", st2, removed)
	}
	if !reflect.DeepEqual(ids(st2.Applied), []string{es[2].ID}) {
		t.Errorf("applied %v", ids(st2.Applied))
	}
	for _, r := range st2.Removed {
		if r.Reason != RemovedUndone {
			t.Errorf("removal %+v", r)
		}
	}
}

// Attack 22: the removal list must equal the preview, and the state must be
// the one the person saw. Every refusal changes nothing.
func TestAttack22_UndoAtRefusesAnyMismatch(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t)
	a, b, c := es[0].ID, es[1].ID, es[2].ID
	cases := []struct {
		name   string
		seq    int64
		commit string
		id     string
		also   []string
		want   error
	}{
		{"missing", st.Seq, st.Commit, a, nil, ErrStale},
		{"extra", st.Seq, st.Commit, a, []string{b, c}, ErrStale},
		{"reordered", st.Seq, st.Commit, a, []string{c, b}, ErrStale},
		{"duplicate", st.Seq, st.Commit, a, []string{b, b}, ErrStale},
		{"the change itself", st.Seq, st.Commit, a, []string{a, b}, ErrStale},
		{"old number", st.Seq - 1, st.Commit, a, []string{b}, ErrStale},
		{"number from the future", st.Seq + 1, st.Commit, a, []string{b}, ErrStale},
		{"other commit", st.Seq, strings.Repeat("a", 40), a, []string{b}, ErrStale},
		{"blank commit", st.Seq, "", a, []string{b}, ErrStale},
		{"zero number", 0, st.Commit, a, []string{b}, ErrStale},
		{"unknown id", st.Seq, st.Commit, "nope", nil, ErrUnknownEntry},
	}
	before := f.snap()
	for _, k := range cases {
		if _, _, err := f.s.UndoAt(f.ctx, k.seq, k.commit, k.id, k.also); !errors.Is(err, k.want) {
			t.Errorf("%s: %v", k.name, err)
		}
	}
	if !reflect.DeepEqual(before, f.snap()) {
		t.Error("a refused undo changed staging")
	}
	if latest, _, _ := f.store.Latest(f.ctx); latest.Seq != st.Seq {
		t.Errorf("a state was recorded: %d", latest.Seq)
	}
}

// A dependent that passed between the preview and the undo makes the list
// stale.
func TestAttack22_NewDependentAfterPreviewIsStale(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t)
	p, err := f.s.UndoPreview(f.ctx, es[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, st2 := f.addFile(4, "d.txt", 1) // ticket 4 waits for ticket 1
	if _, _, err := f.s.UndoAt(f.ctx, st.Seq, st.Commit, es[0].ID, p.Also); !errors.Is(err, ErrStale) {
		t.Errorf("old state: %v", err)
	}
	if _, _, err := f.s.UndoAt(f.ctx, st2.Seq, st2.Commit, es[0].ID, p.Also); !errors.Is(err, ErrStale) {
		t.Errorf("old list: %v", err)
	}
}

// Attack 23: an accepted change, and a change that is not applied.
func TestAttack23_UndoAtAfterAcceptAndNotApplied(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t)
	if res, err := f.s.Accept(f.ctx, st.Seq, st.Commit); err != nil || res.Outcome != OutcomeAccepted {
		t.Fatalf("accept: %+v %v", res, err)
	}
	if _, _, err := f.s.UndoAt(f.ctx, st.Seq, st.Commit, es[2].ID, nil); err == nil ||
		!(errors.Is(err, ErrNotBuilt) || errors.Is(err, ErrStale) || errors.Is(err, ErrMirrorBehind)) {
		t.Errorf("after accept: %v", err)
	}
	if _, err := f.s.UndoPreview(f.ctx, es[2].ID); err == nil {
		t.Error("a preview was made for an accepted change")
	}
}

// A change taken out by a rebuild is not applied, so it cannot be undone.
func TestUndoAtOfAChangeThatIsNotApplied(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	_, st := f.addFile(1, "a.txt")
	bad := f.entry(2, f.commit(f.def, put("bad.flag", "x\n")), f.def)
	st2, err := f.s.Add(f.ctx, bad, f.decision(bad))
	if err != nil || len(st2.Removed) == 0 {
		t.Fatalf("setup: %+v %v", st2, err)
	}
	if _, _, err := f.s.UndoAt(f.ctx, st2.Seq, st2.Commit, bad.ID, nil); !errors.Is(err, ErrUnknownEntry) && !errors.Is(err, ErrStale) {
		t.Errorf("%v", err)
	}
	_ = st
}

// While an Accept is pending, nothing is undone.
func TestUndoAtWhilePending(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t, devMode)
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); err != nil {
		t.Fatal(err)
	}
	before := f.snap()
	if _, _, err := f.s.UndoAt(f.ctx, st.Seq, st.Commit, es[2].ID, nil); !errors.Is(err, ErrFrozen) {
		t.Errorf("UndoAt: %v", err)
	}
	if _, err := f.s.UndoPreview(f.ctx, es[2].ID); !errors.Is(err, ErrFrozen) {
		t.Errorf("UndoPreview: %v", err)
	}
	if !reflect.DeepEqual(before, f.snap()) {
		t.Error("staging changed")
	}
}

// Undo and Add race: exactly one undo succeeds, whatever the order.
func TestUndoAtOnlyOnceUnderRace(t *testing.T) {
	t.Parallel()
	f, es, st := threeChanges(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := f.s.UndoAt(f.ctx, st.Seq, st.Commit, es[2].ID, nil); err == nil {
				mu.Lock()
				okN++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okN != 1 {
		t.Errorf("%d undos succeeded", okN)
	}
}

// CancelAcceptAt also binds the commit.
func TestCancelAcceptAtBindsTheCommit(t *testing.T) {
	t.Parallel()
	f, _, st := threeChanges(t, devMode)
	if err := f.s.CancelAcceptAt(f.ctx, st.Seq, st.Commit); !errors.Is(err, ErrStale) {
		t.Errorf("nothing pending: %v", err)
	}
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]string{"other": strings.Repeat("b", 40), "blank": "", "short": st.Commit[:10]} {
		if err := f.s.CancelAcceptAt(f.ctx, st.Seq, c); !errors.Is(err, ErrStale) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := f.s.CancelAcceptAt(f.ctx, st.Seq+1, st.Commit); !errors.Is(err, ErrStale) {
		t.Errorf("other number: %v", err)
	}
	if _, ok, _ := f.store.GetPending(f.ctx); !ok {
		t.Fatal("a refused cancel ended the accept")
	}
	if err := f.s.CancelAcceptAt(f.ctx, st.Seq, st.Commit); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.store.GetPending(f.ctx); ok {
		t.Error("still pending")
	}
}
