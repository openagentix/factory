package server

import (
	"crypto/sha256"
	"sync"
)

// maxIdemKeys is the most Idempotency-Keys kept. Memory only: a restart forgets
// them all (record 0004, "Actions").
const maxIdemKeys = 1024

type idemState int

const (
	idemRunning idemState = iota
	idemDone
	idemFailed
)

// idemRec is what the server keeps of one Idempotency-Key.
type idemRec struct {
	key, route, target string
	sum                [sha256.Size]byte
	state              idemState
	seq                uint64
	status             int
	reply              []byte
	steps              *stepMap
	// detached is set when the reply was 202, so the caller is gone.
	detached bool
}

// stepMap is the publish-step progress of one key. It implements Steps.
type stepMap struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *stepMap) Done(slot string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.m[slot]
	return id, ok && id != ""
}

func (s *stepMap) Mark(slot, hostID string) {
	if slot == "" || hostID == "" {
		return // a step is never done without the host's id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[slot] = hostID
}

// idemKeeper keeps the keys and the targets that have a write running.
type idemKeeper struct {
	mu   sync.Mutex
	max  int
	seq  uint64
	recs map[string]*idemRec
	busy map[string]string // target -> key of the write running on it
}

func newIdemKeeper(max int) *idemKeeper {
	return &idemKeeper{max: max, recs: map[string]*idemRec{}, busy: map[string]string{}}
}

type idemOutcome int

const (
	outRun      idemOutcome = iota // run the write
	outReplay                      // answer with the stored reply
	outBusy                        // 409 in_progress
	outMismatch                    // 422 idempotency_mismatch
	outFull                        // 503 unavailable
)

// begin decides what a press does. For outRun the record is running and its
// target is busy until finish.
func (k *idemKeeper) begin(key, route, target string, sum [sha256.Size]byte) (*idemRec, idemOutcome) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if rec, ok := k.recs[key]; ok {
		switch {
		case rec.route != route || rec.sum != sum:
			return nil, outMismatch
		case rec.state == idemRunning:
			return nil, outBusy
		case rec.state == idemDone:
			return rec, outReplay
		}
		// failed: carry on with the same steps, if the target is free
		if _, busy := k.busy[target]; busy {
			return nil, outBusy
		}
		rec.state, rec.detached = idemRunning, false
		k.busy[target] = key
		return rec, outRun
	}
	if _, busy := k.busy[target]; busy {
		return nil, outBusy
	}
	if len(k.recs) >= k.max && !k.dropOne() {
		return nil, outFull
	}
	k.seq++
	rec := &idemRec{key: key, route: route, target: target, sum: sum, state: idemRunning, seq: k.seq, steps: &stepMap{}}
	k.recs[key] = rec
	k.busy[target] = key
	return rec, outRun
}

// dropOne frees one slot: the oldest done key, else the oldest failed key. A
// running key is never dropped.
func (k *idemKeeper) dropOne() bool {
	var pick *idemRec
	for _, want := range []idemState{idemDone, idemFailed} {
		for _, r := range k.recs {
			if r.state == want && (pick == nil || r.seq < pick.seq) {
				pick = r
			}
		}
		if pick != nil {
			break
		}
	}
	if pick == nil {
		return false
	}
	delete(k.recs, pick.key)
	return true
}

// finish ends a run. A write that was done keeps its reply. A write refused
// before the caller left is forgotten, so the same key is judged again. A
// write that failed, or was refused after the 202 went out, is kept as
// failed. Either way its target is free at once. It reports whether the
// caller is still waiting.
func (k *idemKeeper) finish(rec *idemRec, status int, reply []byte, ok, hostFailed bool) (waiting bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.busy, rec.target)
	switch {
	case ok:
		rec.state, rec.status, rec.reply = idemDone, status, reply
	case hostFailed || rec.detached:
		rec.state = idemFailed
	default:
		delete(k.recs, rec.key)
		rec.state = idemFailed
	}
	return !rec.detached
}

// detach marks that the 202 is going out. It returns false when the write
// finished first, so the caller must use its result.
func (k *idemKeeper) detach(rec *idemRec) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if rec.state != idemRunning {
		return false
	}
	rec.detached = true
	return true
}
