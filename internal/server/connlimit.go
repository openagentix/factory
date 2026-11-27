package server

import (
	"net"
	"sync"
	"sync/atomic"
)

// limitListener caps the open connections. When the cap is reached it does
// not call Accept until one closes. This is a choice (see doc.go): a waiting
// connection stays in the kernel queue and is served in order as soon as a
// held one times out, so the real person's request gets its turn. Accepting
// and closing at once would turn the person away for as long as the
// attacker's connections are held.
type limitListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
	open  atomic.Int64
	peak  atomic.Int64
}

func newLimitListener(l net.Listener, max int) *limitListener {
	return &limitListener{Listener: l, slots: make(chan struct{}, max), done: make(chan struct{})}
}

func (l *limitListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	n := l.open.Add(1)
	for {
		p := l.peak.Load()
		if n <= p || l.peak.CompareAndSwap(p, n) {
			break
		}
	}
	return &limitConn{Conn: c, l: l}, nil
}

func (l *limitListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type limitConn struct {
	net.Conn
	l    *limitListener
	once sync.Once
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.l.open.Add(-1)
		<-c.l.slots
	})
	return err
}

// openConns and peakConns report the server's own count, for tests and
// diagnosis.
func (s *Server) openConns() int64 {
	if l := s.lim.Load(); l != nil {
		return l.open.Load()
	}
	return 0
}

func (s *Server) peakConns() int64 {
	if l := s.lim.Load(); l != nil {
		return l.peak.Load()
	}
	return 0
}
