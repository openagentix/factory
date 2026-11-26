package acceptance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// Item 1: which kind of run made a report.
func TestReportKind(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "exit 0"))
	c := tr.commit("checks")
	tr.write("x.txt", "x")
	judged := tr.commit("change")
	r := tr.open()
	ctx := context.Background()

	j, err := Run(ctx, r, Request{Judged: judged, Source: c, Goal: 40}, testOptions())
	if err != nil || j.Kind() != KindJudged {
		t.Fatalf("judged: %v %v", j.Kind(), err)
	}
	s, err := RunAtSource(ctx, r, SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil || s.Kind() != KindAtSource {
		t.Fatalf("at source: %v %v", s.Kind(), err)
	}
	b, err := Baseline(ctx, r, Request{Judged: judged, Source: c, Goal: 40}, testOptions())
	if err != nil || b.Report.Kind() != KindBaseline {
		t.Fatalf("baseline: %v %v", b.Report.Kind(), err)
	}

	if _, err := JudgedStates(j); err != nil {
		t.Errorf("judged states: %v", err)
	}
	if _, err := EvidenceEntries(j, nil); err != nil {
		t.Errorf("judged evidence: %v", err)
	}
	for name, rep := range map[string]*Report{"at source": s, "baseline": b.Report, "hand built": {Results: j.Results}, "nil": nil} {
		if _, err := JudgedStates(rep); !errors.Is(err, ErrNotJudged) {
			t.Errorf("%s: JudgedStates err = %v", name, err)
		}
		if _, err := EvidenceEntries(rep, nil); !errors.Is(err, ErrNotJudged) {
			t.Errorf("%s: EvidenceEntries err = %v", name, err)
		}
	}
	if (&Report{}).Kind() != KindUnknown || KindUnknown != 0 {
		t.Error("the zero value must mean unknown")
	}
	bs, err := BaselineStates(b)
	if err != nil || bs[1] != api.StateWorking {
		t.Errorf("baseline states = %v, %v", bs, err)
	}
	if _, err := BaselineStates(&BaselineReport{Report: j}); err == nil {
		t.Error("a judged report is not a baseline")
	}
	if _, err := BaselineStates(nil); err == nil {
		t.Error("nil baseline")
	}
}

// Item 2: whose fault is a check that could not run.
func brokenSetup(t *testing.T) (*testRepo, string) {
	tr := newTestRepo(t)
	tr.write("lib/helper.sh", "exit 0\n")
	tr.write("lib/tool.sh", "exit 0\n")
	tr.writeManifest(40,
		crit(1, "helper deleted", "sh lib/helper.sh"),
		crit(2, "tool fails with 127", "sh lib/tool.sh"),
		crit(3, "broken everywhere", "no-such-tool-xyz"),
		crit(4, "killed everywhere", "kill -9 $$"),
		crit(5, "helper still there", "sh lib/helper.sh && test -f lib/tool.sh"),
		crit(6, "plain fail at both", "exit 1"),
	)
	return tr, tr.commit("reviewed checks")
}

func TestRun_AChangeThatStopsACheckIsNotWorkingYet(t *testing.T) {
	tr, source := brokenSetup(t)
	tr.remove("lib/helper.sh")
	tr.write("lib/tool.sh", "exit 127\n")
	judged := tr.commit("builder change")
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2, 5} {
		r := stateOf(t, rep, id)
		if r.State != api.StateNotWorking || r.Note != NoteChangeStoppedCheck || r.Cause != CauseChange || r.ExitCode == 0 {
			t.Errorf("criterion %d = %+v", id, r)
		}
	}
	if r := stateOf(t, rep, 1); r.ExitCode != 127 {
		t.Errorf("the exit code of the judged run is kept: %d", r.ExitCode)
	}
	for _, id := range []int{3, 4} {
		r := stateOf(t, rep, id)
		if r.State != api.StateCouldNotConfirm || r.Note != NoteCouldNotRun || r.Cause != CauseCheckBroken {
			t.Errorf("criterion %d = %+v", id, r)
		}
	}
	if r := stateOf(t, rep, 6); r.State != api.StateNotWorking || r.Cause != CauseNone || r.Note != "" {
		t.Errorf("criterion 6 = %+v", r)
	}
	if NoteChangeStoppedCheck != "this change stopped its check from running" {
		t.Errorf("note = %q", NoteChangeStoppedCheck)
	}
	txt := RenderText(rep.Results)
	if !strings.Contains(txt, "This change stopped its check from running.") {
		t.Errorf("render:\n%s", txt)
	}
}

func TestRun_TheRerunRespectsTheTotalLimit(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("lib/h.sh", "sleep 60\n")
	tr.writeManifest(40, crit(1, "a", "sh lib/h.sh; exit 127"), crit(2, "b", "no-such-tool-xyz"))
	source := tr.commit("checks")
	tr.write("x", "x")
	judged := tr.commit("change")
	o := testOptions()
	o.TotalDeadline = 3e9
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, o)
	if err != nil {
		t.Fatal(err)
	}
	// Criterion 1 sleeps 60 s at the judged commit and hits the total limit.
	if !rep.TotalLimitReached {
		t.Error("the total limit must be reached")
	}
	for _, r := range rep.Results {
		if r.State == api.StateWorking {
			t.Errorf("criterion %d is working", r.ID)
		}
	}
}

func TestCompare_BrokenChecks(t *testing.T) {
	W, N, C := api.StateWorking, api.StateNotWorking, api.StateCouldNotConfirm
	// Working to either other state is a regression, whoever's fault it is.
	got := Compare(map[int]api.AcceptanceState{1: W, 2: W}, map[int]api.AcceptanceState{1: N, 2: C})
	if len(got.Regressed) != 2 {
		t.Errorf("regressed = %v", got.Regressed)
	}
	// Not working to could not confirm is never an improvement.
	got = Compare(map[int]api.AcceptanceState{1: N}, map[int]api.AcceptanceState{1: C})
	if len(got.Improved) != 0 || len(got.Regressed) != 0 || len(got.Otherwise) != 1 {
		t.Errorf("comparison = %+v", got)
	}
}

// Item 3: a bad file name in the source acceptance directory is a typed error.
func TestRun_ABadNameInTheSourceIsATypedError(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "true"))
	tr.write("acceptance/40/sub/my check.sh", "x")
	source := tr.commit("checks")
	tr.write("x", "x")
	judged := tr.commit("change")
	_, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	var ne *AcceptanceNameError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %T %v", err, err)
	}
	if ne.Path != "acceptance/40/sub/my check.sh" || !strings.Contains(ne.Error(), "my check.sh") {
		t.Errorf("path = %q, msg = %q", ne.Path, ne.Error())
	}
}
