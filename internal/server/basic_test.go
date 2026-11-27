package server

import (
	"strings"
	"testing"
)

func TestHealthzNeedsNoKeyAndHasFixedBody(t *testing.T) {
	e := newEnv(t)
	rec := e.do(req{path: "/healthz"})
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("healthz: %d %q", rec.Code, rec.Body.String())
	}
}

func TestFloorWritesSourceBytes(t *testing.T) {
	e := newEnv(t)
	rec := e.do(req{path: "/api/v1/floor", hdr: e.auth()})
	if rec.Code != 200 || rec.Body.String() != `{"schemaVersion":1}` {
		t.Fatalf("floor: %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q", ct)
	}
}

func TestStaticAndSinglePageFallback(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/index.html", "/goals", "/goals/57/x", "/nope.css"} {
		rec := e.do(req{path: p})
		if rec.Code != 200 || rec.Body.String() != testIndex {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	rec := e.do(req{path: "/app/main.js"})
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Errorf("main.js: %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("main.js content type %q", ct)
	}
}

func TestUnknownAPIPathIsJSON404NotThePage(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/v1/nope", "/api/", "/api", "/api/v1/floor/", "/api/v2/floor"} {
		assertErr(t, e.do(req{path: p, hdr: e.auth()}), 404, CodeNotFound, p)
	}
}
