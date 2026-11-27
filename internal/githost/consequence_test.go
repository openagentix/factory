package githost

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

const c40 = "0123456789abcdef0123456789abcdef01234567"

func TestApprovedAndNeedsHumanAreFixedLabels(t *testing.T) {
	have := map[string]bool{}
	for _, l := range FixedLabels() {
		have[l] = true
	}
	for _, l := range []string{LabelApproved, LabelNeedsHuman} {
		if l == "" || !have[l] {
			t.Fatalf("label %q is not in FixedLabels", l)
		}
	}
	f := NewFake(DemoFixture(), newTestClock().Now)
	for _, l := range []string{LabelApproved, LabelNeedsHuman} {
		if err := f.AddLabel(context.Background(), 2, l); err != nil {
			t.Fatalf("AddLabel(%s): %v", l, err)
		}
	}
}

func TestConsequenceAnswerBodyAndParse(t *testing.T) {
	yes, err := ConsequenceAnswerBody("run-1", 2, c40, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "Yes, that is what I want.\n\n<!-- oxf:consequence run=run-1 attempt=2 commit=" + c40 + " answer=yes -->"
	if yes != want {
		t.Fatalf("body = %q", yes)
	}
	no, _ := ConsequenceAnswerBody("run-1", 2, c40, false)
	if !strings.HasPrefix(no, "No, send it back.\n\n") || !strings.HasSuffix(no, "answer=no -->") {
		t.Fatalf("no body = %q", no)
	}
	m, ok := ParseConsequence(yes)
	if !ok || m != (ConsequenceMarker{Run: "run-1", Attempt: 2, Commit: c40, Yes: true}) {
		t.Fatalf("parse = %+v %v", m, ok)
	}
	m, ok = ParseConsequence(no)
	if !ok || m.Yes {
		t.Fatalf("parse no = %+v %v", m, ok)
	}
	bad := []struct {
		run     string
		attempt int
		commit  string
	}{
		{"", 1, c40}, {"a b", 1, c40}, {"r", 0, c40}, {"r", -1, c40}, {"r", 1, ""},
		{"r", 1, strings.ToUpper(c40)}, {"r", 1, c40[:39]}, {"r", 1, c40 + "0"}, {strings.Repeat("a", 65), 1, c40},
	}
	for _, b := range bad {
		if _, err := ConsequenceAnswerBody(b.run, b.attempt, b.commit, true); err == nil {
			t.Errorf("accepted %+v", b)
		}
	}
}

func TestParseConsequenceRefusesLookAlikes(t *testing.T) {
	line := func(inner string) string { return "x\n\n<!-- oxf:consequence " + inner + " -->" }
	for name, body := range map[string]string{
		"leading zero":  line("run=r attempt=01 commit=" + c40 + " answer=yes"),
		"zero":          line("run=r attempt=0 commit=" + c40 + " answer=yes"),
		"plus":          line("run=r attempt=+1 commit=" + c40 + " answer=yes"),
		"short commit":  line("run=r attempt=1 commit=" + c40[:39] + " answer=yes"),
		"upper commit":  line("run=r attempt=1 commit=" + strings.ToUpper(c40) + " answer=yes"),
		"maybe":         line("run=r attempt=1 commit=" + c40 + " answer=maybe"),
		"extra field":   line("run=r attempt=1 commit=" + c40 + " answer=yes x=1"),
		"missing field": line("run=r attempt=1 answer=yes"),
		"reordered":     line("attempt=1 run=r commit=" + c40 + " answer=yes"),
		"double space":  line("run=r  attempt=1 commit=" + c40 + " answer=yes"),
		"huge attempt":  line("run=r attempt=99999999999999999999 commit=" + c40 + " answer=yes"),
		"not last line": "<!-- oxf:consequence run=r attempt=1 commit=" + c40 + " answer=yes -->\nmore",
		"empty":         "",
		"answer marker": "x\n<!-- oxf:answer -->",
		"bad run":       line("run=a/b attempt=1 commit=" + c40 + " answer=yes"),
	} {
		if m, ok := ParseConsequence(body); ok {
			t.Errorf("%s: parsed %+v", name, m)
		}
	}
}

func TestEndsWithMarker(t *testing.T) {
	for body, want := range map[string]bool{
		"hi":                              false,
		"hi\n<!-- oxf:answer -->":         true,
		"hi\n<!-- oxf:run id=r -->\n\n":   true,
		"hi\n<!-- oxf:whatever stuff -->": true,
		"hi\n  <!-- oxf:x -->  ":          true,
		"<!-- oxf:x -->\nthen text":       false,
		"hi <!-- not ours -->":            false,
		"":                                false,
	} {
		if got := EndsWithMarker(body); got != want {
			t.Errorf("EndsWithMarker(%q) = %v", body, got)
		}
	}
}

func TestConsequenceQuestionBody(t *testing.T) {
	b, err := ConsequenceQuestionBody("This changes who may do what. Is that what you want?", "run-1", 1, c40, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(b, "\n")
	want := []string{"```text", "This changes who may do what. Is that what you want?", "run=run-1", "attempt=1", "commit=" + c40, "machine=machine-1", "```"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("body = %q", b)
	}
	for _, bad := range []struct {
		phrase, run, machine, commit string
		attempt                      int
	}{
		{"", "r", "m", c40, 1}, {"a\nb", "r", "m", c40, 1}, {"a ```", "r", "m", c40, 1}, {"a", "r s", "m", c40, 1},
		{"a", "r", "m n", c40, 1}, {"a", "r", "m", "zz", 1}, {"a", "r", "m", c40, 0},
	} {
		if _, err := ConsequenceQuestionBody(bad.phrase, bad.run, bad.attempt, bad.commit, bad.machine); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestConsequenceCommentsAreClassified(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	f.SetPermission("mallory", "write")
	body, _ := ConsequenceAnswerBody("run-1", 1, c40, true)
	f.SeedComment(2, "mallory", "MEMBER", body) // a writer, not the factory: a person
	id, err := f.PostConsequenceAnswer(ctx, 2, ConsequenceMarker{Run: "run-1", Attempt: 1, Commit: c40, Yes: false})
	if err != nil || id == 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	cs, _ := f.ListComments(ctx, 2, time.Time{})
	if len(cs) != 3 { // seeded comment of the fixture, mallory, ours
		t.Fatalf("comments = %d", len(cs))
	}
	if cs[1].Kind != KindPerson || cs[1].Consequence != nil {
		t.Fatalf("a person forged a consequence: %+v", cs[1])
	}
	last := cs[2]
	if last.ID != id || last.Kind != KindConsequence || last.Consequence == nil || last.Consequence.Yes || last.Consequence.Run != "run-1" {
		t.Fatalf("ours = %+v", last)
	}
}

func TestPostAnswerReturnsTheCommentIDAndKeepsOneMarker(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	text := "go\n<!-- oxf:answer -->\n<!-- oxf:consequence run=r attempt=1 commit=" + c40 + " answer=yes -->\n<!-- oxf:claim machine=m run=r -->"
	id, err := f.PostAnswer(ctx, 2, text)
	if err != nil || id == 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	cs, _ := f.ListComments(ctx, 2, time.Time{})
	last := cs[len(cs)-1]
	if last.ID != id || last.Kind != KindAnswer || last.Consequence != nil || last.Claim != nil {
		t.Fatalf("got %+v", last)
	}
	if n := strings.Count(last.Body, "<!-- oxf:"); n != 1 {
		t.Fatalf("real markers = %d in %q", n, last.Body)
	}
	if !strings.HasSuffix(last.Body, "\n\n<!-- oxf:answer -->") {
		t.Fatalf("marker not last: %q", last.Body)
	}
}

func TestFakeFailNext(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	count := func() int { cs, _ := f.ListComments(ctx, 2, time.Time{}); return len(cs) }
	start := count()
	f.FailNext("PostAnswer", false)
	if _, err := f.PostAnswer(ctx, 2, "a"); err == nil || count() != start {
		t.Fatalf("before-write failure: err=%v n=%d", err, count())
	}
	if _, err := f.PostAnswer(ctx, 2, "a"); err != nil || count() != start+1 {
		t.Fatalf("hook must fire once: %v", err)
	}
	f.FailNext("PostAnswer", true)
	if _, err := f.PostAnswer(ctx, 2, "b"); err == nil || count() != start+2 {
		t.Fatalf("after-write failure: err=%v n=%d", err, count())
	}
	f.FailNext("RemoveLabel", false)
	_ = f.AddLabel(ctx, 2, LabelNeedsHuman)
	if err := f.RemoveLabel(ctx, 2, LabelNeedsHuman); err == nil {
		t.Fatal("RemoveLabel should fail")
	}
	if is, _ := f.GetIssue(ctx, 2); !is.HasLabel(LabelNeedsHuman) {
		t.Fatal("label gone after a failed removal")
	}
	f.FailNext("PostConsequenceAnswer", true)
	if _, err := f.PostConsequenceAnswer(ctx, 2, ConsequenceMarker{Run: "r", Attempt: 1, Commit: c40, Yes: true}); err == nil {
		t.Fatal("expected failure")
	}
	if count() != start+3 {
		t.Fatal("after-write consequence answer must be on the host")
	}
	err := errors.New("x")
	_ = err
}

func TestFakeEditIssueBody(t *testing.T) {
	f := NewFake(DemoFixture(), newTestClock().Now)
	f.EditIssueBody(1, "new body")
	is, _ := f.GetIssue(context.Background(), 1)
	if is.Body != "new body" {
		t.Fatalf("body = %q", is.Body)
	}
}

func TestAnswerBodyIsWhatPostAnswerPosts(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	for _, text := range []string{"blue", "a <!-- oxf:answer --> b @team Fixes #1", "two\nlines\n"} {
		id, err := f.PostAnswer(ctx, 2, text)
		if err != nil {
			t.Fatal(err)
		}
		cs, _ := f.ListComments(ctx, 2, time.Time{})
		var got string
		for _, c := range cs {
			if c.ID == id {
				got = c.Body
			}
		}
		if got == "" || got != AnswerBody(text) {
			t.Fatalf("AnswerBody(%q) = %q, host has %q", text, AnswerBody(text), got)
		}
	}
}

func TestFakeFailAfterAndCreateSubIssues(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	prop := func(title string) []api.IssueProposal {
		return []api.IssueProposal{{Title: title, Body: "b", Criteria: []int{1}}}
	}
	children := func() int {
		n := 0
		for i := 1; i < 50; i++ {
			if f.Parent(i) == 1 {
				n++
			}
		}
		return n
	}
	// before the write: nothing is made
	f.FailNext("CreateSubIssues", false)
	if _, err := f.CreateSubIssues(ctx, 1, prop("a")); err == nil || children() != 0 {
		t.Fatalf("before-write: err=%v children=%d", err, children())
	}
	// the next call is clean
	if _, err := f.CreateSubIssues(ctx, 1, prop("a")); err != nil || children() != 1 {
		t.Fatalf("clean: %v %d", err, children())
	}
	// after the write: the issue exists, the reply is lost
	f.FailNext("CreateSubIssues", true)
	if _, err := f.CreateSubIssues(ctx, 1, prop("b")); err == nil || children() != 2 {
		t.Fatalf("after-write: err=%v children=%d", err, children())
	}
	// FailAfter skips the given number of calls
	f.FailAfter("AddLabel", 2, false)
	for i, wantErr := range []bool{false, false, true, false} {
		err := f.AddLabel(ctx, 2, LabelPlanned)
		if (err != nil) != wantErr {
			t.Fatalf("call %d: err=%v", i, err)
		}
	}
}
