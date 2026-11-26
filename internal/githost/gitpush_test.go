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

// loggingGit writes a wrapper script that records its arguments and
// environment and then runs the real git.
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

// toBare points the pusher's fixed https remote at a local bare repository.
func toBare(p *pusher, bare string) {
	p.extra = []string{"-c", "url." + bare + ".insteadOf=" + p.remote, "-c", "protocol.file.allow=always"}
}

func TestPushToLocalBareRepoKeepsTokenOut(t *testing.T) {
	fx := newGitFixture(t)
	script, logFile := loggingGit(t)
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)
	t.Setenv("GITHUB_TOKEN", "ambient-token-not-real")
	p, err := newPusher("github.com", Repo{"demo", "demo"}, script, testToken, false)
	must(t, err)
	toBare(p, fx.bare)

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
	// A commit that is not in the source directory is refused.
	if err := p.push(context.Background(), fx.work, strings.Repeat("e", 40), "factory/x", false); err == nil {
		t.Fatal("pushed a commit that does not exist")
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
	// The token is in the environment of the push commands only: every
	// invocation of the wrapper adds one block to the log, and only the push
	// ones carry the variable.
	blocks := strings.Count(string(env), "LC_ALL=C")
	if with := strings.Count(string(env), "OXF_GIT_TOKEN="); with >= blocks || with == 0 {
		t.Fatalf("token in %d of %d git environments, want only the push ones", with, blocks)
	}
	for _, bad := range []string{"GITHUB_TOKEN", "GH_TOKEN", "SSH_AUTH_SOCK"} {
		if strings.Contains(string(env), bad+"=") {
			t.Fatalf("%s leaked into git's environment", bad)
		}
	}
	// The temporary repository is gone, and no file under the temp root holds the token.
	left, _ := os.ReadDir(tmpRoot)
	if len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
	assertNoTokenInTree(t, fx.work)
	assertNoTokenInTree(t, fx.bare)
}

func assertNoTokenInTree(t *testing.T, root string) {
	t.Helper()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), testToken) {
				t.Errorf("token found in file %s", path)
			}
		}
		return nil
	})
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
	p, err := newPusher(strings.TrimPrefix(srv.URL, "http://"), Repo{"o", "r"}, script, testToken, true)
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

func TestCredentialHelperAnswersOnlyForTheExpectedHost(t *testing.T) {
	p, err := newPusher("github.com", Repo{"o", "r"}, "", testToken, false)
	must(t, err)
	ask := func(input string) (string, error) {
		return p.git(context.Background(), t.TempDir(), gitCall{
			cfg: p.pushConfig(), args: []string{"credential", "fill"}, stdin: input, token: true,
		})
	}
	out, err := ask("protocol=https\nhost=github.com\n\n")
	if err != nil || !strings.Contains(out, "password=") {
		t.Fatalf("expected host got no credential: %q %v", out, err)
	}
	for _, in := range []string{
		"protocol=https\nhost=evil.example\n\n",
		"protocol=https\nhost=github.com.evil.example\n\n",
		"protocol=http\nhost=github.com\n\n",
		"protocol=https\nhost=GITHUB.com\n\n",
		"protocol=https\nhost=github.com:8443\n\n",
	} {
		out, err := ask(in)
		if err == nil {
			t.Errorf("a credential was given for %q", in)
		}
		if strings.Contains(out, testToken) || (err != nil && strings.Contains(err.Error(), testToken)) {
			t.Errorf("token shown for %q", in)
		}
	}
}

func TestNewPusherBuildsTheRemoteItself(t *testing.T) {
	p, err := newPusher("github.com", Repo{"o", "r"}, "git", testToken, false)
	must(t, err)
	if p.remote != "https://github.com/o/r.git" {
		t.Fatalf("remote = %q", p.remote)
	}
	// http only for a loopback host and only on request.
	p, _ = newPusher("127.0.0.1:9", Repo{"o", "r"}, "git", testToken, false)
	if p.scheme != "https" {
		t.Fatalf("loopback without the test option got %s", p.scheme)
	}
	p, _ = newPusher("127.0.0.1:9", Repo{"o", "r"}, "git", testToken, true)
	if p.scheme != "http" {
		t.Fatalf("loopback with the test option got %s", p.scheme)
	}
	p, _ = newPusher("example.com", Repo{"o", "r"}, "git", testToken, true)
	if p.scheme != "https" {
		t.Fatalf("a non loopback host got %s", p.scheme)
	}
	for _, h := range []string{"", "-oProxyCommand=x", "u:p@example.com", "example.com/x", "a b", "ext::sh", "example.com:port"} {
		if _, err := newPusher(h, Repo{"o", "r"}, "git", testToken, false); err == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	for _, r := range []Repo{{"", "r"}, {"o", ""}, {"o/x", "r"}, {"o", "r@h"}} {
		if _, err := newPusher("github.com", r, "git", testToken, false); err == nil {
			t.Errorf("repo %v accepted", r)
		}
	}
}

func TestOnlyOneCallSiteBuildsAGitCommand(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	sites := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		n := strings.Count(string(b), "exec.Command")
		if n > 0 && f != "gitpush.go" {
			t.Errorf("%s builds a command outside gitpush.go", f)
		}
		sites += n
	}
	if sites != 1 {
		t.Fatalf("%d call sites build a command, want exactly 1", sites)
	}
}
