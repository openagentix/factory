package staging

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/openagentix/factory/internal/api"
)

// Store keeps what staging must remember: the list of passed changes, every
// recorded state, and the pending Accept. A SQLite implementation is a later
// ticket. MemStore is the one used in tests, and by anything that needs a
// store before that.
//
// Rules every implementation must keep:
//
//   - A change id or a commit that was stored once is never stored again,
//     whether it is live, waiting, removed or accepted. A pass is used once.
//   - SaveState is one atomic step: it numbers the state, stores it with
//     Published false and takes the entries it names as removed out of the
//     live list for good.
//   - SetPending is a compare and swap: it fails with ErrPendingExists when an
//     Accept is pending.
//   - ClearPending with OutcomeAccepted moves the entries of the pending state
//     from live to accepted in the same step.
//   - A method that cannot give a plain answer returns an error. Nothing is
//     assumed.
type Store interface {
	// Entries returns the live entries and the waiting ones, each in the
	// order they were stored.
	Entries(ctx context.Context) (live, waiting []Entry, err error)
	// AddEntry stores an entry, as waiting or live. ErrDuplicate when the id
	// or the commit was stored before.
	AddEntry(ctx context.Context, e Entry, waiting bool) error
	// Promote moves the waiting entries to the end of the live list, in
	// order.
	Promote(ctx context.Context) error

	// SaveState numbers and stores a state (Published false) and removes
	// from the live list the entries named by s.Removed. It returns the
	// sequence number, which is 1 or more and grows.
	SaveState(ctx context.Context, s State) (int64, error)
	// MarkPublished sets Published on a stored state.
	MarkPublished(ctx context.Context, seq int64) error
	// Latest returns the newest stored state, published or not.
	Latest(ctx context.Context) (State, bool, error)
	// LatestPublished returns the newest published state.
	LatestPublished(ctx context.Context) (State, bool, error)

	SetPending(ctx context.Context, p Pending) error
	GetPending(ctx context.Context) (Pending, bool, error)
	ClearPending(ctx context.Context, outcome PendingOutcome) error

	// LastAccepted returns the state of the newest accepted Accept.
	LastAccepted(ctx context.Context) (State, bool, error)
	// IsAccepted reports whether the change was accepted.
	IsAccepted(ctx context.Context, id string) (bool, error)

	// SetStagingPR remembers the number of the staging pull request, and 0
	// forgets it. StagingPR returns it, or 0.
	SetStagingPR(ctx context.Context, n int) error
	StagingPR(ctx context.Context) (int, error)
	// StateByCommit returns the newest published state with this commit.
	StateByCommit(ctx context.Context, commit string) (State, bool, error)
	// MarkAccepted records a published state as accepted without a pending
	// Accept (a developer merged the pull request). Its entries move from
	// live to accepted, as in ClearPending with OutcomeAccepted.
	MarkAccepted(ctx context.Context, seq int64) error
	// SetException records that a pull request was merged at a state nobody
	// accepted, with a fixed text. GetException returns it. ClearException is
	// the explicit step of a person.
	SetException(ctx context.Context, detail string) error
	GetException(ctx context.Context) (string, bool, error)
	ClearException(ctx context.Context) error
}

// MemStore is a Store in memory. It is safe for use from many goroutines.
type MemStore struct {
	mu           sync.Mutex
	live         []Entry
	waiting      []Entry
	usedID       map[string]bool
	usedCommit   map[string]bool
	accepted     map[string]bool
	states       []State
	pending      *Pending
	lastAccepted int // index into states plus one, 0 for none
	stagingPR    int
	exception    string
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{usedID: map[string]bool{}, usedCommit: map[string]bool{}, accepted: map[string]bool{}}
}

func cloneEntry(e Entry) Entry {
	e.DependsOn = append([]int(nil), e.DependsOn...)
	return e
}

func cloneEntries(l []Entry) []Entry {
	out := make([]Entry, len(l))
	for i, e := range l {
		out[i] = cloneEntry(e)
	}
	return out
}

func cloneState(s State) State {
	s.Applied = append([]AppliedEntry(nil), s.Applied...)
	rem := make([]Removal, len(s.Removed))
	for i, r := range s.Removed {
		r.Criteria = append([]int(nil), r.Criteria...)
		rem[i] = r
	}
	s.Removed = rem
	crit := make(map[int]map[int]api.AcceptanceState, len(s.Criteria))
	for g, m := range s.Criteria {
		c := make(map[int]api.AcceptanceState, len(m))
		for id, st := range m {
			c[id] = st
		}
		crit[g] = c
	}
	s.Criteria = crit
	return s
}

func (m *MemStore) Entries(ctx context.Context) ([]Entry, []Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneEntries(m.live), cloneEntries(m.waiting), nil
}

