package acceptance

import (
	"context"

	"github.com/openagentix/factory/internal/api"

	"github.com/openagentix/factory/internal/repo"
)

// BaselineReport is a run before any building, with one added fact.
type BaselineReport struct {
	// Report is the run. Its states are the three states, unchanged.
	Report *Report
	// AlreadyWorking lists criteria whose check passed before any building,
	// sorted. A check that already passes proves nothing about the work to
	// come. It may be expected (for example "old clients still work") or it
	// may be a check that cannot fail. A person decides which.
	AlreadyWorking []int
}

// WasWorking reports whether the criterion was already working at the
// baseline.
func (b *BaselineReport) WasWorking(id int) bool {
	for _, x := range b.AlreadyWorking {
		if x == id {
			return true
		}
	}
	return false
}

// Baseline runs the checks of the goal on the commit before any building,
// the red first rule. req.Judged is that commit, which is the parent of the
// first change. req.Source is where the reviewed checks landed. The checks
// are laid over the parent as in Run, so they need not exist there.
func Baseline(ctx context.Context, r *repo.Repo, req Request, opts Options) (*BaselineReport, error) {
	rep, err := Run(ctx, r, req, opts)
	if rep == nil {
		return nil, err
	}
	b := &BaselineReport{Report: rep}
	for _, res := range rep.Results {
		if res.State == api.StateWorking {
			b.AlreadyWorking = append(b.AlreadyWorking, res.ID)
		}
	}
	return b, err
}
