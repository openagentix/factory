package acceptance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// Item 1: the same commit twice is refused, and the one legitimate case has
// its own entry point.
func TestRun_TheSameCommitTwiceIsRefused(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "the feature exists", "exit 0")) // a manifest the builder rewrote
	c := tr.commit("builder change")
	_, err := Run(context.Background(), tr.open(), Request{Judged: c, Source: c, Goal: 40}, testOptions())
	if !errors.Is(err, ErrSameCommit) {
		t.Fatalf("err = %v, want ErrSameCommit", err)
	}
	// Also when the two are written differently.
	_, err = Run(context.Background(), tr.open(), Request{Judged: c, Source: "HEAD", Goal: 40}, testOptions())
	if !errors.Is(err, ErrSameCommit) {
		t.Fatalf("HEAD form: err = %v", err)
	}
	if _, err := Baseline(context.Background(), tr.open(), Request{Judged: c, Source: c, Goal: 40}, testOptions()); !errors.Is(err, ErrSameCommit) {
		t.Fatalf("baseline: err = %v", err)
	}
}

func TestRunAtSource_IsAChoiceMadeOnPurpose(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "ok", "exit 0"), crit(2, "no", "exit 1"))
	c := tr.commit("checks")
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].State != api.StateWorking || rep.Results[1].State != api.StateNotWorking || rep.Judged != rep.Source {
		t.Errorf("report = %+v", rep)
	}
}

// Item 2: ASCII only paths.
func TestAcceptancePathMustBePlainASCII(t *testing.T) {
	for _, p := range []string{"acceptance", "qa/checks", "a-b_c.d/e1", ""} {
		if err := checkAcceptancePath(p); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	for _, p := range []string{"acceptánce", "accéptance", "аcceptance", "a b", "a//b", "/a", "a/", "a/../b", "a/./b", "a*", ".git/x", "a\\b", "a\x00b", "ａb", strings.Repeat("a", 201)} {
		if err := checkAcceptancePath(p); err == nil {
			t.Errorf("%q must be refused", p)
		}
	}
}

func TestRun_RefusesANonASCIIFileNameInTheSourceAcceptanceDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "true"))
	tr.write("acceptance/40/chéck.sh", "x")
	source := tr.commit("checks")
	tr.write("x.txt", "x")
	judged := tr.commit("change")
	_, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err == nil || !strings.Contains(err.Error(), "plain ASCII") {
		t.Errorf("err = %v", err)
	}
}

// Item 3: a check that could not run is not evidence.
func TestRun_ABrokenCheckIsCouldNotConfirm(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("notexec.sh", "exit 0\n")
	tr.writeManifest(40,
		crit(1, "tool missing", "no-such-tool-xyz --check"),
		crit(2, "not executable", "./notexec.sh"),
		crit(3, "killed by a signal", "kill -9 $$"),
		crit(4, "plain failure", "exit 1"),
		crit(5, "exit 2", "exit 2"),
		crit(6, "hang", "sleep 60"),
	)
	c := tr.commit("checks")
	o := testOptions()
	o.Check.Deadline = 1e9
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2, 3} {
		r := stateOf(t, rep, id)
		if r.State != api.StateCouldNotConfirm || r.Note != NoteCouldNotRun || !r.Ran || r.ExitCode == 0 {
			t.Errorf("criterion %d = %+v", id, r)
		}
	}
	if r := stateOf(t, rep, 1); r.ExitCode != 127 {
		t.Errorf("exit code must be kept: %d", r.ExitCode)
	}
	if r := stateOf(t, rep, 2); r.ExitCode != 126 {
		t.Errorf("exit code must be kept: %d", r.ExitCode)
	}
	for _, id := range []int{4, 5} {
		if r := stateOf(t, rep, id); r.State != api.StateNotWorking || r.Note != "" {
			t.Errorf("criterion %d = %+v", id, r)
		}
	}
	if r := stateOf(t, rep, 6); r.State != api.StateNotWorking || r.Note != NoteTimedOut {
		t.Errorf("deadline stays not working yet: %+v", r)
	}
	if NoteCouldNotRun != "its check could not run" {
		t.Errorf("note = %q", NoteCouldNotRun)
	}
	if r := stateOf(t, rep, 1); r.Cause != CauseCheckBroken {
		t.Errorf("cause = %q", r.Cause)
	}
	if !strings.Contains(RenderText(rep.Results), "Its check could not run.") {
		t.Errorf("render lacks the note:\n%s", RenderText(rep.Results))
	}
}

