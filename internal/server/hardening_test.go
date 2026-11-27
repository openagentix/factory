package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// liveSrv is a real server on a loopback port, for tests that need real
// sockets. pre runs before serving, to add routes.
type liveSrv struct {
	s    *Server
	port int
	key  string
	addr string
}

func startLive(t *testing.T, pre func(*Server), mods ...func(*Config)) *liveSrv {
	t.Helper()
	port := freePort(t)
	e := newEnv(t, append([]func(*Config){func(c *Config) { c.Port = port }}, mods...)...)
	if pre != nil {
		pre(e.s)
	}
	ln, err := e.s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(failBound):
			t.Error("server did not stop")
		}
	})
	return &liveSrv{s: e.s, port: port, key: e.key(), addr: fmt.Sprintf("127.0.0.1:%d", port)}
}

func (l *liveSrv) dial(t *testing.T) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp4", l.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// untilClosed reads until the server closes the connection. closed is false
// when only the failure bound ended the wait.
func untilClosed(c net.Conn) (data string, closed bool) {
	_ = c.SetReadDeadline(time.Now().Add(failBound))
	b, err := io.ReadAll(c)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return string(b), false
	}
	return string(b), true
}

// A request the guards refuse must not keep the connection waiting for a
// body the client never sends.
func TestRefusedRequestsCloseTheConnection(t *testing.T) {
	l := startLive(t, func(s *Server) { withWriteRouteOn(t, s) })
	auth := "Authorization: Bearer " + l.key + "\r\n"
	// 429: bring this address to the limit with wrong keys first
	for i := 0; i < 11; i++ {
		c := l.dial(t)
		fmt.Fprintf(c, "GET /api/v1/floor HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer wrong\r\n\r\n", l.addr)
		untilClosed(c)
	}
	cases := []struct{ name, head, status string }{
		{"421", "POST /api/v1/floor HTTP/1.1\r\nHost: evil.test\r\n", "421"},
		{"403", "POST /api/v1/floor HTTP/1.1\r\nHost: " + l.addr + "\r\nOrigin: https://evil.test\r\n", "403"},
		{"429", "POST /api/v1/floor HTTP/1.1\r\nHost: " + l.addr + "\r\nAuthorization: Bearer wrong\r\n", "429"},
		{"405", "PUT /api/v1/floor HTTP/1.1\r\nHost: " + l.addr + "\r\n" + auth, "405"},
		{"415", "POST /api/v1/test-write HTTP/1.1\r\nHost: " + l.addr + "\r\nContent-Type: text/plain\r\n" + auth, "415"},
		{"413", "POST /api/v1/test-write HTTP/1.1\r\nHost: " + l.addr + "\r\nContent-Type: application/json\r\n" + auth, "413"},
		{"401", "POST /api/v1/floor HTTP/1.1\r\nHost: " + l.addr + "\r\n", "401"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := l.dial(t)
			// The body is announced and never sent.
			io.WriteString(conn, c.head+"Content-Length: 100000\r\n\r\n")
			data, closed := untilClosed(conn)
			if !strings.HasPrefix(data, "HTTP/1.1 "+c.status) {
				t.Fatalf("status line %q, want %s", strings.SplitN(data, "\r\n", 2)[0], c.status)
			}
			if !strings.Contains(data, "Connection: close") {
				t.Error("the refusal does not say Connection: close")
			}
			if !closed {
				t.Error("the server kept the connection open and waited for the body")
			}
		})
	}
}

// streamConn reads a raw event stream, keeping what it has read.
type streamConn struct {
	c   net.Conn
	buf string
}

func (s *streamConn) until(t *testing.T, want string) {
	t.Helper()
	_ = s.c.SetReadDeadline(time.Now().Add(failBound))
	b := make([]byte, 4096)
	for !strings.Contains(s.buf, want) {
		n, err := s.c.Read(b)
		s.buf += string(b[:n])
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
	}
	s.buf = s.buf[strings.Index(s.buf, want)+len(want):]
}

func shortTimeouts(c *Config) {
	c.HeaderTimeout = 100 * time.Millisecond
	c.RequestReadTimeout = 100 * time.Millisecond
}

// A request that is let in but never finishes its body is cut off at the
// read deadline. The deadline passing is seen as the server closing the socket.
func TestUnreadBodyIsCutOffAtTheReadDeadline(t *testing.T) {
	l := startLive(t, nil, shortTimeouts)
	c := l.dial(t)
	io.WriteString(c, "GET / HTTP/1.1\r\nHost: "+l.addr+"\r\nContent-Length: 100000\r\n\r\n")
	data, closed := untilClosed(c)
	if !strings.HasPrefix(data, "HTTP/1.1 200") {
		t.Errorf("status line %q", strings.SplitN(data, "\r\n", 2)[0])
	}
	if !closed {
		t.Error("the connection was held open for a body that never came")
	}
}

