package githost

import (
	"errors"
	"testing"
)

func TestCheckBranchPush(t *testing.T) {
	tests := []struct {
		branch  string
		wantErr bool
	}{
		{"factory/run-1", false},
		{"factory/a/b", false},
		{"main", true},
		{"factory-staging", true},
		{"factory/", true},
		{"feature/x", true},
		{"Factory/x", true},
		{"factory/../main", true},
		{"factory/a b", true},
		{"factory/-x", false},
		{"factory/x.lock", true},
		{"factory//x", true},
		{"factory/x/", true},
		{"factory/x\n", true},
		{"refs/heads/factory/x", true},
		{"", true},
	}
	for _, tc := range tests {
		err := checkBranchPush(tc.branch, "main")
		if (err != nil) != tc.wantErr {
			t.Errorf("checkBranchPush(%q) err = %v, wantErr %v", tc.branch, err, tc.wantErr)
		}
		if err != nil && !errors.Is(err, ErrRefused) {
			t.Errorf("checkBranchPush(%q) err %v does not wrap ErrRefused", tc.branch, err)
		}
	}
	// A default branch with the agent prefix is still refused.
	if err := checkBranchPush("factory/odd", "factory/odd"); !errors.Is(err, ErrRefused) {
		t.Errorf("default branch named like an agent branch was allowed: %v", err)
	}
}

func TestCheckCommit(t *testing.T) {
	good := "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []string{good, "--upload-pack=x", "HEAD", "main", good + "x", ""} {
		err := checkCommit(c)
		if (c == good) != (err == nil) {
			t.Errorf("checkCommit(%q) = %v", c, err)
		}
	}
}

func TestCheckPRBranches(t *testing.T) {
	tests := []struct {
		head, base string
		wantErr    bool
	}{
		{"factory/r1", "factory-staging", false},
		{"factory-staging", "main", false},
		{"factory/r1", "main", true},
		{"main", "factory-staging", true},
		{"feature/x", "factory-staging", true},
		{"factory-staging", "factory-staging", true},
		{"factory/r1", "other", true},
	}
	for _, tc := range tests {
		err := checkPRBranches(tc.head, tc.base, "main")
		if (err != nil) != tc.wantErr {
			t.Errorf("checkPRBranches(%q,%q) = %v, wantErr %v", tc.head, tc.base, err, tc.wantErr)
		}
	}
}

func TestCheckLabel(t *testing.T) {
	for _, l := range FixedLabels() {
		if err := checkLabel(l); err != nil {
			t.Errorf("label %q refused: %v", l, err)
		}
	}
	for _, l := range []string{"bug", "factory:other", "factory:", "FACTORY:ready", "factory:ready "} {
		if err := checkLabel(l); !errors.Is(err, ErrRefused) {
			t.Errorf("label %q err = %v, want ErrRefused", l, err)
		}
	}
}

func TestTrusted(t *testing.T) {
	tests := []struct {
		assoc, author string
		want          bool
	}{
		{"OWNER", "a", true}, {"member", "a", true}, {"COLLABORATOR", "a", true},
		{"CONTRIBUTOR", "a", false}, {"NONE", "a", false}, {"FIRST_TIMER", "a", false}, {"", "a", false},
		{"NONE", "factory-bot", true}, {"NONE", "Factory-Bot", true}, {"NONE", "", false},
	}
	for _, tc := range tests {
		if got := trusted(tc.assoc, tc.author, "factory-bot"); got != tc.want {
			t.Errorf("trusted(%q,%q) = %v", tc.assoc, tc.author, got)
		}
	}
	if trusted("NONE", "", "") {
		t.Error("empty author matched empty factory login")
	}
}

func TestTokensNeverPrint(t *testing.T) {
	p, err := NewProposeToken("test-token-not-real")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := NewAcceptToken("test-accept-not-real")
	for _, v := range []any{p, a, &p, &a} {
		for _, s := range []string{sprintf("%v", v), sprintf("%+v", v), sprintf("%#v", v), sprintf("%s", v)} {
			if contains(s, "not-real") {
				t.Errorf("token leaked: %s", s)
			}
		}
	}
	if _, err := NewProposeToken(""); err == nil {
		t.Error("empty token accepted")
	}
	if _, err := NewProposeToken("a b"); err == nil {
		t.Error("token with space accepted")
	}
}

func TestRedact(t *testing.T) {
	got := redact("fatal: https://x:test-token-not-real@h/ and test-token-not-real", "test-token-not-real", "")
	if contains(got, "not-real") {
		t.Fatalf("redact left the token: %s", got)
	}
}
