package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/harness"
	"github.com/openagentix/factory/internal/runner"
	"github.com/openagentix/factory/internal/staging"
	"github.com/openagentix/factory/internal/verify"
)

// job is one piece of work for the agent: a try of a ticket, or the checks of
// a goal.
type job struct {
	task   string // implement or acceptance
	goal   int
	ticket int // 0 for the checks of a goal
	served []int
	deps   []int
	title  string
	script harness.StubScript
}

// result is what a job produced.
type result struct {
	runID   string
	attempt int
	outcome runner.Outcome
	// ok is true when the run reached succeeded with a change.
	ok       bool
	commit   string
	changed  *verify.ChangeSet
	protect  api.ProtectedPathsReport
	failed   []string
	endCode  floor.ReasonCode
	claims   []api.Claim
	question string
}

func (d *Demo) newRunID() string {
	d.runN++
	return fmt.Sprintf("run%03d", d.runN)
}

func (d *Demo) createRun(ctx context.Context, j job) (string, int, error) {
	id := d.newRunID()
	n := d.attempt[j.ticket] + 1
	if j.ticket == 0 {
		n = 1
	}
	ref := fmt.Sprintf("#%d", j.goal)
	if j.ticket > 0 {
		ref = fmt.Sprintf("#%d", j.ticket)
	}
	now := d.now()
	run := &api.Run{ID: id, Task: j.task, Attempt: n, Goal: j.goal, Ticket: j.ticket, Mode: api.ModeUnattended,
		Method: api.MethodBuiltin, MethodUsed: api.MethodBuiltin, Trigger: api.TriggerRef{Kind: api.TriggerFactory, Ref: ref},
		Limits: api.RunLimits{Deadline: "5m"}, Phase: api.PhaseQueued, CreatedAt: now}
	err := d.mutate(ctx, func() error {
		if err := d.st.CreateRun(ctx, run); err != nil {
			return err
		}
		d.attempt[j.ticket] = n // counted only once the record exists
		d.led.run(id, func(r *collect.RunFacts) { r.PhaseSince, r.Deadline = now, 5*time.Minute })
		return nil
	})
	return id, n, err
}

// setPhase moves a run to a new phase and records the time and the facts.
func (d *Demo) setPhase(ctx context.Context, id string, to api.Phase, fn func(*collect.RunFacts)) error {
	return d.mutate(ctx, func() error {
		return d.movePhase(ctx, id, to, fn)
	})
}

func (d *Demo) movePhase(ctx context.Context, id string, to api.Phase, fn func(*collect.RunFacts)) error {
	run, err := d.st.GetRun(ctx, id)
	if err != nil {
		return err
	}
	now := d.now()
	run.Phase = to
	if to == api.PhaseWorking && run.StartedAt == nil {
		run.StartedAt = &now
	}
	if to.IsTerminal() || to == api.PhaseNeedsHuman {
		run.FinishedAt = &now
	}
	if err := d.st.UpdateRun(ctx, run); err != nil {
		return err
	}
	d.led.run(id, func(r *collect.RunFacts) {
		r.PhaseSince = now
		if fn != nil {
			fn(r)
		}
	})
	return nil
}

// runAgent starts the stub harness through the local runner and waits.
func (d *Demo) runAgent(ctx context.Context, id, task string, parent string, script harness.StubScript) (runner.Outcome, error) {
	sdir := filepath.Join(d.cfg.DataDir, "scripts")
	if err := os.MkdirAll(sdir, 0o700); err != nil {
		return runner.Outcome{}, err
	}
	b, err := json.Marshal(script)
	if err != nil {
		return runner.Outcome{}, err
	}
	sp := filepath.Join(sdir, id+".json")
	if err := os.WriteFile(sp, b, 0o600); err != nil {
		return runner.Outcome{}, err
	}
	spec := runner.Spec{
		Repo: d.repo, Commit: parent, DataDir: d.cfg.DataDir,
		Task:        *taskFor(task),
		TriggerJSON: []byte(`{}`),
		Prompt:      harness.PromptParts{TaskPrompt: "Demo task. A stub follows a fixed script.", Trigger: "demo"},
		Adapter:     &harness.Stub{Program: d.cfg.Program},
		Harness:     harness.Spec{StubScript: sp},
		Environ:     d.env,
		Author:      author,
		Deadline:    d.deadline(),
		Grace:       500 * time.Millisecond,
		Limits:      runner.Limits{PipeWait: 2 * time.Second},
	}
	h, err := runner.NewLocal().Start(ctx, spec)
	if err != nil {
		return runner.Outcome{}, err
	}
	o, err := h.Wait(ctx)
	if err != nil {
		_ = h.Cancel(context.WithoutCancel(ctx))
		return runner.Outcome{}, err
	}
	return o, nil
}