// The read deadline must not end an event stream, and the stream must still
// notice a client that left.
func TestStreamOutlivesTheReadDeadlineAndNoticesAClientThatLeft(t *testing.T) {
	l := startLive(t, nil, shortTimeouts)
	c := l.dial(t)
	io.WriteString(c, "GET /api/v1/events HTTP/1.1\r\nHost: "+l.addr+"\r\nAuthorization: Bearer "+l.key+"\r\n\r\n")
	sc := &streamConn{c: c}
	sc.until(t, "event: hello")
	// A second connection that started later with the same deadline. When the
	// server closes it, the stream's deadline has passed too.
	c2 := l.dial(t)
	io.WriteString(c2, "GET / HTTP/1.1\r\nHost: "+l.addr+"\r\nContent-Length: 100000\r\n\r\n")
	if _, closed := untilClosed(c2); !closed {
		t.Fatal("the held connection was not cut off")
	}
	l.s.Hub().Publish(journal(1))
	sc.until(t, "event: journal") // still alive
	c.Close()
	waitFor(t, func() bool { return l.s.Hub().Streams() == 0 })
}

// With the cap and the deadlines, a crowd of half-sent requests cannot lock
// out the person who has the key.
func TestHeldConnectionsDoNotLockOutTheKey(t *testing.T) {
	const cap = 16
	l := startLive(t, nil, shortTimeouts, func(c *Config) { c.MaxConns = cap })
	for i := 0; i < 100; i++ {
		c := l.dial(t)
		if i%2 == 0 {
			io.WriteString(c, "GET /x HTT")
		} else {
			io.WriteString(c, "POST /api/v1/floor HTTP/1.1\r\nHost: "+l.addr+"\r\nContent-Length: 100000\r\n\r\n")
		}
	}
	c := l.dial(t)
	io.WriteString(c, "GET /api/v1/floor HTTP/1.1\r\nHost: "+l.addr+"\r\nAuthorization: Bearer "+l.key+"\r\nConnection: close\r\n\r\n")
	data, _ := untilClosed(c)
	if !strings.HasPrefix(data, "HTTP/1.1 200") {
		t.Fatalf("the person's request: %q", strings.SplitN(data, "\r\n", 2)[0])
	}
	if p := l.s.peakConns(); p > cap {
		t.Fatalf("the server held %d connections at once, cap is %d", p, cap)
	}
}

// Beyond the cap a connection is not served until one frees. It is not
// turned away. This is the chosen behaviour.
func TestBeyondTheCapWaitsForAFreeSlot(t *testing.T) {
	l := startLive(t, nil, func(c *Config) { c.MaxConns = 2 })
	c1, c2 := l.dial(t), l.dial(t)
	for _, c := range []net.Conn{c1, c2} { // both slots are in use, idle
		io.WriteString(c, "GET /healthz HTTP/1.1\r\nHost: "+l.addr+"\r\n\r\n")
	}
	sc1, sc2 := &streamConn{c: c1}, &streamConn{c: c2}
	sc1.until(t, `"status":"ok"`)
	sc2.until(t, `"status":"ok"`)
	c3 := l.dial(t)
	io.WriteString(c3, "GET /healthz HTTP/1.1\r\nHost: "+l.addr+"\r\nConnection: close\r\n\r\n")
	c1.Close() // a slot frees
	data, _ := untilClosed(c3)
	if !strings.HasPrefix(data, "HTTP/1.1 200") {
		t.Fatalf("third connection: %q", data)
	}
	if p := l.s.peakConns(); p > 2 {
		t.Fatalf("peak %d, cap 2", p)
	}
}

// A client that never reads its response cannot hold the connection either.
func TestResponseThatIsNeverReadIsCutOff(t *testing.T) {
	big := []byte(`{"pad":"` + strings.Repeat("x", 16<<20) + `"}`)
	l := startLive(t, nil, func(c *Config) {
		c.ResponseTimeout = 200 * time.Millisecond
		c.Source = stubSource{body: big}
	})
	c := l.dial(t)
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(1024)
	}
	io.WriteString(c, "GET /api/v1/floor HTTP/1.1\r\nHost: "+l.addr+"\r\nAuthorization: Bearer "+l.key+"\r\n\r\n")
	// never read
	waitFor(t, func() bool { return l.s.openConns() == 1 }) // it was accepted
	waitFor(t, func() bool { return l.s.openConns() == 0 }) // and then cut off
}

func TestNegativeTimeoutsAreRefused(t *testing.T) {
	for _, f := range []func(*Config){
		func(c *Config) { c.HeaderTimeout = -1 },
		func(c *Config) { c.RequestReadTimeout = -1 },
		func(c *Config) { c.ResponseTimeout = -1 },
		func(c *Config) { c.IdleTimeout = -1 },
		func(c *Config) { c.MaxConns = -1 },
	} {
		c := baseCfg()
		f(&c)
		if _, err := New(c); err == nil {
			t.Error("a negative value was accepted")
		}
	}
}

func TestRoutesCannotBeAddedAfterServingStarts(t *testing.T) {
	l := startLive(t, nil)
	err := l.s.Handle(Route{Method: "GET", Path: "/api/v1/late", Handler: nopAction})
	if err == nil {
		t.Fatal("a route was added while serving")
	}
	c := l.dial(t)
	io.WriteString(c, "GET /api/v1/late HTTP/1.1\r\nHost: "+l.addr+"\r\nAuthorization: Bearer "+l.key+"\r\nConnection: close\r\n\r\n")
	if data, _ := untilClosed(c); !strings.HasPrefix(data, "HTTP/1.1 404") {
		t.Errorf("late route answered %q", strings.SplitN(data, "\r\n", 2)[0])
	}
}

var _ = fmt.Sprint
