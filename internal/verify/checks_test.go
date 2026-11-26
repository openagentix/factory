package verify

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func repoWithHead(t *testing.T) (*testRepo, string) {
	t.Helper()
	r := newTestRepo(t)
	r.write("committed.txt", "yes\n")
	head := r.commit("head")
	r.write("untracked.txt", "no\n") // not part of the commit
	return r, head
}

func hostileEnviron() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=/home/someone", "LANG=C", "TERM=dumb", "TMPDIR=" + os.TempDir(),
		"GITHUB_TOKEN=ghp_fake", "GH_TOKEN=fake", "ANTHROPIC_API_KEY=fake", "OPENAI_API_KEY=fake",
		"AWS_SECRET_ACCESS_KEY=fake", "SSH_AUTH_SOCK=/tmp/agent", "CLAUDE_CODE_OAUTH_TOKEN=fake",
		"MY_PASSWORD=fake", "CODEX_API_KEY=fake", "UNLISTED_VAR=fake",
	}
}

func TestRunChecks_EnvironmentSeenByACheck(t *testing.T) {
	r, head := repoWithHead(t)
	out, err := RunChecks(context.Background(), r.dir, head,
		[]api.Check{{Name: "env", Run: "env"}},
		CheckOptions{Deadline: 20 * time.Second, Environ: hostileEnviron()})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Result.ExitCode != 0 {
		t.Fatalf("env failed: %+v", out[0])
	}
	seen := map[string]string{}
	for _, l := range strings.Split(out[0].Output, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			seen[k] = v
		}
	}
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "AWS_SECRET_ACCESS_KEY",
		"SSH_AUTH_SOCK", "CLAUDE_CODE_OAUTH_TOKEN", "MY_PASSWORD", "CODEX_API_KEY", "UNLISTED_VAR"} {
		if v, ok := seen[name]; ok {
			t.Errorf("check sees %s=%s", name, v)
		}
	}
	if strings.Contains(out[0].Output, "fake") || strings.Contains(out[0].Output, "ghp_") {
		t.Error("a secret value reached the check")
	}
	for _, name := range []string{"PATH", "HOME", "LANG", "TERM", "TMPDIR"} {
		if _, ok := seen[name]; !ok {
			t.Errorf("check does not see %s", name)
		}
	}
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TMPDIR": true, "TERM": true,
		"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true} // the shell adds the last four
	for k := range seen {
		if !allowed[k] {
			t.Errorf("unexpected variable %s", k)
		}
	}
}

