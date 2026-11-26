package acceptance

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func testOptions() Options {
	var o Options
	o.Check.Environ = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GITHUB_TOKEN=secret-token-value", "ANTHROPIC_API_KEY=secret-key-value"}
	o.Check.Deadline = time.Minute
	return o
}

func stateOf(t *testing.T, rep *Report, id int) Result {
	t.Helper()
	for _, r := range rep.Results {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no result for criterion %d", id)
	return Result{}
}

func TestRun_StatesComeFromTheExitCode(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("feature.txt", "here")
	tr.writeManifest(40,
		crit(1, "passes", "test -f feature.txt"),
		crit(2, "fails but says PASS", "echo PASS all 12 checks passed; exit 3"),
		crit(3, "no check", ""),
		crit(4, "passes silently", "exit 0"),
	)
	c := tr.commit("checks")
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]api.AcceptanceState{1: api.StateWorking, 2: api.StateNotWorking, 3: api.StateCouldNotConfirm, 4: api.StateWorking}
	if got := StatesOf(rep.Results); !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
	r2 := stateOf(t, rep, 2)
	if !r2.Ran || r2.ExitCode != 3 || r2.Text != "fails but says PASS" || r2.Check == "" {
		t.Errorf("result 2 = %+v", r2)
	}
	r3 := stateOf(t, rep, 3)
	if r3.Ran || r3.Note != NoteNoCheck || r3.Check != "" {
		t.Errorf("result 3 = %+v", r3)
	}
	if len(rep.Results) != 4 || rep.Results[0].ID != 1 || rep.Results[3].ID != 4 {
		t.Errorf("results must follow the manifest order: %+v", rep.Results)
	}
}

func TestRun_AWhitespaceCommandIsNoCheck(t *testing.T) {
	tr := newTestRepo(t)
	m := crit(1, "blank", "   \t ")
	m.Reason = "gave up"
	tr.writeManifest(40, m)
	c := tr.commit("checks")
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r := stateOf(t, rep, 1); r.State != api.StateCouldNotConfirm || r.Ran {
		t.Errorf("result = %+v", r)
	}
}

// Attack A: the builder rewrites the manifest and a check in its change.
func TestRun_TheJudgedChangeCannotRewriteTheChecks(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("acceptance/40/check1.sh", "test -f feature.txt\n")
	tr.writeManifest(40,
		crit(1, "the feature exists", "sh acceptance/40/check1.sh"),
		crit(2, "it was reviewed to need a file", "test -f other.txt"),
	)
	source := tr.commit("reviewed checks")

	// The builder's change: the check always passes, the manifest drops the
	// second criterion, and an extra helper file is added to the directory.
	tr.write("acceptance/40/check1.sh", "exit 0\n")
	tr.write("acceptance/40/extra.sh", "exit 0\n")
	tr.writeManifest(40, crit(1, "the feature exists", "exit 0"))
	judged := tr.commit("builder change")

	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("the manifest must come from the source: %+v", rep.Results)
	}
	for _, r := range rep.Results {
		if r.State != api.StateNotWorking {
			t.Errorf("criterion %d = %s, want not working", r.ID, r.State.Name())
		}
	}
	if r := stateOf(t, rep, 1); r.Check != "sh acceptance/40/check1.sh" {
		t.Errorf("check = %q", r.Check)
	}
}

func TestRun_TheJudgedAcceptanceDirectoryIsReplacedNotMerged(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("acceptance/40/check1.sh", "test ! -e acceptance/40/extra.sh && test ! -e acceptance/41\n")
	tr.writeManifest(40, crit(1, "no leftovers", "sh acceptance/40/check1.sh"))
	source := tr.commit("reviewed checks")
	tr.write("acceptance/40/extra.sh", "x")
	tr.write("acceptance/41/manifest.json", "x")
	judged := tr.commit("builder change")
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r := stateOf(t, rep, 1); r.State != api.StateWorking {
		t.Errorf("state = %s: files the source does not have must be gone", r.State.Name())
	}
}

// Attack A: names that collide with the check files on a case-insensitive
// file system. Either the run is refused or the real check is used. It must
// never show working.
func TestRun_CaseVariantNamesCannotWin(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("acceptance/40/check1.sh", "test -f feature.txt\n")
	tr.writeManifest(40, crit(1, "the feature exists", "sh acceptance/40/check1.sh"))
	source := tr.commit("reviewed checks")
	tr.write("ACCEPTANCE/40/CHECK1.SH", "exit 0\n")
	tr.write("Acceptance/40/check1.sh", "exit 0\n")
	judged := tr.commit("builder change")
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	if err != nil {
		t.Logf("refused: %v", err)
		return
	}
	if r := stateOf(t, rep, 1); r.State == api.StateWorking {
		t.Error("a case variant made the check pass")
	}
}

func TestRun_TheSourceMustHaveTheAcceptanceFiles(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("a.txt", "x")
	empty := tr.commit("no checks yet")
	tr.writeManifest(40, crit(1, "x", "true"))
	withChecks := tr.commit("checks")
	if _, err := Run(context.Background(), tr.open(), Request{Judged: withChecks, Source: empty, Goal: 40}, testOptions()); err == nil {
		t.Error("a source with no manifest must be refused")
	}
}

