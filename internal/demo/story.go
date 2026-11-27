package demo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/runner"
	"github.com/openagentix/factory/internal/staging"
	"github.com/openagentix/factory/internal/verify"
)

// storyBeats is the story as data: a list of beats, each done by real code.
// What is scripted is the second opinion (see secondOpinion) and the seeded
// approval of goal 1. The story ends at a waiting point. It never answers a
// question, says yes or no, approves goal 2 or presses Accept.
//
// Each beat that does work says the outcome it expects from the real code,
// the failing beats too. When the real outcome differs, the story stops with
// a DivergedError: it never reports a good end for work that did not happen.
func storyBeats() []beat {
	return []beat{
		{"write_checks", beatWriteChecks},
		{"build_move", ticketBeat("move", 0, OutcomeAdmitted)},
		{"checks_fail", ticketBeat("confirm", 0, checkFailedOutcome(gate.OutcomeRework, "syntax"))},
		{"second_try", ticketBeat("confirm", 1, OutcomeAdmitted)},
		{"question", ticketBeat("ask", 0, questionOutcome(1, false))},
		{"unconfirmed", beatUnconfirmed},
		{"risk", ticketBeat("staff", 0, consequenceOutcome(gate.ExceptionHighRisk, floor.RiskSecurity))},
		{"taken_out", ticketBeat("ahead", 0, takenOutOutcome(staging.RemovedAcceptance, 1))},
		{"offer_accept", beatOfferAccept},
	}
}

// Outcome is what the real code did with one job, as a string of codes only:
// a kind, then the exact reasons. It never holds text an agent wrote. The
// names in it are check names, risk reasons and numbers that this package
// or its dependencies define.
type Outcome string

// The outcomes without a reason.
const (
	OutcomeAdmitted   Outcome = "admitted"    // passed every stage and is in the holding area
	OutcomeNotApplied Outcome = "not_applied" // passed the gate and is not in the holding area
	OutcomeNoChange   Outcome = "no_change"
)

const (
	outcomeFailedPrefix Outcome = "failed:" // followed by the end code
)

// checkFailedOutcome is a change whose project checks failed: what the gate
// did with it, and the names of the checks that failed.
func checkFailedOutcome(gateOutcome gate.Outcome, checks ...string) Outcome {
	names := append([]string(nil), checks...)
	sort.Strings(names)
	return Outcome("check_failed:" + string(gateOutcome) + ":" + strings.Join(names, ","))
}

// questionOutcome is a needs-human end with n questions.
func questionOutcome(n int, blank bool) Outcome {
	o := "question:" + strconv.Itoa(n)
	if blank {
		o += ":blank"
	}
	return Outcome(o)
}

// consequenceOutcome is a change the gate holds for a person.
func consequenceOutcome(kind gate.ExceptionKind, risk floor.RiskReason) Outcome {
	return Outcome("consequence:" + string(kind) + ":" + string(risk))
}

// takenOutOutcome is a change the holding area took out again, with the
// reason and the criteria it concerns.
func takenOutOutcome(reason staging.RemovalReason, criteria ...int) Outcome {
	o := "taken_out:" + string(reason)
	for _, c := range criteria {
		o += ":" + strconv.Itoa(c)
	}
	return Outcome(o)
}

// DivergedError says the real outcome of a beat is not the one the story
// expects.
type DivergedError struct {
	Want, Got Outcome
}

func (e *DivergedError) Error() string {
	return fmt.Sprintf("the real outcome was %q and the story expects %q", e.Got, e.Want)
}

func (d *Demo) ticketJob(key string, try int) (job, error) {
	for _, goal := range []int{goalMove, goalCancel} {
		specs := ticketsOf(goal)
		for _, s := range specs {
			if s.key != key {
				continue
			}
			if try < 0 || try >= len(s.scripts) {
				return job{}, fmt.Errorf("no script for try %d of %s", try, key)
			}
			var deps []int
			for _, i := range s.depends {
				deps = append(deps, d.tk[specs[i].key])
			}
			return job{task: api.TaskImplement, goal: s.goal, ticket: d.tk[key], served: s.served, deps: deps, title: s.title,
				script: s.scripts[try], key: key, try: try}, nil
		}
	}
	return job{}, fmt.Errorf("no ticket %s", key)
}

