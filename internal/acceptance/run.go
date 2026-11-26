package acceptance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/verify"
)

// DefaultTotalDeadline is the time limit for one Run when none is given.
const DefaultTotalDeadline = 30 * time.Minute

// Request names what to run.
type Request struct {
	// Judged is the commit under test, as a revision of the repository.
	Judged string
	// Source is the acceptance source: the commit where the reviewed checks
	// landed. The manifest and every file under the acceptance path come
	// from here, never from Judged.
	Source string
	// Goal is the number of the goal.
	Goal int
	// AcceptancePath is the root of the checks. Empty means
	// api.DefaultAcceptancePath.
	AcceptancePath string
}

// Options tune a run.
type Options struct {
	// Check is passed to the check runner: the deadline of one check, the
	// size limits of a copy, and the environment to scrub.
	Check verify.CheckOptions
	// TotalDeadline bounds the whole run. Zero means DefaultTotalDeadline.
	// Criteria not finished by then are could not confirm, with NoteTotalLimit.
	TotalDeadline time.Duration
}

// Report is the result of one run.
type Report struct {
	Goal int
	// Judged and Source are the full commit ids.
	Judged, Source string
	// Results follow the order of the manifest.
	Results []Result
	// SharedChecks lists groups of criteria that name the same command. A
	// group has two or more ids. This is allowed and reported.
	SharedChecks [][]int
	// TotalLimitReached is true when TotalDeadline ended the run early.
	TotalLimitReached bool
}

// Run runs the check of every criterion of a goal and gives each criterion
// its state.
//
// The manifest is read from req.Source. Then the judged commit is combined
// with the acceptance path of the source (see buildOverlay), so the checks
// that run are the reviewed ones. If Judged and Source are the same commit,
// no combining is needed. Each check runs alone, on its own fresh copy made
// from git objects, so a check cannot leave a file for a later check. This
// costs one export per check. Checks run one after the other. A criterion
// with no check is could not confirm and nothing is run for it.
//
// The state of a criterion comes from api.ComputeState and the exit code. A
// check that hits its deadline is not working yet. Run returns an error when
// it cannot set up (a bad request, a manifest that is not valid, a copy that
// fails), and when ctx ends, with the report so far.
func Run(ctx context.Context, r *repo.Repo, req Request, opts Options) (*Report, error) {
	if r == nil {
		return nil, errors.New("acceptance: no repository")
	}
	if err := checkAcceptancePath(req.AcceptancePath); err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	if req.AcceptancePath == "" {
		req.AcceptancePath = api.DefaultAcceptancePath
	}
	if req.Judged == "" || req.Source == "" {
		return nil, errors.New("acceptance: the judged commit and the acceptance source are both required")
	}
	judged, err := r.Resolve(ctx, req.Judged)
	if err != nil {
		return nil, fmt.Errorf("acceptance: judged commit: %w", err)
	}
	source, err := r.Resolve(ctx, req.Source)
	if err != nil {
		return nil, fmt.Errorf("acceptance: acceptance source: %w", err)
	}
	m, err := LoadManifest(ctx, r, source, req.AcceptancePath, req.Goal)
	if err != nil {
		return nil, err
	}
	total := opts.TotalDeadline
	if total <= 0 {
		total = DefaultTotalDeadline
	}
	runCtx, cancel := context.WithTimeout(ctx, total)
	defer cancel()

	rep := &Report{Goal: req.Goal, Judged: judged, Source: source, SharedChecks: sharedChecks(m)}
	target := judged
	if judged != source {
		lim := repo.Limits{MaxTotalBytes: opts.Check.MaxTotalBytes, MaxFileBytes: opts.Check.MaxFileBytes, MaxEntries: opts.Check.MaxEntries}
		target, err = buildOverlay(runCtx, r, judged, source, req.AcceptancePath, lim)
		if err != nil {
			if ctx.Err() == nil && runCtx.Err() != nil {
				err = fmt.Errorf("the time limit for the whole run ended while the checks were being laid over the change: %w", err)
			}
			return nil, fmt.Errorf("acceptance: %w", err)
		}
	}

	for _, c := range m.Criteria {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		res := Result{ID: c.ID, Text: c.Text}
		switch {
		case !c.HasCheck():
			res.State = api.ComputeState(c, api.CheckNotRun)
			res.Note = NoteNoCheck
		case runCtx.Err() != nil:
			rep.TotalLimitReached = true
			res.Check = c.Run
			res.State = api.ComputeState(c, api.CheckNotRun)
			res.Note = NoteTotalLimit
		default:
			res.Check = c.Run
			outs, runErr := verify.RunChecks(runCtx, r.Dir(), target, []api.Check{{Name: fmt.Sprintf("criterion-%d", c.ID), Run: c.Run}}, opts.Check)
			if err := ctx.Err(); err != nil {
				return rep, err
			}
			if runErr != nil && runCtx.Err() != nil {
				rep.TotalLimitReached = true
				res.State = api.ComputeState(c, api.CheckNotRun)
				res.Note = NoteTotalLimit
				break
			}
			if runErr != nil {
				return rep, fmt.Errorf("acceptance: criterion %d: %w", c.ID, runErr)
			}
			if len(outs) != 1 {
				return rep, fmt.Errorf("acceptance: criterion %d: the check runner gave %d outcomes, want 1", c.ID, len(outs))
			}
			fillFromOutcome(&res, c, outs[0])
		}
		rep.Results = append(rep.Results, res)
	}
	return rep, nil
}

// fillFromOutcome sets the facts and the state from what the runner reported.
// The output of the check is not read.
func fillFromOutcome(res *Result, c api.ManifestCriterion, o verify.CheckOutcome) {
	res.Ran = o.Err == ""
	res.ExitCode = o.Result.ExitCode
	res.DurationSeconds = o.Result.DurationSeconds
	res.TimedOut = o.TimedOut
	var outcome api.CheckOutcome
	switch {
	case o.Err != "":
		outcome = api.CheckNotRun
		res.Note = NoteCouldNotStart
	case o.TimedOut:
		outcome = api.CheckFailed
		res.Note = NoteTimedOut
	case o.Passed():
		outcome = api.CheckPassed
	default:
		outcome = api.CheckFailed
	}
	res.State = api.ComputeState(c, outcome)
}

// sharedChecks groups criteria that name the same command, ignoring spaces at
// the ends.
func sharedChecks(m *api.Manifest) [][]int {
	byCmd := map[string][]int{}
	for _, c := range m.Criteria {
		if c.HasCheck() {
			k := strings.TrimSpace(c.Run)
			byCmd[k] = append(byCmd[k], c.ID)
		}
	}
	var out [][]int
	for _, ids := range byCmd {
		if len(ids) > 1 {
			sort.Ints(ids)
			out = append(out, ids)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