// work runs one job to the end of the verify and publish stages. The run
// record follows the real phases. Nothing the agent wrote is trusted: the
// outcome type decides, and the checks run on the commit the runner made.
func (d *Demo) work(ctx context.Context, j job) (*result, error) {
	if d.jobHook != nil {
		d.jobHook(&j)
	}
	id, n, err := d.createRun(ctx, j)
	if err != nil {
		return nil, err
	}
	res := &result{runID: id, attempt: n}
	if err := d.pause(ctx, d.cfg.Dwell); err != nil {
		return nil, err
	}
	if err := d.setPhase(ctx, id, api.PhaseWorking, nil); err != nil {
		return nil, err
	}
	if err := d.pause(ctx, d.cfg.Dwell); err != nil {
		return nil, err
	}
	out, err := d.runAgent(ctx, id, j.task, d.def, j.script)
	if err != nil {
		return nil, err
	}
	res.outcome = out
	if out.Result != nil {
		res.claims = out.Result.Claims
	}
	switch out.Status {
	case runner.StatusNeedsHuman:
		res.question = ""
		if out.Result != nil && len(out.Result.Questions) > 0 {
			res.question = out.Result.Questions[0]
		}
		return res, d.mutate(ctx, func() error {
			if res.question != "" && j.ticket > 0 {
				if err := d.host.PostComment(ctx, j.ticket, id, res.question); err != nil {
					return err
				}
			}
			return d.movePhase(ctx, id, api.PhaseNeedsHuman, nil)
		})
	case runner.StatusSucceeded, runner.StatusSucceededNoChange:
	case runner.StatusTimedOut:
		res.endCode = floor.Deadline
		return res, d.setPhase(ctx, id, api.PhaseFailed, func(r *collect.RunFacts) { r.EndReason = floor.Deadline })
	default:
		// Failed, cancelled or unknown: the run failed. The agent's own words
		// decide nothing.
		res.endCode = floor.BadResult
		return res, d.setPhase(ctx, id, api.PhaseFailed, func(r *collect.RunFacts) { r.EndReason = floor.BadResult })
	}
	if err := d.setPhase(ctx, id, api.PhaseVerifying, nil); err != nil {
		return nil, err
	}
	if err := d.pause(ctx, d.cfg.Dwell); err != nil {
		return nil, err
	}
	if out.Status == runner.StatusSucceededNoChange || !out.Changed {
		// A run that says it succeeded with no change ends as the packages
		// say: succeeded, with no change. The Floor shows no_change.
		f := false
		return res, d.mutate(ctx, func() error {
			for _, p := range []api.Phase{api.PhasePublishing, api.PhaseSucceeded} {
				if err := d.movePhase(ctx, id, p, func(r *collect.RunFacts) { r.HasChange = &f }); err != nil {
					return err
				}
			}
			return nil
		})
	}
	res.commit = out.Commit
	cs, err := verify.ChangedFiles(ctx, d.repo.Dir(), d.def, out.Commit)
	if err != nil {
		return nil, err
	}
	res.changed = cs
	res.protect, err = verify.CheckProtected(d.factory, taskFor(j.task), cs)
	if err != nil {
		return nil, err
	}
	outs, err := verify.RunChecks(ctx, d.repo.Dir(), out.Commit, taskChecks(), verify.CheckOptions{Deadline: 30 * time.Second, Environ: d.env})
	if err != nil {
		return nil, err
	}
	if len(outs) != len(taskChecks()) {
		return nil, errors.New("a check result is missing")
	}
	for i, o := range outs {
		if o.Err != "" || o.TimedOut || !o.Passed() {
			res.failed = append(res.failed, taskChecks()[i].Name)
		}
	}
	if !res.protect.OK || len(res.failed) > 0 {
		code := floor.CheckFailed
		if !res.protect.OK {
			code = floor.ProtectedPath
		}
		res.endCode = code
		return res, d.mutate(ctx, func() error {
			if err := d.movePhase(ctx, id, api.PhaseFailed, func(r *collect.RunFacts) { r.EndReason = code }); err != nil {
				return err
			}
			if j.ticket > 0 {
				d.led.journal("check_failed", 0, j.ticket, d.now())
				d.firstAttempt(j.ticket, n, false)
			}
			return nil
		})
	}
	if err := d.setPhase(ctx, id, api.PhasePublishing, nil); err != nil {
		return nil, err
	}
	if err := d.pause(ctx, d.cfg.Dwell); err != nil {
		return nil, err
	}
	branch := fmt.Sprintf("factory/%s", id)
	t := true
	err = d.mutate(ctx, func() error {
		if err := d.host.PushBranch(ctx, d.repo.Dir(), out.Commit, branch); err != nil {
			return err
		}
		run, err := d.st.GetRun(ctx, id)
		if err != nil {
			return err
		}
		run.Outputs = api.RunOutputs{Branch: branch, Commit: out.Commit}
		if err := d.st.UpdateRun(ctx, run); err != nil {
			return err
		}
		return d.movePhase(ctx, id, api.PhaseSucceeded, func(r *collect.RunFacts) { r.HasChange = &t })
	})
	if err != nil {
		return nil, err
	}
	res.ok = true
	return res, nil
}

