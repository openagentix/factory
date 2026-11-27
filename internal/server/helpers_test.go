package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

const testPort = 18765

// failBound only makes a hung test fail. It is not an expected duration.
// hangBound is for "does not block" checks: orders of magnitude above the
// real time, so only a real block can reach it.
const (
	failBound = 10 * time.Second
	hangBound = 30 * time.Second
)

type stubSource struct {
	body []byte
	err  error
}

func (s stubSource) Floor(ctx context.Context) ([]byte, error) { return s.body, s.err }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

const testIndex = `<!doctype html><html><head><title>t</title></head><body>index page</body></html>`

type testEnv struct {
	s    *Server
	logs *syncBuf
	now  *time.Time
}

func newEnv(t *testing.T, mods ...func(*Config)) *testEnv {
	t.Helper()
	logs := &syncBuf{}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Port: testPort, Mode: "demo",
		Source: stubSource{body: []byte(`{"schemaVersion":1}`)},
		Static: fstest.MapFS{
			"index.html":  {Data: []byte(testIndex)},
			"app/main.js": {Data: []byte("console.log(1)")},
		},
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	env := &testEnv{logs: logs, now: &now}
	cfg.Now = func() time.Time { return *env.now }
	for _, m := range mods {
		m(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env.s = s
	return env
}

// key pulls the key out of the link. Tests never print it.
func (e *testEnv) key() string { return strings.SplitN(e.s.Link(), "#t=", 2)[1] }

func (e *testEnv) auth() map[string]string {
	return map[string]string{"Authorization": "Bearer " + e.key()}
}

type req struct {
	method, path, host, remote, body string
	hdr                              map[string]string
}

func (e *testEnv) do(r req) *httptest.ResponseRecorder {
	if r.method == "" {
		r.method = "GET"
	}
	if r.host == "" {
		r.host = "127.0.0.1:18765"
	}
	if r.remote == "" {
		r.remote = "127.0.0.1:50000"
	}
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	hr.Host = r.host
	hr.RemoteAddr = r.remote
	for k, v := range r.hdr {
		hr.Header.Set(k, v)
	}
	if r.path == "/api/v1/events" {
		// A stream that is let in never ends by itself. Give it a context
		// that is already ended: it writes its start and returns.
		ctx, cancel := context.WithCancel(hr.Context())
		cancel()
		hr = hr.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	e.s.Handler().ServeHTTP(rec, hr)
	return rec
}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

const wantCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func assertSecure(t *testing.T, rec *httptest.ResponseRecorder, api bool, what string) {
	t.Helper()
	h := rec.Header()
	want := map[string]string{
		"Content-Security-Policy":      wantCSP,
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s: header %s = %q, want %q", what, k, got, v)
		}
	}
	if api && h.Get("Cache-Control") != "no-store" {
		t.Errorf("%s: API response lacks Cache-Control: no-store", what)
	}
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("%s: forbidden header %s", what, k)
		}
	}
}

func assertErr(t *testing.T, rec *httptest.ResponseRecorder, status int, code Code, what string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status %d, want %d", what, rec.Code, status)
	}
	if got, want := strings.TrimSpace(rec.Body.String()), `{"error":"`+string(code)+`"}`; got != want {
		t.Errorf("%s: body %q, want %q", what, got, want)
	}
}

var errSecret = errors.New("boom")

func serve(e *testEnv, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.s.Handler().ServeHTTP(rec, r)
	return rec
}
