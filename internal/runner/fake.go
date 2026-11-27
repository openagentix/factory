package runner

import (
	"context"
	"sync"

	"github.com/openagentix/factory/internal/api"
)

// Fake is a Runner for tests of other packages. It starts nothing. It
// records each spec, replays Events, and gives Outcome. The zero Outcome
// means failed, so a test sets Status to what it wants.
type Fake struct {
	// Outcome is returned by Wait.
	Outcome Outcome
	// Events are handed out, in order, to Spec.OnEvent and to Events().
	Events []api.Event
	// StartErr, when set, makes Start fail.
	StartErr error
	// Hold keeps the run open until Cancel. The outcome is then failed and
	// cancelled.
	Hold bool

	mu      sync.Mutex
	started []Spec
}

var _ Runner = (*Fake)(nil)

// Started returns the specs that were started, in order.
func (f *Fake) Started() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Spec(nil), f.started...)
}

// Start implements Runner.
func (f *Fake) Start(_ context.Context, spec Spec) (Handle, error) {
	if f.StartErr != nil {
		return nil, f.StartErr
	}
	f.mu.Lock()
	f.started = append(f.started, spec)
	f.mu.Unlock()
	h := &fakeHandle{events: make(chan api.Event, len(f.Events)), done: make(chan struct{}), cancelled: make(chan struct{}), out: f.Outcome}
	for _, e := range f.Events {
		if spec.OnEvent != nil {
			spec.OnEvent(e)
		}
		h.events <- e
	}
	close(h.events)
	h.out.EventCount = len(f.Events)
	if f.Hold {
		go func() {
			<-h.cancelled
			h.out.Cancelled, h.out.Status, h.out.Reason = true, StatusCancelled, "the run was cancelled"
			close(h.done)
		}()
	} else {
		close(h.done)
	}
	return h, nil
}

type fakeHandle struct {
	events    chan api.Event
	done      chan struct{}
	cancelled chan struct{}
	once      sync.Once
	out       Outcome
}

func (h *fakeHandle) Events() <-chan api.Event { return h.events }

func (h *fakeHandle) Wait(ctx context.Context) (Outcome, error) {
	select {
	case <-h.done:
		return h.out, nil
	case <-ctx.Done():
		return Outcome{Status: StatusUnknown, Reason: "the run is still going"}, ctx.Err()
	}
}

func (h *fakeHandle) Cancel(ctx context.Context) error {
	h.once.Do(func() { close(h.cancelled) })
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
