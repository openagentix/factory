package acceptance

import (
	"context"
	"reflect"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func TestBaseline_FlagsChecksThatAlreadyPass(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("old.txt", "old client works")
	parent := tr.commit("before any building")
	tr.writeManifest(40,
		crit(1, "old clients still work", "test -f old.txt"),
		crit(2, "new feature exists", "test -f feature.txt"),
		crit(3, "a check that cannot fail", "true"),
		crit(4, "no check", ""),
	)
	source := tr.commit("reviewed checks")

	b, err := Baseline(context.Background(), tr.open(), Request{Judged: parent, Source: source, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 3}; !reflect.DeepEqual(b.AlreadyWorking, want) {
		t.Errorf("already working = %v, want %v", b.AlreadyWorking, want)
	}
	// The three states are not changed by the baseline.
	want := map[int]api.AcceptanceState{1: api.StateWorking, 2: api.StateNotWorking, 3: api.StateWorking, 4: api.StateCouldNotConfirm}
	if got := statesOf(b.Report.Results); !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v", got)
	}
	if !b.WasWorking(1) || b.WasWorking(2) || b.WasWorking(4) || b.WasWorking(99) {
		t.Error("WasWorking is wrong")
	}
}

func TestBaseline_RunsOnTheParentNotOnTheChange(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("a.txt", "x")
	parent := tr.commit("parent")
	tr.writeManifest(40, crit(1, "new feature exists", "test -f feature.txt"))
	tr.write("feature.txt", "built")
	built := tr.commit("built with checks")
	b, err := Baseline(context.Background(), tr.open(), Request{Judged: parent, Source: built, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.AlreadyWorking) != 0 {
		t.Errorf("the feature was not there at the parent: %v", b.AlreadyWorking)
	}
}
