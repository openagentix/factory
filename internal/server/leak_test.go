package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The key must appear in no response body or header, no log line and no
// error text, on any route or error path, whatever the request carries.
func TestKeyNeverLeaves(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Source = stubSource{err: errSecretWith("TOKENPLACEHOLDER")}
	})
	k := e.key()
	// make the source error and the panic text carry the key
	e.s.cfg.Source = stubSource{err: errSecretWith(k)}
	withWriteRoute(t, e)
	e.s.mustHandle(Route{Method: "GET", Path: "/api/v1/boom", Handler: func(w http.ResponseWriter, r *http.Request, _ any) { panic(k) }})

	j := map[string]string{"Content-Type": "application/json"}
	type probe struct {
		name string
		r    req
	}
	var probes []probe
	for _, p := range []string{"/", "/healthz", "/api/v1/floor", "/api/v1/boom", "/api/v1/nope", "/" + k, "/api/v1/" + k, "/api/v1/floor?t=" + k, "/app/main.js?t=" + k, "/%2e%2e/" + k} {
		for _, hdr := range []map[string]string{nil, e.auth(), {"Authorization": "Bearer " + k + "x"}, {"Cookie": "t=" + k}, {"Origin": "https://" + k + ".test"}, {"Referer": "http://127.0.0.1:18765/#t=" + k}} {
			probes = append(probes, probe{"GET " + p, req{path: p, hdr: hdr}})
		}
		probes = append(probes, probe{"bad host " + p, req{path: p, host: k + ".test:18765", hdr: e.auth()}})
	}
	for _, body := range []string{`{"text":"` + k + `"}`, `{"text":"a","text":"` + k + `"}`, `{"` + k + `":1}`, k} {
		probes = append(probes, probe{"POST body", req{method: "POST", path: "/api/v1/test-write", body: body, hdr: merge(e.auth(), j)}})
		probes = append(probes, probe{"POST body bad type", req{method: "POST", path: "/api/v1/test-write", body: body, hdr: merge(e.auth(), map[string]string{"Content-Type": "text/plain"})}})
	}
	for i := 0; i < 15; i++ {
		probes = append(probes, probe{"limited", req{path: "/api/v1/floor", remote: "10.7.7.7:1", hdr: map[string]string{"Authorization": "Bearer " + k + "z"}}})
	}
	for _, m := range []string{"OPTIONS", "TRACE", "PUT", "DELETE"} {
		probes = append(probes, probe{m, req{method: m, path: "/api/v1/floor", hdr: e.auth()}})
	}
	for _, p := range probes {
		rec := e.do(p.r)
		if strings.Contains(rec.Body.String(), k) {
			t.Fatalf("%s: key in body", p.name)
		}
		for hk, hv := range rec.Header() {
			if strings.Contains(hk, k) || strings.Contains(strings.Join(hv, ","), k) {
				t.Fatalf("%s: key in header %s", p.name, hk)
			}
		}
	}
	// the event stream: hello, a journal entry and a floor hint
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	hr := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
	hr.Host, hr.RemoteAddr = "127.0.0.1:18765", "127.0.0.1:1"
	hr.Header.Set("Authorization", "Bearer "+k)
	e.s.Hub().Publish(journal(1))
	rec := httptest.NewRecorder()
	e.s.Handler().ServeHTTP(rec, hr)
	if strings.Contains(rec.Body.String(), k) {
		t.Error("key in the event stream")
	}
	if strings.Contains(e.logs.String(), k) {
		t.Error("key in the log")
	}
	if e.logs.Len() == 0 {
		t.Error("expected the package to log something (the panic, the failed read)")
	}
}

type secretErr string

func (e secretErr) Error() string  { return "read failed: " + string(e) }
func errSecretWith(s string) error { return secretErr(s) }

func (b *syncBuf) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Len()
}

func TestLinkHoldsTheKeyOnlyInTheFragment(t *testing.T) {
	e := newEnv(t)
	l := e.s.Link()
	if !strings.HasPrefix(l, "http://127.0.0.1:18765/#t=") || len(l) != len("http://127.0.0.1:18765/#t=")+43 {
		t.Fatalf("link shape: %d chars", len(l))
	}
	// a fresh key for every server
	e2 := newEnv(t)
	if e.key() == e2.key() {
		t.Fatal("two servers share a key")
	}
	if !bytes.Equal([]byte(e.key()), e.s.key) {
		t.Fatal("link and key differ")
	}
}
