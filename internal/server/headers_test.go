package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSecurityHeadersOnEveryKindOfResponse(t *testing.T) {
	e := newEnv(t)
	withWriteRoute(t, e)
	e.s.mustHandle(Route{Method: "GET", Path: "/api/v1/boom", Handler: func(w http.ResponseWriter, r *http.Request, _ any) { panic("secret panic value") }})
	wrong := map[string]string{"Authorization": "Bearer wrong"}
	cases := []struct {
		name string
		r    req
		code int
		api  bool
	}{
		{"200 floor", req{path: "/api/v1/floor", hdr: e.auth()}, 200, true},
		{"200 healthz", req{path: "/healthz"}, 200, false},
		{"200 index", req{path: "/"}, 200, false},
		{"200 static file", req{path: "/app/main.js"}, 200, false},
		{"404 api", req{path: "/api/v1/nope", hdr: e.auth()}, 404, true},
		{"401", req{path: "/api/v1/floor"}, 401, true},
		{"403", req{path: "/api/v1/floor", hdr: map[string]string{"Origin": "https://evil.test"}}, 403, true},
		{"405", req{method: "POST", path: "/api/v1/floor", hdr: e.auth()}, 405, true},
		{"405 static", req{method: "POST", path: "/"}, 405, false},
		{"415", req{method: "POST", path: "/api/v1/test-write", hdr: e.auth()}, 415, true},
		{"421", req{path: "/", host: "evil.test:18765"}, 421, false},
		{"421 api", req{path: "/api/v1/floor", host: "evil.test:18765"}, 421, true},
		{"500 panic", req{path: "/api/v1/boom", hdr: e.auth()}, 500, true},
		{"429", req{path: "/api/v1/floor", remote: "10.5.5.5:1", hdr: wrong}, 429, true},
	}
	// bring 10.5.5.5 to the limit
	for i := 0; i < 10; i++ {
		e.do(req{path: "/api/v1/floor", remote: "10.5.5.5:1", hdr: wrong})
	}
	for _, c := range cases {
		rec := e.do(c.r)
		if rec.Code != c.code {
			t.Errorf("%s: status %d", c.name, rec.Code)
		}
		assertSecure(t, rec, c.api, c.name)
	}
}

func TestSourceErrorBecomesAFixedCode(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Source = stubSource{err: errors.New("token ghp_SECRET1234 at /home/someone/.config")}
	})
	rec := e.do(req{path: "/api/v1/floor", hdr: e.auth()})
	assertErr(t, rec, 500, CodeReadFailed, "source error")
	for _, bad := range []string{"ghp_SECRET", "/home/", e.key()} {
		if strings.Contains(rec.Body.String(), bad) || strings.Contains(e.logs.String(), bad) {
			t.Errorf("source error text leaked (%d chars)", len(bad))
		}
	}
	// invalid JSON from the source is also a fixed code
	e2 := newEnv(t, func(c *Config) { c.Source = stubSource{body: []byte("<html>")} })
	assertErr(t, e2.do(req{path: "/api/v1/floor", hdr: e2.auth()}), 500, CodeReadFailed, "bad bytes")
	// no source
	e3 := newEnv(t, func(c *Config) { c.Source = nil })
	assertErr(t, e3.do(req{path: "/api/v1/floor", hdr: e3.auth()}), 503, CodeUnavailable, "no source")
}

func TestPanicIsAFixedCodeAndNotLogged(t *testing.T) {
	e := newEnv(t)
	e.s.mustHandle(Route{Method: "GET", Path: "/api/v1/boom", Handler: func(w http.ResponseWriter, r *http.Request, _ any) { panic("PANIC-VALUE-" + e.key()) }})
	rec := e.do(req{path: "/api/v1/boom", hdr: e.auth()})
	assertErr(t, rec, 500, CodeInternal, "panic")
	if strings.Contains(e.logs.String(), "PANIC-VALUE") || strings.Contains(e.logs.String(), e.key()) {
		t.Error("panic value reached the log")
	}
	if !strings.Contains(e.logs.String(), "handler panic") {
		t.Error("the panic was not logged at all")
	}
}

// A later route must not be able to take the headers away or add CORS.
func TestRouteCannotRemoveHeadersOrAddCORS(t *testing.T) {
	e := newEnv(t)
	e.s.mustHandle(Route{Method: "GET", Path: "/api/v1/rude", Handler: func(w http.ResponseWriter, r *http.Request, _ any) {
		h := w.Header()
		h.Del("Content-Security-Policy")
		h.Set("Content-Security-Policy", "default-src *")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("access-control-allow-credentials", "true")
		h.Set("Cache-Control", "public")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}})
	assertSecure(t, e.do(req{path: "/api/v1/rude", hdr: e.auth()}), true, "rude route")
}

func TestNoCORSOnAnyAnswerToACrossSiteRequest(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/healthz", "/api/v1/floor", "/api/v1/zzz"} {
		for _, m := range []string{"GET", "OPTIONS", "POST"} {
			rec := e.do(req{method: m, path: p, hdr: merge(e.auth(), map[string]string{"Origin": "https://evil.test", "Sec-Fetch-Site": "cross-site"})})
			assertSecure(t, rec, strings.HasPrefix(p, "/api/"), m+" "+p)
		}
	}
}
