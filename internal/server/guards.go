package server

import "net/http"

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

// Stub until the key tests are written.
func (s *Server) checkKey(r *http.Request) keyResult { return keyOK }
