package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/staging"
	"github.com/openagentix/factory/internal/verify"
)

// storyBeats is the story as data: a list of beats, each done by real code.
// What is scripted is the second opinion (see secondOpinion) and the seeded
// approval of goal 1. The story ends at a waiting point. It never answers a
// question, says yes or no, approves goal 2 or presses Accept.
func storyBeats() []beat {
	return []beat{
		{"write_checks", beatWriteChecks},
		{"build_move", ticketBeat("move", 0)},
		{"checks_fail", ticketBeat("confirm", 0)},
		{"second_try", ticketBeat("confirm", 1)},
		{"question", ticketBeat("ask", 0)},
		{"unconfirmed", beatUnconfirmed},
		{"risk", ticketBeat("staff", 0)},
		{"taken_out", ticketBeat("ahead", 0)},
		{"offer_accept", beatOfferAccept},
	}
}

func (d *Demo) ticketJob(key string, try int) (job, error) {
	specs := tickets()
	for _, s := range specs {
		if s.key != key {
			continue
		}
		if try >= len(s.scripts) {
			return job{}, fmt.Errorf("no script for try %d of %s", try, key)
		}
		var deps []int
		for _, i := range s.depends {
			deps = append(deps, d.tk[specs[i].key])
		}
		return job{task: api.TaskImplement, goal: goalMove, ticket: d.tk[key], served: s.served, deps: deps, title: s.title, script: s.scripts[try]}, nil
	}
	return job{}, fmt.Errorf("no ticket %s", key)
}

func ticketBeat(key string, try int) func(context.Context, *Demo) error {
	return func(ctx context.Context, d *Demo) error {
		j, err := d.ticketJob(key, try)
		if err != nil {
			return err
		}
		res, err := d.work(ctx, j)
		if err != nil || !res.ok {
			return err // a failed try, or a question, is a result of the story, not an error
		}
		dec, _, err := d.decide(ctx, j, res)
		if err != nil {
			return err
		}
		return d.admit(ctx, j, res, dec)
	}
}

func beatWriteChecks(ctx context.Context, d *Demo) error {
	j := job{task: api.TaskAcceptance, goal: goalMove, title: "Checks for goal 1", script: acceptanceScript()}
	res, err := d.work(ctx, j)
	if err != nil {
		return err
	}
	if !res.ok {
		return errors.New("the checks could not be written")
	}
	// The baseline: the checks run on the commit before any building, red first.
	opts := acceptance.Options{Check: verify.CheckOptions{Deadline: 30 * time.Second, Environ: d.env}}
	b, err := acceptance.Baseline(ctx, d.repo, acceptance.Request{Judged: d.def, Source: res.commit, Goal: goalMove, AcceptancePath: api.DefaultAcceptancePath}, opts)
	if err != nil {
		return err
	}
	states, err := acceptance.BaselineStates(b)
	if err != nil {
		return err
	}
	dec, _, err := d.decide(ctx, j, res)
	if err != nil {
		return err
	}
	if err := d.mutate(ctx, func() error {
		d.led.goal(goalMove, func(g *collect.GoalFacts) {
			g.AcceptanceSource, g.Baseline, g.ManifestEntry = res.commit, states, res.runID
		})
		return nil
	}); err != nil {
		return err
	}
	return d.admit(ctx, j, res, dec)
}

// beatUnconfirmed tells the journal about a line the checks could not
// confirm. The fact comes from the staging state, which the acceptance
// package judged.
func beatUnconfirmed(ctx context.Context, d *Demo) error {
	st, ok, err := d.stgSt.Latest(ctx)
	if err != nil || !ok {
		return errors.New("no staging state to read")
	}
	found := false
	for _, s := range st.Criteria[goalMove] {
		if s == api.StateCouldNotConfirm {
			found = true
		}
	}
	if !found {
		return errors.New("no line was left unconfirmed")
	}
	return d.mutate(ctx, func() error {
		d.led.journal("could_not_confirm", goalMove, 0, d.now())
		return nil
	})
}

// beatOfferAccept checks that there is something in the holding area for the
// person to accept. It presses nothing.
func beatOfferAccept(ctx context.Context, d *Demo) error {
	st, ok, err := d.stgSt.LatestPublished(ctx)
	if err != nil || !ok || len(st.Applied) == 0 {
		return errors.New("nothing is in the holding area")
	}
	return nil
}

// Accept accepts the whole holding area, as the person's action will. The
// story never calls it. It is a driver method for the piece that adds the
// route.
func (d *Demo) Accept(ctx context.Context) (staging.AcceptResult, error) {
	var res staging.AcceptResult
	err := d.mutate(ctx, func() error {
		st, ok, err := d.stgSt.LatestPublished(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return staging.ErrNothingToAccept
		}
		res, err = d.stg.Accept(ctx, st.Seq, st.Commit)
		if err != nil {
			return err
		}
		if res.Outcome != staging.OutcomeAccepted {
			d.led.with(func(f *collect.LedgerFacts) { f.PendingSince = res.Pending.Started })
			return nil
		}
		now := d.now()
		for _, a := range st.Applied {
			if a.Ticket != a.Goal {
				d.led.with(func(f *collect.LedgerFacts) {
					f.Accepted = append(f.Accepted, acceptedIn(a, now))
				})
			}
		}
		d.led.journal("accepted", goalMove, 0, now)
		// The default branch is the accepted state. Fetch it into the mirror,
		// as staging asks the caller to do.
		return d.repo.SetDefaultMirror(ctx, st.Commit, d.def)
	})
	return res, err
}

// Undo takes one change out of the holding area, with what depends on it.
func (d *Demo) Undo(ctx context.Context, entryID string) ([]string, error) {
	var out []string
	err := d.mutate(ctx, func() error {
		st, ids, err := d.stg.Undo(ctx, entryID)
		if err != nil {
			return err
		}
		out = ids
		d.observe(st, job{})
		return nil
	})
	return out, err
}

var _ = repo.ZeroID
