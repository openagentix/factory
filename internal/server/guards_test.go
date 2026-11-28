package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// routes that every guard test must cover: a static page, the health check,
// and every API route.
func guardTargets(e *testEnv) []string {
	out := []string{"/", "/healthz"}
	for _, r := range e.s.Routes() {
		out = append(out, strings.ReplaceAll(r.Path, "{id}", "q-5-9"))
	}
	return out
}

// passStatus is the status a GET gets once it is past every guard: 200 for a
// read route, 405 for a write route, which only answers POST.
// The guard tests build the server with no Actions and no Detail, so the two
// read routes of the actions answer 503, and an id that is no piece id is 404.
func passStatus(target string) int {
	switch {
	case strings.HasPrefix(target, "/api/v1/calls/"), strings.HasPrefix(target, "/api/v1/holding-area/"),
		strings.HasSuffix(target, "/undo"):
		return 405
	case strings.HasSuffix(target, "/undo-preview"):
		return 503
	case strings.HasPrefix(target, "/api/v1/pieces/"):
		return 404
	}
	return 200
}

func TestHostAllowList(t *testing.T) {
	e := newEnv(t)
	p := fmt.Sprint(testPort)
	bad := []string{
		"evil.test:" + p, "localhost.:" + p, "127.0.0.1.nip.io:" + p, "", "LOCALHOST:" + p,
		"Localhost:" + p, "127.0.0.1:" + fmt.Sprint(testPort+1), "127.0.0.1", "localhost", "[::1]",
		"::1:" + p, "[0:0:0:0:0:0:0:1]:" + p, "[::ffff:127.0.0.1]:" + p, "127.1:" + p,
		"2130706433:" + p, "0.0.0.0:" + p, "user@127.0.0.1:" + p, "127.0.0.1:" + p + " ",
		" 127.0.0.1:" + p, "127.0.0.1:" + p + ",evil.test", "[::1]:" + fmt.Sprint(testPort+1),
	}
	for _, target := range guardTargets(e) {
		for _, h := range bad {
			hr := httptest.NewRequest("GET", target, nil)
			hr.Host = h
			if h == "" {
				hr.Header.Del("Host")
			}
			for k, v := range e.auth() {
				hr.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.s.Handler().ServeHTTP(rec, hr)
			assertErr(t, rec, 421, CodeMisdirected, fmt.Sprintf("%s Host %q", target, h))
			assertSecure(t, rec, strings.HasPrefix(target, "/api/"), "421 "+target)
		}
		// two Host headers
		hr := httptest.NewRequest("GET", target, nil)
		hr.Host = "127.0.0.1:" + p
		hr.Header["Host"] = []string{"127.0.0.1:" + p, "evil.test:" + p}
		rec := httptest.NewRecorder()
		e.s.Handler().ServeHTTP(rec, hr)
		assertErr(t, rec, 421, CodeMisdirected, target+" two Host headers")
		for _, good := range []string{"127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p} {
			if rec := e.do(req{path: target, host: good, hdr: e.auth()}); rec.Code != passStatus(target) {
				t.Errorf("%s Host %q: status %d", target, good, rec.Code)
			}
		}
	}
}

func TestOriginMustBeAbsentOrThisHost(t *testing.T) {
	e := newEnv(t)
	p := fmt.Sprint(testPort)
	bad := []string{
		"https://evil.test", "null", "http://127.0.0.1:" + p + "/path", "https://127.0.0.1:" + p,
		"http://127.0.0.1:1", "http://localhost", "http://127.0.0.1", "", "http://LOCALHOST:" + p,
		"http://127.0.0.1:" + p + "/", "http://evil.test:" + p, "http://127.0.0.1.nip.io:" + p,
	}
	for _, target := range guardTargets(e) {
		for _, o := range bad {
			hr := httptest.NewRequest("GET", target, nil)
			hr.Host = "127.0.0.1:" + p
			hr.Header["Origin"] = []string{o}
			for k, v := range e.auth() {
				hr.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.s.Handler().ServeHTTP(rec, hr)
			assertErr(t, rec, 403, CodeForbidden, fmt.Sprintf("%s Origin %q", target, o))
			assertSecure(t, rec, strings.HasPrefix(target, "/api/"), "403 "+target)
		}
		hr := httptest.NewRequest("GET", target, nil)
		hr.Host = "127.0.0.1:" + p
		hr.Header["Origin"] = []string{"http://127.0.0.1:" + p, "http://127.0.0.1:" + p}
		rec := httptest.NewRecorder()
		e.s.Handler().ServeHTTP(rec, hr)
		assertErr(t, rec, 403, CodeForbidden, target+" two Origin headers")
		for _, good := range []string{"http://127.0.0.1:" + p, "http://localhost:" + p, "http://[::1]:" + p} {
			if rec := e.do(req{path: target, hdr: merge(e.auth(), map[string]string{"Origin": good})}); rec.Code != passStatus(target) {
				t.Errorf("%s Origin %q: status %d", target, good, rec.Code)
			}
		}
	}
}

func TestSecFetchSite(t *testing.T) {
	e := newEnv(t)
	for _, target := range guardTargets(e) {
		for _, v := range []string{"cross-site", "same-site", "", "Same-Origin", "none, same-origin", "bogus"} {
			hr := httptest.NewRequest("GET", target, nil)
			hr.Host = "127.0.0.1:18765"
			hr.Header["Sec-Fetch-Site"] = []string{v}
			for k, val := range e.auth() {
				hr.Header.Set(k, val)
			}
			rec := httptest.NewRecorder()
			e.s.Handler().ServeHTTP(rec, hr)
			assertErr(t, rec, 403, CodeForbidden, fmt.Sprintf("%s Sec-Fetch-Site %q", target, v))
		}
		for _, v := range []string{"same-origin", "none"} {
			if rec := e.do(req{path: target, hdr: merge(e.auth(), map[string]string{"Sec-Fetch-Site": v})}); rec.Code != passStatus(target) {
				t.Errorf("%s Sec-Fetch-Site %q: status %d", target, v, rec.Code)
			}
		}
	}
}

func TestGuardOrderHostBeforeOriginBeforeKey(t *testing.T) {
	e := newEnv(t)
	// bad host and bad origin and no key: host wins.
	rec := e.do(req{path: "/api/v1/floor", host: "evil.test:18765", hdr: map[string]string{"Origin": "https://evil.test"}})
	assertErr(t, rec, 421, CodeMisdirected, "host first")
	// good host, bad origin, no key: origin wins over the key check.
	rec = e.do(req{path: "/api/v1/floor", hdr: map[string]string{"Origin": "https://evil.test"}})
	assertErr(t, rec, 403, CodeForbidden, "origin before key")
}
