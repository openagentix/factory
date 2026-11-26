package githost

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testToken = "test-token-not-real"

// loggingGit writes a wrapper script that records its arguments and then runs
// the real git.
func loggingGit(t *testing.T) (script, logFile string) {
	t.Helper()
	real, err := lookGit()
	must(t, err)
	dir := t.TempDir()
	logFile = filepath.Join(dir, "args.log")
	script = filepath.Join(dir, "git")
	body := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + logFile + "'; done\nenv >> '" + logFile + ".env'\nexec " + real + " \"$@\"\n"
	must(t, os.WriteFile(script, []byte(body), 0o755))
	return script, logFile
}

func TestPushToLocalBareRepoKeepsTokenOut(t *testing.T) {
	fx := newGitFixture(t)
	script, logFile := loggingGit(t)
	p, err := newPusher(fx.bare, script, testToken)
	must(t, err)

	must(t, p.push(context.Background(), fx.work, fx.c1, "factory/run-1", false))
	if fx.head("factory/run-1") != fx.c1 {
		t.Fatalf("remote head = %q, want %q", fx.head("factory/run-1"), fx.c1)
	}
	// A force push over an unrelated commit works; a plain one is refused.
	if err := p.push(context.Background(), fx.work, fx.c2, "factory/run-1", false); err == nil {
		t.Fatal("a non fast-forward push without force succeeded")
	} else if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error holds the token: %v", err)
	}
	must(t, p.push(context.Background(), fx.work, fx.c2, StagingBranch, true))
	if fx.head(StagingBranch) != fx.c2 {
		t.Fatalf("staging = %q", fx.head(StagingBranch))
	}

	args, _ := os.ReadFile(logFile)
	if len(args) == 0 {
		t.Fatal("the git wrapper was not used")
	}
	if strings.Contains(string(args), testToken) {
		t.Fatalf("token in the argument list:\n%s", args)
	}
	env, _ := os.ReadFile(logFile + ".env")
	if !strings.Contains(string(env), "OXF_GIT_TOKEN="+testToken) {
		t.Fatal("token was not passed through the environment")
	}
	for _, bad := range []string{"GITHUB_TOKEN", "GH_TOKEN", "SSH_AUTH_SOCK"} {
		if strings.Contains(string(env), bad+"=") {
			t.Fatalf("%s leaked into git's environment", bad)
		}
	}
}

func TestPushSendsTokenOnlyInTheAuthorizationHeader(t *testing.T) {
	fx := newGitFixture(t)
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Error(w, "repository says no: "+testToken, http.StatusForbidden)
	}))
	defer srv.Close()
	script, logFile := loggingGit(t)
	t.Setenv("GITHUB_TOKEN", "ambient-token-not-real")
	p, err := newPusher(srv.URL+"/o/r.git", script, testToken)
	must(t, err)
	err = p.push(context.Background(), fx.work, fx.c1, "factory/run-1", false)
	if err == nil {
		t.Fatal("push to a server that answers 403 succeeded")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "ambient") {
		t.Fatalf("error holds a token: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+testToken))
	got := false
	for _, a := range auths {
		got = got || a == want
	}
	if !got {
		t.Fatalf("the credential helper did not deliver the token; saw %d requests", len(auths))
	}
	args, _ := os.ReadFile(logFile)
	if strings.Contains(string(args), testToken) {
		t.Fatalf("token in the argument list:\n%s", args)
	}
}

func TestNewPusherRejectsUnsafeRemotes(t *testing.T) {
	for _, u := range []string{
		"https://user:secret@example.invalid/o/r.git", "https://user@example.invalid/o/r.git",
		"-oProxyCommand=x", "ext::sh -c id", "ssh://example.invalid/o/r.git", "relative/path", "", "git://example.invalid/o/r",
	} {
		if _, err := newPusher(u, "git", testToken); err == nil {
			t.Errorf("remote %q accepted", u)
		}
	}
	for _, u := range []string{"https://github.com/o/r.git", "http://127.0.0.1:1/o/r.git", "/tmp/remote.git", "file:///tmp/remote.git"} {
		if _, err := newPusher(u, "git", testToken); err != nil {
			t.Errorf("remote %q refused: %v", u, err)
		}
	}
}
