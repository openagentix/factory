package acceptance

import (
	"sort"

	"github.com/openagentix/factory/internal/api"
)

// Comparison is the change between the states at two points.
type Comparison struct {
	// Regressed lists criteria that were working and no longer are.
	Regressed []int
	// Improved lists criteria that are working now and were not.
	Improved []int
	// Unchanged lists criteria with the same state at both points.
	Unchanged []int
	// Otherwise lists criteria that changed between not working yet and
	// could not confirm, in either direction.
	Otherwise []int
}

// Compare lists what changed between two sets of states. A criterion missing
// from a set, or with a state that is not one of the three, counts as could
// not confirm. A criterion that was working and is now either of the other
// two states is a regression: it is no longer confirmed. Each list is sorted.
func Compare(before, after map[int]api.AcceptanceState) Comparison {
	ids := map[int]bool{}
	for id := range before {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	sorted := make([]int, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Ints(sorted)
	var c Comparison
	for _, id := range sorted {
		b, a := norm(before[id]), norm(after[id])
		switch {
		case a == b:
			c.Unchanged = append(c.Unchanged, id)
		case b == api.StateWorking:
			c.Regressed = append(c.Regressed, id)
		case a == api.StateWorking:
			c.Improved = append(c.Improved, id)
		default:
			c.Otherwise = append(c.Otherwise, id)
		}
	}
	return c
}

func norm(s api.AcceptanceState) api.AcceptanceState {
	if !s.Valid() {
		return api.StateCouldNotConfirm
	}
	return s
}
