package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/server"
)

// An action is decided and its host writes are made in one step under the
// write lock. The slow work that follows (the agent, the checks, the holding
// area) cannot be done there, so the action queues it. Work and Drain do it,
// one task at a time, under the same step lock as the story.

type fuKind int

const (
	fuResume fuKind = iota // run an already queued try (after an answer)
	fuRework               // make the next try of a change that was sent back
	fuAdd                  // add a change that got a yes to the holding area
	fuChecks               // write the checks of a goal
	fuTicket               // build a ticket of a goal
)

type followup struct {
	kind fuKind
	j    job
	// runID and attempt name the queued try of fuResume.
	runID   string
	attempt int
	// res and dec are what the gate held, for fuAdd.
	res *result
	dec gate.Decision
}

// sealedChange is what the gate held for a person: the sealed decision, and
// the job and result it is about. It is kept in memory (record 0004).
type sealedChange struct {
	dec gate.Decision
	j   job
	res *result
}

// enqueue adds a task. The caller may hold the write lock.
func (d *Demo) enqueue(f followup) {
	d.fuMu.Lock()
	d.fu = append(d.fu, f)
	d.fuMu.Unlock()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Demo) dequeue() (followup, bool) {
	d.fuMu.Lock()
	defer d.fuMu.Unlock()
	if len(d.fu) == 0 {
		return followup{}, false
	}
	f := d.fu[0]
	d.fu = d.fu[1:]
	return f, true
}

// Failed says whether a task that follows an action failed for a real reason.
// A stop the person asked for is not a failure.
func (d *Demo) Failed() bool { return d.failed.Load() }

func (d *Demo) report(format string, a ...any) {
	if d.cfg.Report != nil {
		d.cfg.Report("DEMO " + fmt.Sprintf(format, a...))
	}
}

// Work does the queued tasks as they come, until ctx ends. It is the loop the
// command starts beside the server.
func (d *Demo) Work(ctx context.Context) error {
	for {
		if err := d.Drain(ctx); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.wake:
		case <-time.After(2 * time.Second):
		}
	}
}

// Drain does every queued task now, one at a time. A task that fails is
// reported and the others still run. It returns the failures joined.
func (d *Demo) Drain(ctx context.Context) error {
	var errs []error
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		// The step lock is taken before a task is taken from the queue, so a
		// moment with the queue empty and the lock free means no task is in flight.
		d.stepMu.Lock()
		f, ok := d.dequeue()
		if !ok {
			d.stepMu.Unlock()
			return errors.Join(errs...)
		}
		err := d.runFollowup(ctx, f)
		if err != nil {
			d.unqueue(f) // a task that did not finish may be asked for again
		}
		d.stepMu.Unlock()
		if err != nil {
			// A stop the person asked for ends the task, and is not a failure.
			if ctx.Err() == nil && !d.closing.Load() {
				d.failed.Store(true)
				d.report("a step after an action did not finish")
			}
			if d.taskErr != nil {
				d.taskErr(err)
			}
			errs = append(errs, err)
		}
	}
}

// runFollowup does one task. The caller holds the step lock.
func (d *Demo) runFollowup(ctx context.Context, f followup) error {
	if d.closing.Load() {
		return server.ErrUnavailable
	}
	if d.taskHook != nil {
		d.taskHook()
	}
	switch f.kind {
	case fuResume:
		res, err := d.runJob(ctx, f.j, f.runID, f.attempt)
		if err != nil {
			return err
		}
		_, err = d.finish(ctx, f.j, res)
		return err
	case fuRework:
		res, err := d.work(ctx, f.j)
		if err != nil {
			return err
		}
		_, err = d.finish(ctx, f.j, res)
		return err
	case fuTicket:
		res, err := d.work(ctx, f.j)
		if err != nil {
			return err
		}
		_, err = d.finish(ctx, f.j, res)
		return err
	case fuChecks:
		return d.writeChecks(ctx, f.j.goal)
	case fuAdd:
		return d.addAnswered(ctx, f)
	}
	return fmt.Errorf("demo: unknown follow-up %d", f.kind)
}

// addAnswered adds a change a person said yes to into the holding area, with
// the decision the gate gave back. When this fails, the change is shown as
// stuck: a yes that did not lead anywhere is never shown as done.
func (d *Demo) addAnswered(ctx context.Context, f followup) error {
	var err error
	if d.addHook != nil {
		err = d.addHook()
	}
	if err == nil {
		_, err = d.admit(ctx, f.j, f.res, f.dec)
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return err // a stop, not a failure: the change is not marked
	}
	// The real step failed after the person was told "done". Say so.
	ticket := f.j.ticket
	_ = d.mutate(context.WithoutCancel(ctx), func() error {
		d.led.setGate(floor.GateIn{Ticket: ticket, Status: floor.GateException, Code: floor.Unknown, Since: floor.Some(d.now()),
			Run: f.res.runID, Attempt: f.res.attempt, Commit: f.res.commit, Machine: machineName})
		return nil
	})
	return err
}

// unqueue forgets that a build was queued, so that a later approval may queue it
// again. The caller holds the step lock.
func (d *Demo) unqueue(f followup) {
	switch f.kind {
	case fuChecks:
		delete(d.queued, checksKey(f.j.goal))
	case fuTicket:
		delete(d.queued, ticketKey(f.j.key))
	}
}

// followupJob builds the job of the next try of a ticket.
func (d *Demo) nextTry(key string, try int, trigger []byte) (job, error) {
	j, err := d.ticketJob(key, try)
	if err != nil {
		return job{}, err
	}
	j.trigger = trigger
	return j, nil
}

var _ = api.TaskImplement
var _ = collect.RunFacts{}
