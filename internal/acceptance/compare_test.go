package acceptance

import (
	"reflect"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func TestCompare(t *testing.T) {
	W, N, C := api.StateWorking, api.StateNotWorking, api.StateCouldNotConfirm
	cases := []struct {
		name          string
		before, after map[int]api.AcceptanceState
		want          Comparison
	}{
		{"nothing", nil, nil, Comparison{}},
		{"unchanged", map[int]api.AcceptanceState{1: W, 2: N, 3: C}, map[int]api.AcceptanceState{1: W, 2: N, 3: C},
			Comparison{Unchanged: []int{1, 2, 3}}},
		{"regression", map[int]api.AcceptanceState{1: W}, map[int]api.AcceptanceState{1: N},
			Comparison{Regressed: []int{1}}},
		{"improvement", map[int]api.AcceptanceState{1: N, 2: C}, map[int]api.AcceptanceState{1: W, 2: W},
			Comparison{Improved: []int{1, 2}}},
		{"working to could not confirm is a regression", map[int]api.AcceptanceState{1: W}, map[int]api.AcceptanceState{1: C},
			Comparison{Regressed: []int{1}}},
		{"other change", map[int]api.AcceptanceState{1: N, 2: C}, map[int]api.AcceptanceState{1: C, 2: N},
			Comparison{Otherwise: []int{1, 2}}},
		{"missing before counts as could not confirm", nil, map[int]api.AcceptanceState{1: W, 2: N},
			Comparison{Improved: []int{1}, Otherwise: []int{2}}},
		{"missing after counts as could not confirm", map[int]api.AcceptanceState{1: W, 2: N}, nil,
			Comparison{Regressed: []int{1}, Otherwise: []int{2}}},
		{"sorted", map[int]api.AcceptanceState{9: W, 3: W, 5: N}, map[int]api.AcceptanceState{9: N, 3: N, 5: W},
			Comparison{Regressed: []int{3, 9}, Improved: []int{5}}},
		{"unknown state is could not confirm", map[int]api.AcceptanceState{1: "done"}, map[int]api.AcceptanceState{1: W},
			Comparison{Improved: []int{1}}},
	}
	for _, c := range cases {
		got := Compare(c.before, c.after)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestStatesOf(t *testing.T) {
	got := StatesOf([]Result{{ID: 1, State: api.StateWorking}, {ID: 2, State: api.StateNotWorking}})
	want := map[int]api.AcceptanceState{1: api.StateWorking, 2: api.StateNotWorking}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}