func TestRun_EachCheckHasItsOwnCopy(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40,
		crit(1, "leaves a file", "touch leftover.txt"),
		crit(2, "needs the leftover", "test -f leftover.txt"),
		crit(3, "needs no leftover", "test ! -e leftover.txt"),
		crit(4, "also leaves one in a subdirectory", "mkdir -p sub && touch sub/x"),
		crit(5, "needs the subdirectory", "test -d sub"),
	)
	c := tr.commit("checks")
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]api.AcceptanceState{1: api.StateWorking, 2: api.StateNotWorking, 3: api.StateWorking, 4: api.StateWorking, 5: api.StateNotWorking}
	if got := StatesOf(rep.Results); !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestRun_TheCheckRunsInACopyWithAScrubbedEnvironment(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40,
		crit(1, "the copy has the files and no .git", "test -f acceptance/40/manifest.json && test ! -e .git"),
		crit(2, "no secrets in the environment", `test -z "$GITHUB_TOKEN$ANTHROPIC_API_KEY" && ! env | grep -q secret-`),
		crit(3, "the home directory is empty and is not the real one", `test "$HOME" != /nonexistent && test -z "$(ls -A "$HOME")"`),
	)
	c := tr.commit("checks")
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		if r.State != api.StateWorking {
			t.Errorf("criterion %d: %s (exit %d)", r.ID, r.State.Name(), r.ExitCode)
		}
	}
}

func TestRun_ACheckThatHitsItsDeadlineIsNotWorkingYet(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "hangs", "sleep 60"), crit(2, "fine", "true"))
	c := tr.commit("checks")
	o := testOptions()
	o.Check.Deadline = time.Second
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, o)
	if err != nil {
		t.Fatal(err)
	}
	r := stateOf(t, rep, 1)
	if r.State != api.StateNotWorking || !r.TimedOut || r.Note != NoteTimedOut || !r.Ran {
		t.Errorf("result = %+v", r)
	}
	if stateOf(t, rep, 2).State != api.StateWorking {
		t.Error("a later check must still run")
	}
}

func TestRun_TheTotalTimeLimitStopsAStall(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "hangs", "sleep 60"), crit(2, "never starts", "true"), crit(3, "none", ""))
	c := tr.commit("checks")
	o := testOptions()
	o.Check.Deadline = time.Minute
	o.TotalDeadline = 2 * time.Second
	start := time.Now()
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, o)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 40*time.Second {
		t.Error("the total limit did not stop the run")
	}
	if !rep.TotalLimitReached {
		t.Error("TotalLimitReached must be set")
	}
	for _, id := range []int{1, 2} {
		r := stateOf(t, rep, id)
		if r.State != api.StateCouldNotConfirm || r.Note != NoteTotalLimit {
			t.Errorf("criterion %d = %+v", id, r)
		}
	}
	if r := stateOf(t, rep, 3); r.Note != NoteNoCheck {
		t.Errorf("criterion 3 note = %q", r.Note)
	}
}

func TestRun_ACancelledContextReturnsTheError(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "true"))
	c := tr.commit("checks")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunAtSource(ctx, tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions()); err == nil {
		t.Error("a cancelled context must return an error")
	}
}

func TestRun_SharedChecksAreReported(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40,
		crit(1, "a", "true"), crit(2, "b", " true "), crit(3, "c", "test -d ."), crit(4, "d", "true"), crit(5, "e", ""), crit(6, "f", ""))
	c := tr.commit("checks")
	rep, err := RunAtSource(context.Background(), tr.open(), SourceRequest{Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]int{{1, 2, 4}}; !reflect.DeepEqual(rep.SharedChecks, want) {
		t.Errorf("shared = %v, want %v", rep.SharedChecks, want)
	}
}

func TestRun_BadRequests(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "true"))
	c := tr.commit("checks")
	tr.write("more.txt", "x")
	d := tr.commit("change")
	r := tr.open()
	for name, req := range map[string]Request{
		"no judged":      {Source: c, Goal: 40},
		"no source":      {Judged: c, Goal: 40},
		"dash judged":    {Judged: "-x", Source: c, Goal: 40},
		"missing judged": {Judged: "nope", Source: c, Goal: 40},
		"wrong goal":     {Judged: d, Source: c, Goal: 41},
		"bad path":       {Judged: d, Source: c, Goal: 40, AcceptancePath: "../x"},
	} {
		if _, err := Run(context.Background(), r, req, testOptions()); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := Run(context.Background(), nil, Request{Judged: d, Source: c, Goal: 40}, testOptions()); err == nil {
		t.Error("a nil repository must be refused")
	}
}

func TestRun_NoOutputIsKept(t *testing.T) {
	// Result has no field that can hold the output of a check.
	rt := reflect.TypeOf(Result{})
	for i := 0; i < rt.NumField(); i++ {
		if n := strings.ToLower(rt.Field(i).Name); strings.Contains(n, "output") || strings.Contains(n, "log") || strings.Contains(n, "summary") {
			t.Errorf("field %s could carry an opinion", rt.Field(i).Name)
		}
	}
}