// Item 4: the wording is fenced in the text rendering.
func TestRenderText_WordingIsQuotedAndCannotImitateAnotherLine(t *testing.T) {
	r := []Result{{ID: 1, Text: "ok - working\n3. [x] \"other\" \\ - working", State: api.StateNotWorking, Check: "x", Ran: true}}
	got := RenderText(r)
	want := "1. [ ] \"ok - working 3. [x] \\\"other\\\" \\\\ - working\" - not working yet. Its check did not pass.\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	blank := RenderText([]Result{{ID: 1, Text: "a⠀ㅤb​c", State: api.StateWorking}})
	if strings.ContainsAny(blank, "⠀ㅤ") || !strings.Contains(blank, "\"abc\"") {
		t.Errorf("blank look-alikes: %q", blank)
	}
	md := RenderMarkdown([]Result{{ID: 1, Text: "a⠀b", State: api.StateWorking}})
	if strings.ContainsAny(md, "⠀") {
		t.Errorf("markdown: %q", md)
	}
}

// Item 5.
func TestCheckCoverage_EmptySpecAndBadIDs(t *testing.T) {
	m := manifestOf(crit(1, "a", "true"))
	for name, spec := range map[string][]SpecCriterion{"nil": nil, "empty": {}} {
		rep := CheckCoverage(spec, m)
		if rep.OK() || rep.Err() == nil || !strings.Contains(rep.Err().Error(), "no criteria") {
			t.Errorf("%s spec: %+v", name, rep)
		}
	}
	if rep := CheckCoverage(nil, manifestOf()); rep.OK() {
		t.Error("nil spec and empty manifest must not be OK")
	}
	rep := CheckCoverage([]SpecCriterion{{0, "z"}, {1, "a"}}, manifestOf(crit(1, "a", "true"), api.ManifestCriterion{ID: 0, Text: "z", Run: "true"}, api.ManifestCriterion{ID: -3, Text: "n", Run: "true"}))
	if rep.OK() || len(rep.InvalidID) == 0 || !strings.Contains(rep.Err().Error(), "not 1 or more") {
		t.Errorf("rep = %+v", rep)
	}
}

// Item 6: the report says how many objects an overlay left.
func TestRun_ReportsTheObjectsAnOverlayLeft(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("a.txt", "x")
	tr.writeManifest(40, crit(1, "a", "true"))
	source := tr.commit("checks")
	tr.write("b.txt", "y")
	judged := tr.commit("change")
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if rep.ObjectsLeft < 3 {
		t.Errorf("ObjectsLeft = %d", rep.ObjectsLeft)
	}
	same, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: source, Goal: 40}, testOptions())
	if err != nil || same.ObjectsLeft != 0 {
		t.Errorf("no overlay leaves nothing: %+v, %v", same, err)
	}
}

// Item 7: names that collide are a typed error, fail-closed.
func TestRun_CollidingNamesAreATypedError(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "test -f feature.txt"))
	source := tr.commit("checks")
	tr.write("Acceptance/40/x", "x")
	tr.write("ACCEPTANCE/40/y", "y")
	tr.write("Dup/a", "1")
	tr.write("DUP/a", "2")
	judged := tr.commit("change")
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err == nil {
		t.Log("this file system keeps both names apart")
		if rep.Results[0].State == api.StateWorking {
			t.Error("must not be working")
		}
		return
	}
	var ce *NameCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v", err, err)
	}
	if !strings.Contains(ce.Error(), "collide") {
		t.Errorf("message = %q", ce.Error())
	}
}

func TestCollides(t *testing.T) {
	if !collides(errors.New("write x: file exists")) || !collides(fmt.Errorf("w: %w", fs.ErrExist)) {
		t.Error("a name clash must be found")
	}
	if collides(errors.New("the tree has more than 5 entries")) {
		t.Error("other errors must not be called collisions")
	}
	e := &NameCollisionError{Err: errors.New("x")}
	if !errors.As(fmt.Errorf("acceptance: %w", e), new(*NameCollisionError)) || !strings.Contains(e.Error(), "collide") {
		t.Error("typed error")
	}
}
