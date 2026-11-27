package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type writeBody struct {
	Text string         `json:"text"`
	Meta map[string]any `json:"meta"`
}

// withWriteRoute registers a write route through the extension point and
// returns a counter of how often its handler ran.
func withWriteRoute(t *testing.T, e *testEnv) *atomic.Int32 { return withWriteRouteOn(t, e.s) }

func withWriteRouteOn(t *testing.T, s *Server) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	err := s.Handle(Route{
		Method: "POST", Path: "/api/v1/test-write",
		NewBody: func() any { return &writeBody{} },
		Handler: func(w http.ResponseWriter, r *http.Request, body any) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &calls
}

// Every registered route, added through the extension point or built in, is
// behind every guard. A route that skipped a guard fails here.
//
// Limit: this test sees only routes that went through Handle. Code that wraps
// Server.Handler() from outside, for example an http.ServeMux that sends
// some paths elsewhere, adds routes this test cannot see. Handler() is the
// guarded whole; whoever wraps it owns that risk. Handle is the only way in
// that this package offers, and it refuses a route after serving has started.
func TestEveryRouteHasEveryGuard(t *testing.T) {
	e := newEnv(t)
	calls := withWriteRoute(t, e)
	e.s.mustHandle(Route{Method: "PUT", Path: "/api/v1/things/{id}", Handler: func(w http.ResponseWriter, r *http.Request, _ any) {
		calls.Add(1)
	}})
	routes := e.s.Routes()
	if len(routes) < 3 {
		t.Fatalf("expected the built-in routes and the test routes, got %v", routes)
	}
	for _, rt := range routes {
		path := strings.ReplaceAll(rt.Path, "{id}", "t57")
		what := rt.Method + " " + rt.Path
		j := map[string]string{"Content-Type": "application/json"}
		assertErr(t, e.do(req{method: rt.Method, path: path, host: "evil.test:18765", hdr: merge(e.auth(), j)}), 421, CodeMisdirected, what+" host")
		assertErr(t, e.do(req{method: rt.Method, path: path, hdr: merge(e.auth(), j, map[string]string{"Origin": "https://evil.test"})}), 403, CodeForbidden, what+" origin")
		assertErr(t, e.do(req{method: rt.Method, path: path, hdr: merge(e.auth(), j, map[string]string{"Sec-Fetch-Site": "cross-site"})}), 403, CodeForbidden, what+" fetch site")
		assertErr(t, e.do(req{method: rt.Method, path: path, hdr: j}), 401, CodeUnauthorized, what+" no key")
		assertErr(t, e.do(req{method: rt.Method, path: path, remote: "10.9.9.9:1", hdr: merge(j, map[string]string{"Authorization": "Bearer x"})}), 401, CodeUnauthorized, what+" wrong key")
		if rt.Method != "GET" {
			assertErr(t, e.do(req{method: rt.Method, path: path, hdr: merge(e.auth(), map[string]string{"Content-Type": "text/plain"})}), 415, CodeUnsupportedMedia, what+" text/plain")
		}
		assertErr(t, e.do(req{method: "TRACE", path: path, hdr: e.auth()}), 405, CodeMethodNotAllowed, what+" TRACE")
	}
	if calls.Load() != 0 {
		t.Fatalf("a handler ran %d times behind a failed guard", calls.Load())
	}
}

func TestCrossSiteFormPostsNeverReachTheHandler(t *testing.T) {
	e := newEnv(t)
	calls := withWriteRoute(t, e)
	types := []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "text/plain;charset=UTF-8"}
	bodies := []string{`{"text":"a"}`, "text=a", "--x\r\nContent-Disposition: form-data; name=\"text\"\r\n\r\na\r\n--x--\r\n"}
	for _, ct := range types {
		for _, b := range bodies {
			cross := map[string]string{"Content-Type": ct, "Origin": "https://evil.test", "Sec-Fetch-Site": "cross-site"}
			assertErr(t, e.do(req{method: "POST", path: "/api/v1/test-write", body: b, hdr: cross}), 403, CodeForbidden, "cross-site "+ct)
			// a stolen key does not help from another site either
			assertErr(t, e.do(req{method: "POST", path: "/api/v1/test-write", body: b, hdr: merge(cross, e.auth())}), 403, CodeForbidden, "cross-site with key "+ct)
			// same site, key, wrong content type
			same := merge(e.auth(), map[string]string{"Content-Type": ct})
			assertErr(t, e.do(req{method: "POST", path: "/api/v1/test-write", body: b, hdr: same}), 415, CodeUnsupportedMedia, "same-origin "+ct)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("handler ran %d times", calls.Load())
	}
}

func TestContentTypeIsExact(t *testing.T) {
	e := newEnv(t)
	calls := withWriteRoute(t, e)
	for _, ct := range []string{"application/json; charset=utf-8", "Application/JSON", "application/json ", "application/jsonx", "", "application/json, text/plain"} {
		h := e.auth()
		if ct != "" {
			h["Content-Type"] = ct
		}
		assertErr(t, e.do(req{method: "POST", path: "/api/v1/test-write", body: `{}`, hdr: h}), 415, CodeUnsupportedMedia, "ct "+ct)
	}
	hr := httpReq(e, RouteInfo{"POST", "/api/v1/test-write"})
	hr.Body = io.NopCloser(strings.NewReader(`{}`))
	hr.Header.Set("Authorization", e.auth()["Authorization"])
	hr.Header["Content-Type"] = []string{"application/json", "application/json"}
	assertErr(t, serve(e, hr), 415, CodeUnsupportedMedia, "two content types")
	if calls.Load() != 0 {
		t.Fatalf("handler ran %d times", calls.Load())
	}
	h := merge(e.auth(), map[string]string{"Content-Type": "application/json"})
	if rec := e.do(req{method: "POST", path: "/api/v1/test-write", body: `{"text":"a"}`, hdr: h}); rec.Code != 200 {
		t.Fatalf("good write: %d", rec.Code)
	}
}

type countReader struct {
	n, total int
}

func (c *countReader) Read(p []byte) (int, error) {
	if c.n >= c.total {
		return 0, io.EOF
	}
	k := len(p)
	if k > c.total-c.n {
		k = c.total - c.n
	}
	for i := 0; i < k; i++ {
		p[i] = 'a'
	}
	c.n += k
	return k, nil
}

func jsonOfSize(n int) string {
	pad := n - len(`{"text":""}`)
	return `{"text":"` + strings.Repeat("a", pad) + `"}`
}

func TestBodyLimit(t *testing.T) {
	e := newEnv(t)
	calls := withWriteRoute(t, e)
	h := merge(e.auth(), map[string]string{"Content-Type": "application/json"})
	if rec := e.do(req{method: "POST", path: "/api/v1/test-write", body: jsonOfSize(16384), hdr: h}); rec.Code != 200 {
		t.Fatalf("16 KiB exactly: %d", rec.Code)
	}
	assertErr(t, e.do(req{method: "POST", path: "/api/v1/test-write", body: jsonOfSize(16385), hdr: h}), 413, CodePayloadTooLarge, "16 KiB + 1")
	// a big body of unknown length: the server stops reading near the cap
	cr := &countReader{total: 50 << 20}
	hr := httptest.NewRequest("POST", "/api/v1/test-write", cr)
	hr.ContentLength = -1
	hr.Host, hr.RemoteAddr = "127.0.0.1:18765", "127.0.0.1:1"
	for k, v := range h {
		hr.Header.Set(k, v)
	}
	assertErr(t, serve(e, hr), 413, CodePayloadTooLarge, "50 MiB stream")
	if cr.n > 16384+8192 {
		t.Errorf("read %d bytes of an oversized body", cr.n)
	}
	// a declared length over the cap is refused without reading anything
	cr2 := &countReader{total: 50 << 20}
	hr = httptest.NewRequest("POST", "/api/v1/test-write", cr2)
	hr.ContentLength = 50 << 20
	hr.Host, hr.RemoteAddr = "127.0.0.1:18765", "127.0.0.1:1"
	for k, v := range h {
		hr.Header.Set(k, v)
	}
	assertErr(t, serve(e, hr), 413, CodePayloadTooLarge, "declared 50 MiB")
	if cr2.n != 0 {
		t.Errorf("read %d bytes of a body that declared itself too large", cr2.n)
	}
	if calls.Load() != 1 {
		t.Errorf("handler ran %d times, want 1", calls.Load())
	}
}

func TestStrictJSON(t *testing.T) {
	e := newEnv(t)
	calls := withWriteRoute(t, e)
	h := merge(e.auth(), map[string]string{"Content-Type": "application/json"})
	bad := map[string]string{
		"duplicate key":           `{"text":"a","text":"b"}`,
		"duplicate key, case":     `{"Text":"a","text":"b"}`,
		"duplicate nested":        `{"meta":{"x":1,"x":2}}`,
		"duplicate in array":      `{"meta":{"l":[{"x":1,"x":2}]}}`,
		"unknown field":           `{"text":"a","admin":true}`,
		"trailing object":         `{"text":"a"} {}`,
		"trailing junk":           `{"text":"a"}x`,
		"top level array":         `[]`,
		"top level null":          `null`,
		"top level string":        `"a"`,
		"wrong type":              `{"text":1}`,
		"not json":                `hello`,
		"unterminated":            `{"text":`,
		"too deep":                `{"meta":` + strings.Repeat(`{"a":`, 100) + `1` + strings.Repeat(`}`, 100) + `}`,
		"two documents":           `{"text":"a"}{"text":"b"}`,
		"unknown field in nested": `{"meta":{"a":1},"zzz":1}`,
	}
	for name, body := range bad {
		assertErr(t, e.do(req{method: "POST", path: "/api/v1/test-write", body: body, hdr: h}), 400, CodeBadRequest, name)
	}
	good := []string{`{"text":"a"}`, " {\"text\":\"a\"}\n", `{}`, `{"meta":{"a":[1,2,{"b":null}]}}`, ""}
	for _, body := range good {
		if rec := e.do(req{method: "POST", path: "/api/v1/test-write", body: body, hdr: h}); rec.Code != 200 {
			t.Errorf("good body %q: %d", body, rec.Code)
		}
	}
	if calls.Load() != int32(len(good)) {
		t.Errorf("handler ran %d times, want %d", calls.Load(), len(good))
	}
}

func TestMethodAllowListAndOptions(t *testing.T) {
	e := newEnv(t)
	withWriteRoute(t, e)
	j := map[string]string{"Content-Type": "application/json"}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD", "TRACE", "CONNECT", "FOO"} {
		rec := e.do(req{method: m, path: "/api/v1/floor", hdr: merge(e.auth(), j)})
		assertErr(t, rec, 405, CodeMethodNotAllowed, m+" floor")
		if rec.Header().Get("Allow") != "GET" {
			t.Errorf("%s: Allow = %q", m, rec.Header().Get("Allow"))
		}
	}
	assertErr(t, e.do(req{method: "GET", path: "/api/v1/test-write", hdr: e.auth()}), 405, CodeMethodNotAllowed, "GET on write route")
	// a preflight from another site grants nothing and is refused before the key
	pre := map[string]string{"Origin": "https://evil.test", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,content-type", "Sec-Fetch-Site": "cross-site"}
	rec := e.do(req{method: "OPTIONS", path: "/api/v1/test-write", hdr: pre})
	assertErr(t, rec, 403, CodeForbidden, "cross-site preflight")
	assertSecure(t, rec, true, "preflight")
	// a same-origin OPTIONS without a key is 401, with a key 405, never a grant
	rec = e.do(req{method: "OPTIONS", path: "/api/v1/test-write", hdr: map[string]string{"Access-Control-Request-Method": "POST"}})
	assertErr(t, rec, 401, CodeUnauthorized, "OPTIONS no key")
	rec = e.do(req{method: "OPTIONS", path: "/healthz"})
	assertErr(t, rec, 405, CodeMethodNotAllowed, "OPTIONS healthz")
	rec = e.do(req{method: "POST", path: "/", body: "x", hdr: j})
	assertErr(t, rec, 405, CodeMethodNotAllowed, "POST static")
	// 405 comes after the key check
	assertErr(t, e.do(req{method: "POST", path: "/api/v1/floor", hdr: j}), 401, CodeUnauthorized, "POST floor no key")
}

func TestPathValuesAreStrictIDs(t *testing.T) {
	e := newEnv(t)
	var got atomic.Value
	e.s.mustHandle(Route{Method: "GET", Path: "/api/v1/pieces/{id}", Handler: func(w http.ResponseWriter, r *http.Request, _ any) {
		got.Store(r.PathValue("id"))
		_, _ = w.Write([]byte(`{}`))
	}})
	if rec := e.do(req{path: "/api/v1/pieces/t57", hdr: e.auth()}); rec.Code != 200 || got.Load() != "t57" {
		t.Fatalf("good id: %d %v", rec.Code, got.Load())
	}
	for _, id := range []string{"..", ".", "%2e%2e", "a%2fb", "a.b", strings.Repeat("a", 65), "%C3%A9", "-x", "a%00", "a%20b", "a;b"} {
		assertErr(t, e.do(req{path: "/api/v1/pieces/" + id, hdr: e.auth()}), 404, CodeNotFound, "id "+id)
	}
	assertErr(t, e.do(req{path: "/api/v1/pieces//t57", hdr: e.auth()}), 404, CodeNotFound, "double slash")
	assertErr(t, e.do(req{path: "/api/v1/pieces/t57/", hdr: e.auth()}), 404, CodeNotFound, "trailing slash")
}

func TestHandleRefusesBadRoutes(t *testing.T) {
	e := newEnv(t)
	ok := func(w http.ResponseWriter, r *http.Request, _ any) {}
	cases := []Route{
		{Method: "GET", Path: "/healthz2", Handler: ok},
		{Method: "GET", Path: "/api/v1/", Handler: ok},
		{Method: "GET", Path: "/api/v2/x", Handler: ok},
		{Method: "GET", Path: "/api/v1/a//b", Handler: ok},
		{Method: "GET", Path: "/api/v1/A", Handler: ok},
		{Method: "GET", Path: "/api/v1/{ID}", Handler: ok},
		{Method: "GET", Path: "/api/v1/floor", Handler: ok}, // duplicate
		{Method: "TRACE", Path: "/api/v1/x", Handler: ok},
		{Method: "", Path: "/api/v1/x", Handler: ok},
		{Method: "GET", Path: "/api/v1/x"},
		{Method: "GET", Path: "/api/v1/x", Handler: ok, NewBody: func() any { return nil }},
		// wildcard and pattern forms that net/http's mux would take
		{Method: "GET", Path: "/api/v1/*", Handler: ok},
		{Method: "GET", Path: "/api/v1/x/*", Handler: ok},
		{Method: "GET", Path: "/api/v1/{id...}", Handler: ok},
		{Method: "GET", Path: "/api/v1/{$}", Handler: ok},
		{Method: "GET", Path: "/api/v1/x/", Handler: ok},
		{Method: "GET", Path: "GET /api/v1/x", Handler: ok},
		{Method: "GET", Path: "/api/v1/x{id}", Handler: ok},
		{Method: "GET", Path: "/*", Handler: ok},
		{Method: "GET", Path: "/", Handler: ok},
		{Method: "GET", Path: "", Handler: ok},
	}
	for i, c := range cases {
		if err := e.s.Handle(c); err == nil {
			t.Errorf("case %d (%s %s) was accepted", i, c.Method, c.Path)
		}
	}
	_ = fmt.Sprint()
}

func nopAction(w http.ResponseWriter, r *http.Request, _ any) {}
