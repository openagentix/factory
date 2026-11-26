package acceptance

import "github.com/openagentix/factory/internal/api"

// EvidenceEntries makes the acceptance entries of the evidence bundle from a
// run's results, in the order of the results. State is the result's state.
// Check is the manifest command, set only for working and not working, as the
// record defines it: those states come only from a check that ran. Before is
// the state from the earlier point in before, and is left empty for a
// criterion that has none, because no earlier state is known.
func EvidenceEntries(results []Result, before map[int]api.AcceptanceState) []api.AcceptanceEntry {
	out := make([]api.AcceptanceEntry, 0, len(results))
	for _, r := range results {
		e := api.AcceptanceEntry{ID: r.ID, State: r.State, Before: before[r.ID]}
		if r.State == api.StateWorking || r.State == api.StateNotWorking {
			e.Check = r.Check
		}
		out = append(out, e)
	}
	return out
}
