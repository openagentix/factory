package server

import (
	"net"
	"net/http"
	"strings"
)

// hostAllowed: the Host header is exactly 127.0.0.1:<port>, localhost:<port>
// or [::1]:<port>. Anything else, a missing Host or more than one Host is
// refused. This stops a hostile name that resolves to loopback (rebinding).
// The compare is exact and case sensitive: browsers send lower case.
func (s *Server) hostAllowed(r *http.Request) bool {
	if len(r.Header["Host"]) > 1 {
		return false
	}
	return s.hosts[r.Host]
}

// originAllowed: Origin is absent, or exactly one of this server's origins.
// A present but empty, "null" or repeated Origin is refused.
func (s *Server) originAllowed(r *http.Request) bool {
	v := r.Header["Origin"]
	if len(v) == 0 {
		return true
	}
	return len(v) == 1 && s.origins[v[0]]
}

// fetchSiteAllowed: Sec-Fetch-Site is absent, same-origin or none. Anything
// else, including an empty or repeated value, is refused.
func fetchSiteAllowed(r *http.Request) bool {
	v := r.Header["Sec-Fetch-Site"]
	if len(v) == 0 {
		return true
	}
	return len(v) == 1 && (v[0] == "same-origin" || v[0] == "none")
}

type keyResult int

const (
	keyOK keyResult = iota
	keyMissing
	keyWrong
	keyLimited
)

// checkKey reads the key from the Authorization header only. A key in a
// query string or a cookie is never looked at.
//
// The form is strict (a stricter reading than RFC 9110, which makes the
// scheme case-insensitive): exactly "Bearer", one space, then the key. Two
// Authorization headers are refused. A request with no header is not counted
// against the limit. A request with any header that is not the key is.
func (s *Server) checkKey(r *http.Request) keyResult {
	v := r.Header["Authorization"]
	if len(v) == 0 {
		return keyMissing
	}
	if len(v) == 1 && strings.HasPrefix(v[0], bearerPrefix) && keyMatches(s.key, v[0][len(bearerPrefix):]) {
		// The right key is never limited, so nobody can lock the person out.
		return keyOK
	}
	if s.limiter.fail(remoteHost(r.RemoteAddr)) {
		return keyLimited
	}
	return keyWrong
}

const bearerPrefix = "Bearer "

func remoteHost(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}