func ticketBeat(key string, try int, want Outcome) func(context.Context, *Demo) error {
	return func(ctx context.Context, d *Demo) error {
		j, err := d.ticketJob(key, try)
		if err != nil {
			return err
		}
		res, err := d.work(ctx, j)
		if err != nil {
			return err
		}
		got, err := d.finish(ctx, j, res)
		if err != nil {
			return err
		}
		if got != want {
			return &DivergedError{Want: want, Got: got}
		}
		return nil
	}
}

// finish takes a job that has run through the gate and into the holding area
// when it passed, and says what happened.
func (d *Demo) finish(ctx context.Context, j job, res *result) (Outcome, error) {
	if res.outcome.Status == runner.StatusNeedsHuman {
		n, blank := 0, false
		if res.outcome.Result != nil {
			for _, q := range res.outcome.Result.Questions {
				if strings.TrimSpace(q) == "" {
					blank = true
				} else {
					n++
				}
			}
		}
		return questionOutcome(n, blank), nil
	}
	if !res.ok {
		switch {
		case res.endCode == floor.CheckFailed:
			return d.checkFailedOutcome(j, res), nil
		case res.endCode != "":
			return outcomeFailedPrefix + Outcome(res.endCode), nil
		}
		return OutcomeNoChange, nil
	}
	dec, _, err := d.decide(ctx, j, res)
	if err != nil {
		return "", err
	}
	return d.admit(ctx, j, res, dec)
}

// checkFailedOutcome asks the gate what it does with a change whose checks
// failed, and says it with the names of the failed checks. The gate must give
// the verify reason of each of them, or the outcome says it did not.
func (d *Demo) checkFailedOutcome(j job, res *result) Outcome {
	ref := gate.ChangeRef{Ticket: max(j.ticket, j.goal), Attempt: res.attempt, Commit: res.commit, BuilderRun: res.runID}
	dec := gate.Decide(gate.Facts{Task: j.task, Change: ref,
		Verify: gate.VerifyFacts{ProtectedOK: res.protect.OK, ChecksPassed: false, FailedChecks: res.failed},
		Risk:   api.Risk{Level: api.RiskLow}, BuilderHarness: api.HarnessStub, MaxAttempts: maxAttempts})
	o := checkFailedOutcome(dec.Outcome, res.failed...)
	for _, c := range res.failed {
		found := false
		for _, r := range dec.Reasons {
			if r == "verify: task check failed: "+c {
				found = true
			}
		}
		if !found {
			return o + ":reason_missing"
		}
	}
	return o
}

func beatWriteChecks(ctx context.Context, d *Demo) error { return d.writeChecks(ctx, goalMove) }

// writeChecks has the agent write the checks of a goal, and adds them to the
// holding area when the gate passes them.
func (d *Demo) writeChecks(ctx context.Context, goal int) error {
	j := job{task: api.TaskAcceptance, goal: goal, title: fmt.Sprintf("Checks for goal %d", goal), script: acceptanceScriptFor(goal)}
	res, err := d.work(ctx, j)
	if err != nil {
		return err
	}
	if !res.ok {
		return errors.New("the checks could not be written")
	}
	// The baseline: the checks run on the commit before any building, red first.
	opts := acceptance.Options{Check: verify.CheckOptions{Deadline: 30 * time.Second, Environ: d.env}}
	b, err := acceptance.Baseline(ctx, d.repo, acceptance.Request{Judged: d.def, Source: res.commit, Goal: goal, AcceptancePath: api.DefaultAcceptancePath}, opts)
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
	// The facts about the checks are written only after the holding area has
	// taken them. Until then the staging step reads the source from d.pending.
	d.mu.Lock()
	d.pendingSource = map[int]string{goal: res.commit}
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.pendingSource = nil; d.mu.Unlock() }()
	got, err := d.admitThen(ctx, j, res, dec, func() {
		d.led.goal(goal, func(g *collect.GoalFacts) {
			g.AcceptanceSource, g.Baseline, g.ManifestEntry = res.commit, states, res.runID
		})
	})
	if err != nil {
		return err
	}
	if got != OutcomeAdmitted {
		return &DivergedError{Want: OutcomeAdmitted, Got: got}
	}
	return nil
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

var _ = repo.ZeroID