func (m *MemStore) AddEntry(ctx context.Context, e Entry, waiting bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.ID == "" || e.Commit == "" {
		return fmt.Errorf("%w: blank id or commit", ErrBadEntry)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.usedID[e.ID] || m.usedCommit[e.Commit] {
		return ErrDuplicate
	}
	m.usedID[e.ID], m.usedCommit[e.Commit] = true, true
	if waiting {
		m.waiting = append(m.waiting, cloneEntry(e))
	} else {
		m.live = append(m.live, cloneEntry(e))
	}
	return nil
}

func (m *MemStore) Promote(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.live = append(m.live, m.waiting...)
	m.waiting = nil
	return nil
}

func dropIDs(l []Entry, ids map[string]bool) []Entry {
	out := l[:0:0]
	for _, e := range l {
		if !ids[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

func (m *MemStore) SaveState(ctx context.Context, s State) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s = cloneState(s)
	s.Seq = int64(len(m.states) + 1)
	s.Published = false
	for i := range s.Removed {
		s.Removed[i].Seq = s.Seq
	}
	ids := map[string]bool{}
	for _, r := range s.Removed {
		ids[r.EntryID] = true
	}
	m.live = dropIDs(m.live, ids)
	m.states = append(m.states, s)
	return s.Seq, nil
}

func (m *MemStore) MarkPublished(ctx context.Context, seq int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if seq < 1 || seq > int64(len(m.states)) {
		return fmt.Errorf("staging: no state %d", seq)
	}
	m.states[seq-1].Published = true
	return nil
}

func (m *MemStore) Latest(ctx context.Context) (State, bool, error) {
	if err := ctx.Err(); err != nil {
		return State{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.states) == 0 {
		return State{}, false, nil
	}
	return cloneState(m.states[len(m.states)-1]), true, nil
}

func (m *MemStore) LatestPublished(ctx context.Context) (State, bool, error) {
	if err := ctx.Err(); err != nil {
		return State{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.states) - 1; i >= 0; i-- {
		if m.states[i].Published {
			return cloneState(m.states[i]), true, nil
		}
	}
	return State{}, false, nil
}

func (m *MemStore) SetPending(ctx context.Context, p Pending) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		return ErrPendingExists
	}
	m.pending = &p
	return nil
}

func (m *MemStore) GetPending(ctx context.Context) (Pending, bool, error) {
	if err := ctx.Err(); err != nil {
		return Pending{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return Pending{}, false, nil
	}
	return *m.pending, true, nil
}

func (m *MemStore) ClearPending(ctx context.Context, outcome PendingOutcome) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return errors.New("staging: no accept is pending")
	}
	if outcome == OutcomeAccepted {
		if err := m.acceptLocked(m.pending.StateSeq); err != nil {
			return err
		}
	}
	m.pending = nil
	return nil
}

func (m *MemStore) acceptLocked(seq int64) error {
	if seq < 1 || seq > int64(len(m.states)) {
		return fmt.Errorf("staging: no state %d", seq)
	}
	ids := map[string]bool{}
	for _, a := range m.states[seq-1].Applied {
		ids[a.EntryID] = true
		m.accepted[a.EntryID] = true
	}
	m.live = dropIDs(m.live, ids)
	if int(seq) > m.lastAccepted {
		m.lastAccepted = int(seq)
	}
	return nil
}

func (m *MemStore) MarkAccepted(ctx context.Context, seq int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.states[max(seq, 1)-1].Published {
		return fmt.Errorf("staging: state %d is not published", seq)
	}
	return m.acceptLocked(seq)
}

func (m *MemStore) SetStagingPR(ctx context.Context, n int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stagingPR = n
	return nil
}

func (m *MemStore) StagingPR(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stagingPR, nil
}

func (m *MemStore) StateByCommit(ctx context.Context, commit string) (State, bool, error) {
	if err := ctx.Err(); err != nil {
		return State{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.states) - 1; i >= 0; i-- {
		if m.states[i].Published && m.states[i].Commit == commit {
			return cloneState(m.states[i]), true, nil
		}
	}
	return State{}, false, nil
}

func (m *MemStore) SetException(ctx context.Context, detail string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exception = detail
	return nil
}

func (m *MemStore) GetException(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exception, m.exception != "", nil
}

func (m *MemStore) ClearException(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exception = ""
	return nil
}

func (m *MemStore) LastAccepted(ctx context.Context) (State, bool, error) {
	if err := ctx.Err(); err != nil {
		return State{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastAccepted == 0 {
		return State{}, false, nil
	}
	return cloneState(m.states[m.lastAccepted-1]), true, nil
}

func (m *MemStore) IsAccepted(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accepted[id], nil
}
