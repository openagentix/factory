package verify

import (
	"reflect"
	"sort"
	"testing"
)

func TestScrubEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/bin", "HOME=/home/u", "LANG=C", "LC_ALL=C", "TMPDIR=/tmp", "TERM=xterm",
		"GITHUB_TOKEN=a", "GH_TOKEN=b", "GH_HOST=c", "GITHUB_ACTOR=d",
		"ANTHROPIC_API_KEY=e", "ANTHROPIC_BASE_URL=f", "OPENAI_API_KEY=g", "CLAUDE_CODE_X=h", "CODEX_HOME=i",
		"AWS_ACCESS_KEY_ID=j", "AWS_PROFILE=k", "SSH_AUTH_SOCK=/s",
		"MY_SECRET=l", "db_password=m", "STRIPE_API_KEY=n", "Some_Token_Value=o",
		"EDITOR=vi", "USER=u", "SHELL=/bin/sh", "LC_CTYPE=C", "GOFLAGS=-mod=mod",
		"BROKEN", "=x",
	}
	got := ScrubEnv(in)
	want := []string{"HOME=/home/u", "LANG=C", "LC_ALL=C", "PATH=/usr/bin", "TERM=xterm", "TMPDIR=/tmp"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestSensitiveName(t *testing.T) {
	for _, n := range []string{"GITHUB_TOKEN", "x_token", "A_SECRET_B", "PASSWORD", "my_api_key", "ANTHROPIC_FOO", "OPENAI_X",
		"CLAUDE_X", "CODEX_X", "GH_X", "GITHUB_X", "AWS_X", "SSH_AUTH_SOCK", "gh_host"} {
		if !sensitiveName(n) {
			t.Errorf("%q should be sensitive", n)
		}
	}
	for _, n := range []string{"PATH", "HOME", "LANG", "TERM", "TMPDIR", "GOFLAGS", "GHOST"} {
		if sensitiveName(n) {
			t.Errorf("%q should not be sensitive", n)
		}
	}
}
