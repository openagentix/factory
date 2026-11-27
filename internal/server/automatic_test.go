package server

import (
	"io"
	"strings"
	"testing"
)

const marker = "MARKER-9f3a"

// Go's own HTTP code answers some requests before any handler runs: a
// request line or header it cannot parse, an unknown protocol version, a
// transfer encoding it does not know. We do not write our own parser.
// This test pins exactly which answers lack our security headers, and shows
// that none of them repeats request bytes or carries a CORS header. The
// answers that do reach the handler must all have the headers.
func TestAutomaticResponsesArePinned(t *testing.T) {
	l := startLive(t, nil)
	h := l.addr
	cases := []struct {
		name, req string
		status    string
		headers   bool // do we add our headers to this answer
	}{
		{"malformed line", "GARBAGE" + marker + "\r\n\r\n", "400", false},
		{"huge header", "GET / HTTP/1.1\r\nHost: " + h + "\r\nX-" + marker + ": " + strings.Repeat("a", 30000) + "\r\n\r\n", "431", false},
		{"no Host 1.1", "GET /" + marker + " HTTP/1.1\r\n\r\n", "400", false},
		{"no Host 1.0", "GET /api/v1/floor?" + marker + " HTTP/1.0\r\n\r\n", "421", true},
		{"bad version", "GET /" + marker + " HTTP/9.9\r\nHost: " + h + "\r\n\r\n", "505", false},
		{"CONNECT", "CONNECT " + h + " HTTP/1.1\r\nHost: " + h + "\r\n\r\n", "405", true},
		{"two Host", "GET / HTTP/1.1\r\nHost: " + h + "\r\nHost: evil-" + marker + "\r\nConnection: close\r\n\r\n", "400", false},
		{"two Content-Length", "POST /api/v1/x HTTP/1.1\r\nHost: " + h + "\r\nX-" + marker + ": 1\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\nab", "400", false},
		{"bad Transfer-Encoding", "POST /x HTTP/1.1\r\nHost: " + h + "\r\nTransfer-Encoding: gzip-" + marker + "\r\nConnection: close\r\n\r\n", "501", false},
		{"bad header name", "GET / HTTP/1.1\r\nHost: " + h + "\r\nBad " + marker + ": x\r\n\r\n", "400", false},
		{"bare LF", "GET /" + marker + " HTTP/1.1\nHost: " + h + "\nConnection: close\n\n", "200", true},
		{"HTTP/2 preface", "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n", "421", true},
		{"absolute URI, evil host", "GET http://evil.test/" + marker + " HTTP/1.1\r\nHost: " + h + "\r\nConnection: close\r\n\r\n", "421", true},
		{"dot dot", "GET /api/v1/../v1/floor HTTP/1.1\r\nHost: " + h + "\r\nConnection: close\r\n\r\n", "401", true},
		{"OPTIONS star", "OPTIONS * HTTP/1.1\r\nHost: " + h + "\r\nConnection: close\r\n\r\n", "405", true},
		{"OPTIONS star, evil host", "OPTIONS * HTTP/1.1\r\nHost: evil.test\r\nConnection: close\r\n\r\n", "421", true},
		{"OPTIONS star, no Host", "OPTIONS * HTTP/1.1\r\nConnection: close\r\n\r\n", "400", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := l.dial(t)
			io.WriteString(conn, c.req)
			data, _ := untilClosed(conn)
			status := ""
			if f := strings.Fields(strings.SplitN(data, "\r\n", 2)[0]); len(f) > 1 {
				status = f[1]
			}
			hasCSP := strings.Contains(data, "Content-Security-Policy: "+wantCSP)

			if c.status != "" && status != c.status {
				t.Errorf("status %q, want %q", status, c.status)
			}
			if hasCSP != c.headers {
				t.Errorf("our headers present = %v, pinned as %v", hasCSP, c.headers)
			}
			if strings.Contains(data, marker) {
				t.Error("the answer repeats request bytes")
			}
			if strings.Contains(strings.ToLower(data), "access-control-") {
				t.Error("the answer carries a CORS header")
			}
			if strings.Contains(data, l.key) {
				t.Error("key in the answer")
			}
		})
	}
}
