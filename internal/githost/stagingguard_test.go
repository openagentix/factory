package githost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The staging branch is force pushed. If the repository's default branch is
// the staging branch, a force push would rewrite the default branch. So
// PushStaging reads the default branch name first, and refuses when it is
// blank, cannot be read, or is the staging branch in any spelling.

func TestPushStagingNeverWritesTheDefaultBranch_Fake(t *testing.T) {
	c1, _ := fakeCommits()
	for _, def := range []string{"factory-staging", "refs/heads/factory-staging", "Factory-Staging", " factory-staging", "factory-staging ", "factory-staging/", "FACTORY-STAGING", ""} {
		t.Run(fmt.Sprintf("default=%q", def), func(t *testing.T) {
			fx := DemoFixture()
			fx.DefaultBranch = def
			f := NewFake(fx, newTestClock().Now)
			err := f.PushStaging(context.Background(), t.TempDir(), c1)
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("PushStaging with default branch %q = %v, want ErrRefused", def, err)
			}
			if got := f.BranchHead(StagingBranch); got == c1 {
				t.Fatalf("the staging branch (the default branch) was written: %q", got)
			}
		})
	}
}

// ghDefault serves a GitHub repository whose default branch is def. A status
// of 0 means 200.
func ghDefault(def string, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/demo/demo" {
			if status != 0 {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"boom"}`)
				return
			}
			fmt.Fprintf(w, `{"default_branch":%q}`, def)
			return
		}
		fmt.Fprint(w, `[]`)
	}
}

// reachedPush calls fn and reports whether it got as far as the git push.
// The pusher of the test client is nil, so reaching it panics.
func reachedPush(fn func() error) (err error, reached bool) {
	defer func() {
		if r := recover(); r != nil {
			reached = true
		}
	}()
	return fn(), false
}

func TestPushStagingNeverWritesTheDefaultBranch_GitHub(t *testing.T) {
	c1, _ := fakeCommits()
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"staging is the default", ghDefault("factory-staging", 0)},
		{"other spelling", ghDefault("refs/heads/factory-staging", 0)},
		{"other case", ghDefault("Factory-Staging", 0)},
		{"blank default", ghDefault("", 0)},
		{"default cannot be read", ghDefault("main", 500)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := testGH(t, tc.handler)
			g.push = nil
			err, reached := reachedPush(func() error { return g.PushStaging(context.Background(), t.TempDir(), c1) })
			if reached {
				t.Fatal("PushStaging got as far as the push")
			}
			if err == nil {
				t.Fatal("PushStaging must fail")
			}
			// A blank or unreadable default branch is an error from the host call, which
			// is as good: nothing was pushed.
			if strings.HasPrefix(tc.name, "staging") || strings.HasPrefix(tc.name, "other") {
				if !errors.Is(err, ErrRefused) {
					t.Fatalf("err = %v, want ErrRefused", err)
				}
			}
		})
	}
}

func TestPushStagingGoesAheadWhenTheDefaultBranchIsAnother_GitHub(t *testing.T) {
	c1, _ := fakeCommits()
	g, _ := testGH(t, ghDefault("main", 0))
	g.push = nil
	if _, reached := reachedPush(func() error { return g.PushStaging(context.Background(), t.TempDir(), c1) }); !reached {
		t.Fatal("with default branch main, PushStaging must go on to the push")
	}
}

// A pull request into the staging branch, from an agent branch, is a pull
// request into the default branch when the default branch is the staging
// branch.
func TestOpenPullRequestRefusedWhenStagingIsTheDefault(t *testing.T) {
	fx := DemoFixture()
	fx.DefaultBranch = StagingBranch
	f := NewFake(fx, newTestClock().Now)
	if _, err := f.OpenPullRequest(context.Background(), PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "x"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}
	if err := checkPRBranches("factory/r1", StagingBranch, StagingBranch); !errors.Is(err, ErrRefused) {
		t.Fatalf("checkPRBranches = %v", err)
	}
}

// Every publish action that takes a branch name refuses the staging branch,
// in the spellings an attacker would try. Only the exact name is the staging
// branch, and it is allowed in the one place the design names: the head of the
// staging pull request into the default branch, and the base of an agent
// branch's pull request.
func TestBranchNamesNeverReachTheStagingBranch(t *testing.T) {
	spellings := []string{
		"refs/heads/factory-staging", "Factory-Staging", "FACTORY-STAGING", "factory-staging/", "factory/../factory-staging",
		"factory/./factory-staging", "factory-staging.lock", " factory-staging", "factory-staging ", "\tfactory-staging",
		"factory-staging\n", "\nfactory-staging", "factory-staging\x00", "factory\x00-staging", "factory-staging\x00x", "factory-stag\ning",
		"factory//factory-staging", "factory-staging/.", "./factory-staging", "factory/.factory-staging", "factory/factory-staging.lock",
		"refs/heads/factory/../factory-staging", "factory-staging@{0}", "-factory-staging", "factory-staging.",
	}
	type host struct {
		name string
		mk   func(t *testing.T) Host
	}
	hosts := []host{
		{"fake", func(t *testing.T) Host { return NewFake(DemoFixture(), newTestClock().Now) }},
		{"github", func(t *testing.T) Host {
			g, _ := testGH(t, ghDefault("main", 0))
			g.push = nil
			return g
		}},
	}
	ctx := context.Background()
	c2 := strings.Repeat("2", 40)
	for _, h := range hosts {
		for _, s := range spellings {
			t.Run(fmt.Sprintf("%s/%q", h.name, s), func(t *testing.T) {
				host := h.mk(t)
				expect := func(what string, fn func() error) {
					t.Helper()
					err, reached := reachedPush(fn)
					if reached || !errors.Is(err, ErrRefused) {
						t.Errorf("%s with %q: err = %v, reached the push = %v; want ErrRefused", what, s, err, reached)
					}
				}
				expect("PushBranch", func() error { return host.PushBranch(ctx, t.TempDir(), c2, s) })
				expect("ManualPullRequestURL", func() error { _, err := host.ManualPullRequestURL(ctx, s); return err })
				expect("OpenPullRequest head into staging", func() error {
					_, err := host.OpenPullRequest(ctx, PRSpec{Head: s, Base: StagingBranch, Title: "x"})
					return err
				})
				expect("OpenPullRequest staging-like head into default", func() error {
					_, err := host.OpenPullRequest(ctx, PRSpec{Head: s, Base: "main", Title: "x"})
					return err
				})
				expect("OpenPullRequest agent branch into staging-like base", func() error {
					_, err := host.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: s, Title: "x"})
					return err
				})
			})
		}
	}
	// The exact name: not a branch an agent may push or ask a link for.
	for _, h := range hosts {
		host := h.mk(t)
		if err := host.PushBranch(ctx, t.TempDir(), c2, StagingBranch); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: PushBranch(staging) = %v", h.name, err)
		}
		if _, err := host.ManualPullRequestURL(ctx, StagingBranch); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: ManualPullRequestURL(staging) = %v", h.name, err)
		}
		if _, err := host.OpenPullRequest(ctx, PRSpec{Head: StagingBranch, Base: StagingBranch, Title: "x"}); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: staging into staging = %v", h.name, err)
		}
	}
}

// A branch that differs from the default branch only by letter case is the
// default branch on a host that folds case. The rules compare without case.
func TestBranchesThatDifferFromTheDefaultOnlyByCaseAreRefused(t *testing.T) {
	push := []struct {
		branch, def string
		wantErr     bool
	}{
		{"factory/X", "factory/x", true},
		{"factory/x", "factory/X", true},
		{"FACTORY/x", "factory/x", true},
		{"factory/x", "factory/x", true},
		{"factory/y", "factory/x", false},
		{"factory/run-1", "main", false},
		{"factory/Run-1", "main", false},
	}
	for _, tc := range push {
		err := checkBranchPush(tc.branch, tc.def)
		if (err != nil) != tc.wantErr || (err != nil && !errors.Is(err, ErrRefused)) {
			t.Errorf("checkBranchPush(%q, default %q) = %v, wantErr %v", tc.branch, tc.def, err, tc.wantErr)
		}
	}
	pr := []struct {
		head, base, def string
		wantErr         bool
	}{
		{"factory/X", StagingBranch, "factory/x", true},
		{"factory/x", StagingBranch, "factory/X", true},
		{"factory/r1", StagingBranch, "main", false},
		{StagingBranch, "main", "main", false},
		{StagingBranch, "Main", "main", true},
		{StagingBranch, "main", "Main", true},
	}
	for _, tc := range pr {
		err := checkPRBranches(tc.head, tc.base, tc.def)
		if (err != nil) != tc.wantErr || (err != nil && !errors.Is(err, ErrRefused)) {
			t.Errorf("checkPRBranches(%q, %q, default %q) = %v, wantErr %v", tc.head, tc.base, tc.def, err, tc.wantErr)
		}
	}
	// Through the hosts.
	fx := DemoFixture()
	fx.DefaultBranch = "factory/x"
	f := NewFake(fx, newTestClock().Now)
	ctx := context.Background()
	c := strings.Repeat("2", 40)
	if err := f.PushBranch(ctx, t.TempDir(), c, "factory/X"); !errors.Is(err, ErrRefused) {
		t.Errorf("Fake PushBranch = %v", err)
	}
	if _, err := f.ManualPullRequestURL(ctx, "factory/X"); !errors.Is(err, ErrRefused) {
		t.Errorf("Fake ManualPullRequestURL = %v", err)
	}
	if _, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/X", Base: StagingBranch, Title: "x"}); !errors.Is(err, ErrRefused) {
		t.Errorf("Fake OpenPullRequest = %v", err)
	}
	g, _ := testGH(t, ghDefault("factory/x", 0))
	g.push = nil
	if err, reached := reachedPush(func() error { return g.PushBranch(ctx, t.TempDir(), c, "factory/X") }); reached || !errors.Is(err, ErrRefused) {
		t.Errorf("GitHub PushBranch = %v, reached push %v", err, reached)
	}
}
