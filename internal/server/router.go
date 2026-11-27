package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// ActionFunc handles one guarded route. For a write route, body is the
// value made by Route.NewBody, already decoded strictly. For a read route it
// is nil.
type ActionFunc func(w http.ResponseWriter, r *http.Request, body any)

// Route is one API route. This is the only way to add a route, and every
// route added here is behind all the guards (see Server.serve).
type Route struct {
	// Method is GET, POST, PUT, PATCH or DELETE.
	Method string
	// Path starts with /api/v1/. A segment {name} matches one strict id and
	// is read with r.PathValue("name").
	Path string
	// NewBody makes the value a write body is decoded into. Unknown fields
	// are refused. With nil, a write may only send an empty body or {}.
	NewBody func() any
	Handler ActionFunc
}

// RouteInfo describes a registered route, for tests and listings.
type RouteInfo struct{ Method, Path string }

type route struct {
	method  string
	pattern string
	segs    []string
	newBody func() any
	handler ActionFunc
	write   bool
}

var (
	literalSeg = regexp.MustCompile(`^[a-z0-9-]+$`)
	paramSeg   = regexp.MustCompile(`^\{[a-z]+\}$`)
	// A strict id: no dots, no slashes, no unicode.
	idValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// Handle adds a route behind the guards. It is the extension point for the
// action routes.
func (s *Server) Handle(rt Route) error {
	switch rt.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return errors.New("server: route method not allowed")
	}
	if rt.Handler == nil {
		return errors.New("server: route has no handler")
	}
	if rt.Method == http.MethodGet && rt.NewBody != nil {
		return errors.New("server: a read route takes no body")
	}
	if !strings.HasPrefix(rt.Path, "/api/v1/") {
		return errors.New("server: route path must start with /api/v1/")
	}
	segs := strings.Split(strings.TrimPrefix(rt.Path, "/"), "/")
	for _, sg := range segs {
		if !literalSeg.MatchString(sg) && !paramSeg.MatchString(sg) {
			return errors.New("server: bad route segment")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Routes are fixed before serving starts: a late route is refused.
	if s.started.Load() {
		return errors.New("server: routes cannot be added after serving has started")
	}
	for _, e := range s.routes {
		if e.method == rt.Method && e.pattern == rt.Path {
			return errors.New("server: route already registered")
		}
	}
	s.routes = append(s.routes, &route{
		method: rt.Method, pattern: rt.Path, segs: segs,
		newBody: rt.NewBody, handler: rt.Handler, write: rt.Method != http.MethodGet,
	})
	return nil
}

// Routes lists every registered route.
func (s *Server) Routes() []RouteInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RouteInfo, 0, len(s.routes))
	for _, e := range s.routes {
		out = append(out, RouteInfo{e.method, e.pattern})
	}
	return out
}

// match finds the route for the request. When the path exists but the method
// does not, it returns nil and the allowed methods.
func (s *Server) match(r *http.Request) (*route, []string) {
	if strings.ContainsAny(r.URL.Path, "\\\x00") || strings.Contains(r.URL.Path, "//") {
		return nil, nil
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	s.mu.RLock()
	defer s.mu.RUnlock()
	var allow []string
	for _, e := range s.routes {
		vals, ok := e.matchSegs(parts)
		if !ok {
			continue
		}
		if e.method == r.Method {
			for k, v := range vals {
				r.SetPathValue(k, v)
			}
			return e, nil
		}
		allow = append(allow, e.method)
	}
	return nil, allow
}

func (e *route) matchSegs(parts []string) (map[string]string, bool) {
	if len(parts) != len(e.segs) {
		return nil, false
	}
	var vals map[string]string
	for i, sg := range e.segs {
		if paramSeg.MatchString(sg) {
			if !idValue.MatchString(parts[i]) {
				return nil, false
			}
			if vals == nil {
				vals = map[string]string{}
			}
			vals[sg[1:len(sg)-1]] = parts[i]
			continue
		}
		if sg != parts[i] {
			return nil, false
		}
	}
	return vals, true
}
