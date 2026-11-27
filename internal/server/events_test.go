package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// streamRW is a response writer that hands each Write to a channel, so a
// test can read a stream while the handler is still running.
type streamRW struct {
	h    http.Header
	code int
	ch   chan string
	once sync.Once
}

func newStreamRW() *streamRW { return &streamRW{h: http.Header{}, ch: make(chan string, 4096)} }

func (s *streamRW) Header() http.Header { return s.h }
func (s *streamRW) WriteHeader(c int)   { s.once.Do(func() { s.code = c }) }
func (s *streamRW) Flush()              {}
func (s *streamRW) Write(p []byte) (int, error) {
	s.WriteHeader(200)
	s.ch <- string(p)
	return len(p), nil
}

type stream struct {
	rw     *streamRW
	cancel context.CancelFunc
	done   chan struct{}
}

func (e *testEnv) openStream(t *testing.T, hdr map[string]string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	hr := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
	hr.Host, hr.RemoteAddr = "127.0.0.1:18765", "127.0.0.1:50000"
	for k, v := range merge(e.auth(), hdr) {
		hr.Header[k] = []string{v}
	}
	st := &stream{rw: newStreamRW(), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(st.done)
		e.s.Handler().ServeHTTP(st.rw, hr)
	}()
	t.Cleanup(func() { cancel(); <-st.done })
	return st
}

func (st *stream) next(t *testing.T) string {
	t.Helper()
	select {
	case f := <-st.rw.ch:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("no frame in time")
		return ""
	}
}

func (st *stream) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case f := <-st.rw.ch:
		t.Fatalf("unexpected frame %q", f)
	case <-time.After(d):
	}
}

func (st *stream) ended(t *testing.T) {
	t.Helper()
	select {
	case <-st.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not end")
	}
}

func journal(seq uint64) Event {
	return Event{Kind: KindJournal, ID: seq, Data: []byte(fmt.Sprintf(`{"journalSeq":%d}`, seq))}
}

func TestStreamStartsWithHello(t *testing.T) {
	e := newEnv(t)
	st := e.openStream(t, nil)
	f := st.next(t)
	if f != "event: hello\ndata: {\"journalSeq\":0,\"mode\":\"demo\"}\n\n" {
		t.Fatalf("hello frame %q", f)
	}
	if st.rw.code != 200 || st.rw.h.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream headers: %d %q", st.rw.code, st.rw.h.Get("Content-Type"))
	}
	assertSecureHeader(t, st.rw.h)
}

func assertSecureHeader(t *testing.T, h http.Header) {
	t.Helper()
	rec := httptest.NewRecorder()
	for k, v := range h {
		rec.Header()[k] = v
	}
	assertSecure(t, rec, true, "stream")
}

func TestStreamCarriesJournalFloorAndReset(t *testing.T) {
	e := newEnv(t)
	st := e.openStream(t, nil)
	st.next(t)
	e.s.Hub().Publish(journal(1))
	e.s.Hub().Publish(Event{Kind: KindFloor, ID: 1})
	e.s.Hub().Publish(Event{Kind: KindReset})
	if f := st.next(t); f != "id: 1\nevent: journal\ndata: {\"journalSeq\":1}\n\n" {
		t.Errorf("journal frame %q", f)
	}
	if f := st.next(t); f != "event: floor\ndata: {\"journalSeq\":1}\n\n" {
		t.Errorf("floor frame %q", f)
	}
	if f := st.next(t); f != "event: reset\ndata: {}\n\n" {
		t.Errorf("reset frame %q", f)
	}
}

func TestPublishCannotForgeEvents(t *testing.T) {
	e := newEnv(t)
	st := e.openStream(t, nil)
	st.next(t)
	e.s.Hub().Publish(Event{Kind: KindJournal, ID: 1, Data: []byte("{\n\"a\":1\n}\n\nevent: reset\n")}) // not valid json: dropped
	e.s.Hub().Publish(Event{Kind: KindJournal, ID: 2, Data: []byte("{\r\n\"a\":1\r\n}")})
	e.s.Hub().Publish(Event{Kind: EventKind("evil\nevent: reset"), ID: 3, Data: []byte(`{}`)})
	e.s.Hub().Publish(Event{Kind: KindHello, ID: 4, Data: []byte(`{}`)})
	f := st.next(t)
	if strings.Count(f, "\n\n") != 1 || !strings.HasSuffix(f, "\n\n") || strings.Contains(f, "\r") || strings.Contains(f, "event: reset") {
		t.Errorf("frame %q", f)
	}
	st.none(t, 100*time.Millisecond)
}

