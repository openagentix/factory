package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// apiTargets: every registered API route, with a method that matches it.
func apiTargets(e *testEnv) []RouteInfo { return e.s.Routes() }

func TestAPIWithoutOrWithWrongKeyIs401(t *testing.T) {
	e := newEnv(t)
	for _, rt := range apiTargets(e) {
		what := rt.Method + " " + rt.Path
		assertErr(t, e.do(req{method: rt.Method, path: rt.Path}), 401, CodeUnauthorized, what+" no key")
		assertErr(t, e.do(req{method: rt.Method, path: rt.Path, hdr: map[string]string{"Authorization": "Bearer nope", "Content-Type": "application/json"}}), 401, CodeUnauthorized, what+" wrong key")
	}
	// unknown API paths do not reveal that they are unknown
	assertErr(t, e.do(req{path: "/api/v1/zzz"}), 401, CodeUnauthorized, "unknown path no key")
	assertErr(t, e.do(req{path: "/api/"}), 401, CodeUnauthorized, "/api/ no key")
	assertErr(t, e.do(req{path: "/api"}), 401, CodeUnauthorized, "/api no key")
}

func TestKeyInQueryOrCookieIsNotAccepted(t *testing.T) {
	e := newEnv(t)
	k := e.key()
	for _, rt := range apiTargets(e) {
		for _, q := range []string{"?t=" + k, "?token=" + k, "?key=" + k, "?access_token=" + k, "?Authorization=Bearer+" + k} {
			assertErr(t, e.do(req{method: rt.Method, path: rt.Path + q}), 401, CodeUnauthorized, rt.Path+" query")
		}
		for _, c := range []string{"t=" + k, "token=" + k, "key=" + k, "session=" + k} {
			assertErr(t, e.do(req{method: rt.Method, path: rt.Path, hdr: map[string]string{"Cookie": c}}), 401, CodeUnauthorized, rt.Path+" cookie")
		}
	}
}

// Ruling: the scheme must be exactly "Bearer", one space, then the key.
// RFC 9110 says the scheme is case-insensitive, but our own page always
// sends this form, so the stricter reading costs nothing.
func TestBearerFormIsStrict(t *testing.T) {
	e := newEnv(t)
	k := e.key()
	bad := []string{"bearer " + k, "BEARER " + k, "Bearer  " + k, " Bearer " + k, "Bearer " + k + " ", "Bearer\t" + k, "Basic " + k, "Bearer", "Bearer ", k, "Token " + k, "Bearer " + k + "x", "Bearer " + k[:len(k)-1]}
	n := 0
	for _, rt := range apiTargets(e) {
		for _, v := range bad {
			n++
			hr := map[string]string{"Authorization": v, "Content-Type": "application/json"}
			rec := e.do(req{method: rt.Method, path: rt.Path, hdr: hr, remote: fmt.Sprintf("10.1.0.%d:1", n)})
			if rec.Code != 401 {
				t.Errorf("%s: Authorization form of length %d got %d, want 401", rt.Path, len(v), rec.Code)
			}
		}
		// two Authorization headers: refused even when one is right
		hr := httpReq(e, rt)
		hr.RemoteAddr = "10.2.0.1:1"
		hr.Header["Authorization"] = []string{"Bearer " + k, "Bearer " + k}
		if rec := serve(e, hr); rec.Code != 401 {
			t.Errorf("%s: two Authorization headers got %d, want 401", rt.Path, rec.Code)
		}
	}
	if rec := e.do(req{path: "/api/v1/floor", hdr: e.auth()}); rec.Code != 200 {
		t.Fatalf("right key got %d", rec.Code)
	}
}

func TestOnlyWrongKeysAreCountedAndLimitIsPerAddress(t *testing.T) {
	e := newEnv(t)
	wrong := map[string]string{"Authorization": "Bearer wrong"}
	a, b := "127.0.0.1:40001", "127.0.0.1:40002" // same address, other port
	other := "127.0.0.2:40001"
	// no key, bad host, bad origin: none of these count
	for i := 0; i < 30; i++ {
		e.do(req{path: "/api/v1/floor", remote: a})
		e.do(req{path: "/api/v1/floor", remote: a, host: "evil.test:18765", hdr: wrong})
		e.do(req{path: "/api/v1/floor", remote: a, hdr: merge(wrong, map[string]string{"Origin": "https://evil.test"})})
		e.do(req{path: "/api/v1/floor", remote: a, hdr: merge(wrong, map[string]string{"Sec-Fetch-Site": "cross-site"})})
	}
	for i := 1; i <= 10; i++ {
		if rec := e.do(req{path: "/api/v1/floor", remote: a, hdr: wrong}); rec.Code != 401 {
			t.Fatalf("wrong key #%d: %d, want 401", i, rec.Code)
		}
	}
	assertErr(t, e.do(req{path: "/api/v1/floor", remote: a, hdr: wrong}), 429, CodeTooManyRequests, "11th wrong key")
	// the port does not matter, the address does
	assertErr(t, e.do(req{path: "/api/v1/floor", remote: b, hdr: wrong}), 429, CodeTooManyRequests, "same address, other port")
	// a caller with the right key is not locked out
	if rec := e.do(req{path: "/api/v1/floor", remote: a, hdr: e.auth()}); rec.Code != 200 {
		t.Errorf("right key while limited: %d", rec.Code)
	}
	// another address is independent
	if rec := e.do(req{path: "/api/v1/floor", remote: other, hdr: wrong}); rec.Code != 401 {
		t.Errorf("other address: %d, want 401", rec.Code)
	}
	// the window ends
	*e.now = e.now.Add(61 * time.Second)
	if rec := e.do(req{path: "/api/v1/floor", remote: a, hdr: wrong}); rec.Code != 401 {
		t.Errorf("after the window: %d, want 401", rec.Code)
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	e := newEnv(t)
	wrong := map[string]string{"Authorization": "Bearer wrong"}
	for i := 0; i < 100000; i++ {
		addr := fmt.Sprintf("10.%d.%d.%d:1", i>>16&255, i>>8&255, i&255)
		e.do(req{path: "/api/v1/floor", remote: addr, hdr: wrong})
	}
	if n := e.s.limiter.size(); n > 1024 {
		t.Fatalf("limiter holds %d addresses, want at most 1024", n)
	}
}

// Structure test: the compare uses crypto/subtle, and nothing compares the
// key with == or bytes.Equal or strings.EqualFold.
func TestKeyCompareStructure(t *testing.T) {
	src, err := os.ReadFile("key.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, `"crypto/subtle"`) || !strings.Contains(s, "subtle.ConstantTimeCompare") {
		t.Error("key.go must compare with crypto/subtle")
	}
	files, _ := os.ReadDir(".")
	for _, f := range files {
		if strings.HasSuffix(f.Name(), "_test.go") || !strings.HasSuffix(f.Name(), ".go") {
			continue
		}
		b, _ := os.ReadFile(f.Name())
		for _, bad := range []string{"bytes.Equal(", "strings.EqualFold(", "s.key ==", "== s.key", "string(s.key) ==", "== string(s.key)"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s uses %s on the key", f.Name(), bad)
			}
		}
	}
}

func httpReq(e *testEnv, rt RouteInfo) *http.Request {
	r, _ := http.NewRequest(rt.Method, "http://127.0.0.1:18765"+rt.Path, nil)
	r.Host = "127.0.0.1:18765"
	r.RemoteAddr = "127.0.0.1:50001"
	return r
}
