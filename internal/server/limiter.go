package server

import (
	"sync"
	"time"
)

// limiter counts wrong-key attempts per remote address in a fixed window.
// Its size is capped, so many addresses cannot grow memory without limit.
type limiter struct {
	now      func() time.Time
	max      int
	window   time.Duration
	capacity int

	mu sync.Mutex
	m  map[string]*attempts
}

type attempts struct {
	start time.Time
	n     int
}

func newLimiter(now func() time.Time, max int, window time.Duration, capacity int) *limiter {
	return &limiter{now: now, max: max, window: window, capacity: capacity, m: map[string]*attempts{}}
}

// fail records one wrong key and says whether the address is now over the
// limit: the first max attempts in a window answer false, the next ones true.
func (l *limiter) fail(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a, ok := l.m[addr]
	if ok && now.Sub(a.start) >= l.window {
		ok = false
	}
	if !ok {
		if _, had := l.m[addr]; !had && len(l.m) >= l.capacity {
			l.makeRoom(now)
		}
		a = &attempts{start: now}
		l.m[addr] = a
	}
	a.n++
	return a.n > l.max
}

// makeRoom drops ended windows, then, if still full, an eighth of the
// entries in map order. Dropping many at once keeps the cost per insert low.
func (l *limiter) makeRoom(now time.Time) {
	for k, a := range l.m {
		if now.Sub(a.start) >= l.window {
			delete(l.m, k)
		}
	}
	target := l.capacity - l.capacity/8 - 1
	for k := range l.m {
		if len(l.m) <= target {
			break
		}
		delete(l.m, k)
	}
}

func (l *limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}