// firstAttempt records how the first try of a ticket ended. Later tries are
// not recorded.
func (d *Demo) firstAttempt(ticket, attempt int, reached bool) {
	if attempt != 1 {
		return
	}
	at := d.now()
	d.led.with(func(f *collect.LedgerFacts) {
		for _, x := range f.FirstAttempts {
			if x.Ticket == ticket {
				return
			}
		}
		f.FirstAttempts = append(f.FirstAttempts, floor.FirstAttemptIn{Ticket: ticket, EndedAt: floor.Some(at), ReachedHolding: reached})
	})
}

// secondOpinion is scripted: no model reads the change. It makes a review run
// record and an agreeing review that is bound to the change by the gate's own
// function. Station 05 says "scripted".
func (d *Demo) secondOpinion(ctx context.Context, j job, res *result, subject string) (*gate.BoundReview, error) {
	rid := d.newRunID()
	now := d.now()
	run := &api.Run{ID: rid, Task: api.TaskReview, Attempt: res.attempt, Goal: j.goal, Ticket: j.ticket, Mode: api.ModeUnattended,
		Method: api.MethodBuiltin, MethodUsed: api.MethodBuiltin, Trigger: api.TriggerRef{Kind: api.TriggerFactory, Ref: "#" + fmt.Sprint(max(j.ticket, j.goal))},
		Limits: api.RunLimits{Deadline: "5m"}, Phase: api.PhaseQueued, CreatedAt: now}
	err := d.mutate(ctx, func() error {
		if err := d.st.CreateRun(ctx, run); err != nil {
			return err
		}
		d.led.run(rid, func(r *collect.RunFacts) {
			r.PhaseSince, r.SubjectRun, r.SubjectAttempt, r.Deadline = now, res.runID, res.attempt, 5*time.Minute
		})
		return d.movePhase(ctx, rid, api.PhaseWorking, nil)
	})
	if err != nil {
		return nil, err
	}
	if err := d.pause(ctx, d.cfg.Dwell); err != nil {
		return nil, err
	}
	t := true
	if err := d.mutate(ctx, func() error {
		for _, p := range []api.Phase{api.PhaseVerifying, api.PhasePublishing, api.PhaseSucceeded} {
			if err := d.movePhase(ctx, rid, p, func(r *collect.RunFacts) { r.HasChange = &t }); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	ref := gate.ChangeRef{Ticket: max(j.ticket, j.goal), Attempt: res.attempt, Commit: res.commit, BuilderRun: res.runID}
	rr := gate.ReviewRun{RunID: rid, Task: api.TaskReview, Phase: api.PhaseSucceeded, Ticket: ref.Ticket, Attempt: res.attempt,
		Commit: res.commit, Harness: api.HarnessCodex}
	return gate.BindReview(ref, rr, &api.ReviewFile{SchemaVersion: 1, Subject: subject, Verdict: api.VerdictAgrees})
}

// decide judges a change that passed verify. The acceptance checks run on the
// change and on its parent. The gate decides from measured facts only.
func (d *Demo) decide(ctx context.Context, j job, res *result) (gate.Decision, api.Risk, error) {
	var zero gate.Decision
	gf := d.led.goalFacts(j.goal)
	rev, err := d.secondOpinion(ctx, j, res, gate.SubjectFor(j.task))
	if err != nil {
		return zero, api.Risk{}, err
	}
	opts := acceptance.Options{Check: verify.CheckOptions{Deadline: 30 * time.Second, Environ: d.env}}
	var entries []api.AcceptanceEntry
	working := 0
	if j.task == api.TaskImplement {
		rep, err := acceptance.Run(ctx, d.repo, acceptance.Request{Judged: res.commit, Source: gf.AcceptanceSource, Goal: j.goal, AcceptancePath: api.DefaultAcceptancePath}, opts)
		if err != nil {
			return zero, api.Risk{}, err
		}
		states, err := acceptance.JudgedStates(rep)
		if err != nil {
			return zero, api.Risk{}, err
		}
		if entries, err = acceptance.EvidenceEntries(rep, gf.Baseline); err != nil {
			return zero, api.Risk{}, err
		}
		for _, c := range j.served {
			if states[c] == api.StateWorking {
				working++
			}
		}
	}
	risk := api.Risk{Level: api.RiskLow, Reasons: []string{"the checks are written, not code"}}
	if j.task == api.TaskImplement {
		risk, err = verify.ComputeRisk(verify.RiskInput{Changes: res.changed, RiskPaths: d.factory.Spec.RiskPaths, SizeLimit: 200,
			ReviewOtherHarness: true, CriteriaServed: len(j.served), CriteriaWorking: working,
			Uncovered: verify.UncoveredFiles(res.changed, res.claims)})
		if err != nil {
			return zero, api.Risk{}, err
		}
	}
	ref := gate.ChangeRef{Ticket: max(j.ticket, j.goal), Attempt: res.attempt, Commit: res.commit, BuilderRun: res.runID}
	dec := gate.Decide(gate.Facts{Task: j.task, Change: ref,
		Verify:     gate.VerifyFacts{ProtectedOK: res.protect.OK, ChecksPassed: len(res.failed) == 0},
		Acceptance: entries, Review: rev, ReviewTries: 1, Risk: risk, BuilderHarness: api.HarnessStub, MaxAttempts: maxAttempts})
	return dec, risk, nil
}

// riskReason maps the paths of a high risk change to the one reason the
// Floor can phrase. The first match in a fixed order wins.
func (d *Demo) riskReason(cs *verify.ChangeSet) floor.RiskReason {
	rp := d.factory.Spec.RiskPaths
	for _, p := range cs.Paths() {
		for _, pat := range rp.Contract {
			if verify.MatchFold(pat, p) {
				return floor.RiskContract
			}
		}
		for _, pat := range rp.Security {
			if verify.MatchFold(pat, p) {
				return floor.RiskSecurity
			}
		}
		for _, pat := range rp.Dependencies {
			if verify.MatchFold(pat, p) {
				return floor.RiskDependency
			}
		}
	}
	return floor.RiskTestChange
}

// admit records the gate's answer and, for a pass, adds the change to staging
// and rebuilds it. All of it happens in one change of facts, so no snapshot
// shows a pass that is not yet applied and has no gate fact, or the reverse.
// A fact is written only after the call it describes has succeeded. It says
// what the real code did with the change.
func (d *Demo) admit(ctx context.Context, j job, res *result, dec gate.Decision) (Outcome, error) {
	return d.admitThen(ctx, j, res, dec, nil)
}

// admitThen is admit with facts to write once the holding area has taken the
// change. They are written in the same change of facts.
func (d *Demo) admitThen(ctx context.Context, j job, res *result, dec gate.Decision, after func()) (Outcome, error) {
	ticket := max(j.ticket, j.goal)
	now := d.now()
	gi := floor.GateIn{Ticket: ticket, Since: floor.Some(now), Run: res.runID, Attempt: res.attempt, Commit: res.commit, Machine: machineName}
	switch dec.Outcome {
	case gate.OutcomePass:
		gi.Status = floor.GatePassed
	case gate.OutcomeException:
		gi.Status = floor.GateConsequenceOpen
		gi.Risk = d.riskReason(res.changed)
		return consequenceOutcome(dec.Exception, gi.Risk), d.mutate(ctx, func() error {
			d.led.setGate(gi)
			return nil
		})
	default:
		return "", fmt.Errorf("the gate said %q", dec.Outcome)
	}
	e := staging.Entry{ID: res.runID, Ticket: ticket, Goal: j.goal, Attempt: res.attempt, Task: j.task,
		Commit: res.commit, Parent: d.def, DependsOn: j.deps, Title: j.title}
	out := OutcomeNotApplied
	err := d.mutate(ctx, func() error {
		st, err := d.stg.Add(ctx, e, dec)
		if err != nil {
			return err
		}
		// The change is in the holding area. Now the facts about it are true.
		if j.ticket > 0 {
			d.led.setGate(gi)
		}
		if after != nil {
			after()
		}
		d.observe(st, j)
		for _, a := range st.Applied {
			if a.EntryID == res.runID {
				out = OutcomeAdmitted
			}
		}
		for _, r := range st.Removed {
			if r.EntryID == res.runID {
				out = takenOutOutcome(r.Reason, r.Criteria...)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// observe stamps the ledger from a new staging state: when each change was
// first seen applied or taken out, and the journal. Call it under the write lock.
func (d *Demo) observe(st staging.State, j job) {
	if !st.Published {
		// A state that was recorded but not pushed is not the holding area
		// yet. Nothing in it counts: not an entry, not a removal.
		return
	}
	now := d.now()
	for _, a := range st.Applied {
		if d.led.stampApplied(a.EntryID, now) {
			if a.Ticket == a.Goal { // the checks of a goal
				d.led.journal("checks_written", a.Goal, 0, now)
			} else {
				d.led.journal("entered_holding_area", 0, a.Ticket, now)
				d.firstAttemptApplied(a)
			}
			d.led.with(func(f *collect.LedgerFacts) {
				for i := range f.GoalTimings {
					if f.GoalTimings[i].Goal == a.Goal && !f.GoalTimings[i].FirstHoldingAt.Known && st.Published {
						f.GoalTimings[i].FirstHoldingAt = floor.Some(now)
					}
				}
			})
		}
	}
	for _, r := range st.Removed {
		if d.led.stampRemoval(r.EntryID, r.Ticket, now) {
			kind := floor.JournalKind("taken_out")
			if r.Reason == staging.RemovedUndone {
				kind = "undone"
			}
			d.led.journal(kind, 0, r.Ticket, now)
			d.firstAttempt(r.Ticket, 1, false)
		}
	}
}

func (d *Demo) firstAttemptApplied(a staging.AppliedEntry) {
	d.firstAttempt(a.Ticket, d.attemptOf(a.EntryID), true)
}

func (d *Demo) attemptOf(runID string) int {
	run, err := d.st.GetRun(context.Background(), runID)
	if err != nil {
		return 0
	}
	return run.Attempt
}

func acceptedIn(a staging.AppliedEntry, at time.Time) floor.AcceptedIn {
	return floor.AcceptedIn{Ticket: a.Ticket, Goal: a.Goal, At: floor.Some(at)}
}
