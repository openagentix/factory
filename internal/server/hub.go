package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
)

// EventKind is the kind of a server-sent event. The list is closed.
type EventKind string

// The event kinds. Hello is made by the hub for each new stream and cannot
// be published.
const (
	KindHello   EventKind = "hello"
	KindJournal EventKind = "journal"
	KindFloor   EventKind = "floor"
	KindReset   EventKind = "reset"
)

const (
	windowSize     = 1000     // journal entries kept for resume
	maxStreams     = 8        // open streams
	subQueue       = 64       // frames queued for one stream before it is dropped
	maxJournalData = 16 << 10 // bytes of one journal entry
)

// Event is what a publisher gives the hub.
//
// Journal: ID is the journal sequence, which must grow; Data is the entry as
// JSON. Floor: ID is the journal sequence, a hint to fetch the snapshot
// again; Data is ignored. Reset: no fields. Anything else is dropped.
type Event struct {
	Kind EventKind
	ID   uint64
	Data []byte
}

var errTooManyStreams = errors.New("too many streams")

type entry struct {
	seq   uint64
	frame []byte
}

// Hub fans events out to streams. Publish never blocks: a stream whose queue
// is full is dropped, and its client reconnects.
type Hub struct {
	mode string

	mu      sync.Mutex
	seq     uint64 // newest journal sequence
	evicted uint64 // newest sequence that left the window
	window  []entry
	subs    map[*Subscription]struct{}
}

// NewHub makes a hub. Mode is "demo" or "live" and goes in each hello.
func NewHub(mode string) *Hub {
	return &Hub{mode: mode, subs: map[*Subscription]struct{}{}}
}

// Subscription is one stream's view of the hub.
type Subscription struct {
	hub     *Hub
	ch      chan []byte
	dropped chan struct{}
	initial [][]byte
}

// Initial is what the stream sends first: hello, then the missed entries or a
// reset. It is made under the same lock as the registration, so nothing is
// missed or repeated.
func (s *Subscription) Initial() [][]byte { return s.initial }

// Frames are the later events.
func (s *Subscription) Frames() <-chan []byte { return s.ch }

// Dropped is closed when the hub dropped this stream for being slow.
func (s *Subscription) Dropped() <-chan struct{} { return s.dropped }

// Close frees the stream's slot. It is safe to call more than once.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	delete(s.hub.subs, s)
	s.hub.mu.Unlock()
}

// Subscribe adds a stream. hasLast says a Last-Event-ID header was present;
// last is its value. A value that is not a plain number, is older than the
// window or is newer than the hub's newest entry gets a reset.
func (h *Hub) Subscribe(hasLast bool, last string) (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= maxStreams {
		return nil, errTooManyStreams
	}
	mode, _ := json.Marshal(h.mode)
	hello := frameOf(KindHello, 0, []byte(`{"journalSeq":`+strconv.FormatUint(h.seq, 10)+`,"mode":`+string(mode)+`}`))
	s := &Subscription{hub: h, ch: make(chan []byte, subQueue), dropped: make(chan struct{}), initial: [][]byte{hello}}
	if hasLast {
		id, ok := parseEventID(last)
		if !ok || id > h.seq || id < h.evicted {
			s.initial = append(s.initial, frameOf(KindReset, 0, []byte(`{}`)))
		} else {
			for _, e := range h.window {
				if e.seq > id {
					s.initial = append(s.initial, e.frame)
				}
			}
		}
	}
	h.subs[s] = struct{}{}
	return s, nil
}

// parseEventID accepts plain decimal digits only: no sign, no space, no
// leading zero, no overflow.
func parseEventID(v string) (uint64, bool) {
	if v == "" || len(v) > 19 || (len(v) > 1 && v[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(v, 10, 64)
	return n, err == nil
}

// Publish hands an event to every stream. It never blocks.
func (h *Hub) Publish(ev Event) {
	var f []byte
	switch ev.Kind {
	case KindJournal:
		if ev.ID == 0 || len(ev.Data) > maxJournalData || !json.Valid(ev.Data) {
			return
		}
		var c bytes.Buffer
		if json.Compact(&c, ev.Data) != nil {
			return
		}
		f = frameOf(KindJournal, ev.ID, c.Bytes())
	case KindFloor:
		f = frameOf(KindFloor, 0, []byte(`{"journalSeq":`+strconv.FormatUint(ev.ID, 10)+`}`))
	case KindReset:
		f = frameOf(KindReset, 0, []byte(`{}`))
	default:
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.Kind == KindJournal {
		if ev.ID <= h.seq {
			return
		}
		h.seq = ev.ID
		h.window = append(h.window, entry{ev.ID, f})
		if len(h.window) > windowSize {
			h.evicted = h.window[0].seq
			copy(h.window, h.window[1:])
			h.window = h.window[:windowSize]
		}
	}
	for s := range h.subs {
		select {
		case s.ch <- f:
		default:
			delete(h.subs, s)
			close(s.dropped)
		}
	}
}

// Streams is the number of open streams.
func (h *Hub) Streams() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Seq is the newest journal sequence the hub has seen.
func (h *Hub) Seq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

func (h *Hub) windowLen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.window)
}

// frameOf builds one frame. The data is compact JSON, so it has no newline,
// and the kind is from the closed list, so a frame is always exactly one event.
func frameOf(kind EventKind, id uint64, data []byte) []byte {
	var b bytes.Buffer
	if id != 0 {
		b.WriteString("id: " + strconv.FormatUint(id, 10) + "\n")
	}
	b.WriteString("event: " + string(kind) + "\ndata: ")
	b.Write(data)
	b.WriteString("\n\n")
	return b.Bytes()
}