func TestResume(t *testing.T) {
	e := newEnv(t)
	for i := uint64(1); i <= 5; i++ {
		e.s.Hub().Publish(journal(i))
	}
	st := e.openStream(t, map[string]string{"Last-Event-ID": "3"})
	if f := st.next(t); !strings.HasPrefix(f, "event: hello") || !strings.Contains(f, `"journalSeq":5`) {
		t.Fatalf("hello %q", f)
	}
	for _, id := range []uint64{4, 5} {
		if f := st.next(t); !strings.HasPrefix(f, fmt.Sprintf("id: %d\n", id)) {
			t.Fatalf("want entry %d, got %q", id, f)
		}
	}
	st.none(t, 50*time.Millisecond)
	// the latest id: nothing to replay
	st2 := e.openStream(t, map[string]string{"Last-Event-ID": "5"})
	st2.next(t)
	st2.none(t, 50*time.Millisecond)
}

func TestResumeResetCases(t *testing.T) {
	e := newEnv(t)
	for i := uint64(1); i <= 1200; i++ {
		e.s.Hub().Publish(journal(i))
	}
	// the window holds 201..1200; 200 is the newest id that is gone but not missed
	resets := []string{"100", "199", "abc", "-1", "1.5", " 5", "5 ", "", "0x10", "+5", "007", "99999999999999999999999", "1201", "5000", "0"}
	for _, id := range resets {
		hr := map[string]string{"Last-Event-ID": id}
		st := e.openStream(t, hr)
		st.next(t) // hello
		if f := st.next(t); f != "event: reset\ndata: {}\n\n" {
			t.Errorf("Last-Event-ID %q: got %q, want reset", id, f)
		}
		st.cancel()
	}
	st := e.openStream(t, map[string]string{"Last-Event-ID": "200"})
	st.next(t)
	if f := st.next(t); !strings.HasPrefix(f, "id: 201\n") {
		t.Errorf("id 200 should resume at 201, got %q", f)
	}
	// two Last-Event-ID headers
	hr := httptest.NewRequest("GET", "/api/v1/events", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hr = hr.WithContext(ctx)
	hr.Host, hr.RemoteAddr = "127.0.0.1:18765", "127.0.0.1:1"
	hr.Header.Set("Authorization", e.auth()["Authorization"])
	hr.Header["Last-Event-ID"] = []string{"1199", "1199"}
	rw := newStreamRW()
	done := make(chan struct{})
	go func() { e.s.Handler().ServeHTTP(rw, hr); close(done) }()
	<-rw.ch
	if f := <-rw.ch; f != "event: reset\ndata: {}\n\n" {
		t.Errorf("two headers: %q", f)
	}
	cancel()
	<-done
}

func TestOnlyEightStreamsAndTheCountRecovers(t *testing.T) {
	e := newEnv(t)
	var open []*stream
	for i := 0; i < 8; i++ {
		st := e.openStream(t, nil)
		st.next(t)
		open = append(open, st)
	}
	rec := e.do(req{path: "/api/v1/events", hdr: e.auth()})
	assertErr(t, rec, 503, CodeUnavailable, "ninth stream")
	assertSecure(t, rec, true, "ninth stream")
	open[0].cancel()
	open[0].ended(t)
	waitFor(t, func() bool { return e.s.Hub().Streams() == 7 })
	st := e.openStream(t, nil)
	if f := st.next(t); !strings.HasPrefix(f, "event: hello") {
		t.Fatalf("after recovery: %q", f)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestPingAndMaxAge(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.PingInterval = 20 * time.Millisecond; c.StreamMaxAge = 150 * time.Millisecond })
	st := e.openStream(t, nil)
	st.next(t)
	if f := st.next(t); f != ": ping\n\n" {
		t.Fatalf("ping %q", f)
	}
	st.ended(t) // closed after the maximum age
	if e.s.Hub().Streams() != 0 {
		t.Errorf("streams left: %d", e.s.Hub().Streams())
	}
}

func TestDefaultsAreFifteenSecondsAndThirtyMinutes(t *testing.T) {
	e := newEnv(t)
	if e.s.cfg.PingInterval != 15*time.Second || e.s.cfg.StreamMaxAge != 30*time.Minute || e.s.cfg.WriteDeadline != 10*time.Second {
		t.Fatalf("defaults: %v %v %v", e.s.cfg.PingInterval, e.s.cfg.StreamMaxAge, e.s.cfg.WriteDeadline)
	}
}

func TestPublishNeverBlocksOnAStuckReader(t *testing.T) {
	h := NewHub("demo")
	sub, err := h.Subscribe(false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	start := time.Now()
	for i := uint64(1); i <= 10000; i++ {
		h.Publish(journal(i))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("10,000 publishes took %v", d)
	}
	select {
	case <-sub.Dropped():
	default:
		t.Error("a reader that never reads was not dropped")
	}
	if h.Streams() != 0 {
		t.Errorf("dropped stream still counted: %d", h.Streams())
	}
}

func TestWindowKeeps1000(t *testing.T) {
	h := NewHub("demo")
	for i := uint64(1); i <= 2500; i++ {
		h.Publish(journal(i))
	}
	if n := h.windowLen(); n != 1000 {
		t.Fatalf("window holds %d", n)
	}
}

func TestOldOrRepeatedIDsAreIgnored(t *testing.T) {
	h := NewHub("demo")
	h.Publish(journal(5))
	h.Publish(journal(5))
	h.Publish(journal(3))
	h.Publish(Event{Kind: KindJournal, ID: 0, Data: []byte(`{}`)})
	if h.windowLen() != 1 || h.Seq() != 5 {
		t.Fatalf("window %d seq %d", h.windowLen(), h.Seq())
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// A client that never reads is dropped, its slot is freed, and publishing
// stays fast. Uses a real loopback connection.
func TestSlowReaderOverTCPIsDropped(t *testing.T) {
	port := freePort(t)
	e := newEnv(t, func(c *Config) { c.Port = port; c.WriteDeadline = 300 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := e.s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- e.s.Serve(ctx, ln) }()
	defer func() { cancel(); <-served }()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(1024)
	}
	fmt.Fprintf(conn, "GET /api/v1/events HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nAuthorization: Bearer %s\r\n\r\n", port, e.key())
	waitFor(t, func() bool { return e.s.Hub().Streams() == 1 })
	big := []byte(`{"pad":"` + strings.Repeat("x", 8000) + `"}`)
	start := time.Now()
	for i := uint64(1); i <= 2000; i++ { // 16 MB, far more than the socket buffers hold
		e.s.Hub().Publish(Event{Kind: KindJournal, ID: i, Data: big})
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("publishing took %v with a stuck reader", d)
	}
	waitFor(t, func() bool { return e.s.Hub().Streams() == 0 })
}

func TestServerContextEndStopsStreamsAndLeavesNoGoroutine(t *testing.T) {
	port := freePort(t)
	e := newEnv(t, func(c *Config) { c.Port = port })
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- e.s.ListenAndServe(ctx) }()
	var conn net.Conn
	var err error
	waitFor(t, func() bool {
		conn, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		return err == nil
	})
	fmt.Fprintf(conn, "GET /api/v1/events HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nAuthorization: Bearer %s\r\n\r\n", port, e.key())
	waitFor(t, func() bool { return e.s.Hub().Streams() == 1 })
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("ListenAndServe: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	conn.Close()
	waitFor(t, func() bool { return runtime.NumGoroutine() <= before })
	if e.s.Hub().Streams() != 0 {
		t.Errorf("streams left: %d", e.s.Hub().Streams())
	}
}