func TestRunChecks_CleanCheckoutOfHead(t *testing.T) {
	r, head := repoWithHead(t)
	out, err := RunChecks(context.Background(), r.dir, head, []api.Check{
		{Name: "present", Run: "test -f committed.txt && ! test -e untracked.txt && pwd"},
	}, CheckOptions{Deadline: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Result.ExitCode != 0 {
		t.Fatalf("outcome: %+v", out[0])
	}
	dir := strings.TrimSpace(out[0].Output)
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("checkout %s was not removed", dir)
	}
	if err := checkTrusted(context.Background(), r.dir); err != nil {
		t.Errorf("repository no longer passes the guard: %v", err)
	}
}

func TestRunChecks_RecordsOutcomes(t *testing.T) {
	r, head := repoWithHead(t)
	out, err := RunChecks(context.Background(), r.dir, head, []api.Check{
		{Name: "ok", Run: "echo fine"},
		{Name: "fails", Run: "echo bad >&2; exit 3"},
		{Name: "after", Run: "true"},
	}, CheckOptions{Deadline: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d outcomes", len(out))
	}
	if out[0].Result.Name != "ok" || out[0].Result.ExitCode != 0 || out[0].Result.RanBy != "orchestrator" || out[0].TimedOut {
		t.Errorf("ok: %+v", out[0])
	}
	if out[1].Result.ExitCode != 3 || !strings.Contains(out[1].Output, "bad") {
		t.Errorf("fails: %+v", out[1])
	}
	if out[2].Result.ExitCode != 0 {
		t.Errorf("a failing check must not stop the next: %+v", out[2])
	}
	if AllPassed(out) {
		t.Error("AllPassed must be false when a check failed")
	}
	if !AllPassed(out[:1]) || AllPassed(nil) != true {
		t.Error("AllPassed must be true for passing checks, and for none")
	}
}

func TestRunChecks_DeadlineKillsTheProcessGroup(t *testing.T) {
	r, head := repoWithHead(t)
	start := time.Now()
	out, err := RunChecks(context.Background(), r.dir, head, []api.Check{
		// a background child holds the output pipe open, as a stuck test server would
		{Name: "hang", Run: "sleep 60 & echo $! ; wait"},
		{Name: "next", Run: "true"},
	}, CheckOptions{Deadline: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v, the deadline did not stop the check", d)
	}
	if !out[0].TimedOut || out[0].Result.ExitCode == 0 {
		t.Errorf("hang: %+v", out[0])
	}
	if out[1].TimedOut || out[1].Result.ExitCode != 0 {
		t.Errorf("next: %+v", out[1])
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(out[0].Output, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("no child pid in %q", out[0].Output)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			break // gone
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d is still running after the deadline", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := checkTrusted(context.Background(), r.dir); err != nil {
		t.Errorf("repository no longer passes the guard: %v", err)
	}
}

func TestRunChecks_BadCommitLeavesNothing(t *testing.T) {
	r, _ := repoWithHead(t)
	_, err := RunChecks(context.Background(), r.dir, "0123456789abcdef0123456789abcdef01234567",
		[]api.Check{{Name: "x", Run: "true"}}, CheckOptions{})
	if err == nil {
		t.Fatal("want an error")
	}
	if err := checkTrusted(context.Background(), r.dir); err != nil {
		t.Errorf("repository no longer passes the guard: %v", err)
	}
}

func TestRunChecks_CancelledContextStillCleansUp(t *testing.T) {
	r, head := repoWithHead(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	out, err := RunChecks(ctx, r.dir, head, []api.Check{{Name: "hang", Run: "sleep 60"}}, CheckOptions{Deadline: time.Minute})
	if err == nil && (len(out) == 0 || out[0].Result.ExitCode == 0) {
		t.Fatalf("a cancelled run must not look like a pass: %+v", out)
	}
	if err := checkTrusted(context.Background(), r.dir); err != nil {
		t.Errorf("repository no longer passes the guard: %v", err)
	}
}

func TestRunChecks_FreshHomeForEachCheck(t *testing.T) {
	r, head := repoWithHead(t)
	out, err := RunChecks(context.Background(), r.dir, head, []api.Check{
		{Name: "a", Run: `echo "$HOME"; echo "$TMPDIR"; ls -A "$HOME" | wc -l; touch "$HOME/cache"`},
		{Name: "b", Run: `echo "$HOME"; ls -A "$HOME" | wc -l`},
	}, CheckOptions{Deadline: 20 * time.Second, Environ: hostileEnviron()})
	if err != nil {
		t.Fatal(err)
	}
	la := strings.Fields(out[0].Output)
	lb := strings.Fields(out[1].Output)
	if len(la) != 3 || len(lb) != 2 {
		t.Fatalf("outputs: %q %q", out[0].Output, out[1].Output)
	}
	if strings.Contains(out[0].Output, "someone") {
		t.Errorf("the real HOME reached the check: %q", out[0].Output)
	}
	if la[0] != la[1] {
		t.Errorf("TMPDIR %q must be the same fresh directory as HOME %q", la[1], la[0])
	}
	if la[2] != "0" || lb[1] != "0" {
		t.Errorf("HOME must start empty: %q %q", la[2], lb[1])
	}
	if la[0] == lb[0] {
		t.Errorf("each check needs its own HOME, both had %s", la[0])
	}
	if _, err := os.Stat(la[0]); err == nil {
		t.Errorf("HOME %s was not removed", la[0])
	}
	notes := strings.Join(out[0].Notes, "\n")
	for _, want := range []string{"cold", "sandbox", "setsid", "same user", "tampered", "separate user"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q: %s", want, notes)
		}
	}
}
