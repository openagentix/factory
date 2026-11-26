package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func TestFakeRunnerGivesItsOutcomeAndEvents(t *testing.T) {
	f := &Fake{Outcome: Outcome{Commit: "abc", Changed: true}, Events: []api.Event{{Kind: api.EventMessage, Text: "hi"}}}
	var seen []string
	h, err := f.Start(context.Background(), Spec{OnEvent: func(e api.Event) { seen = append(seen, e.Text) }, Commit: "base"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for e := range h.Events() {
		got = append(got, e.Text)
	}
	o, err := h.Wait(context.Background())
	if err != nil || o.Commit != "abc" || len(got) != 1 || len(seen) != 1 {
		t.Errorf("o=%+v err=%v got=%v seen=%v", o, err, got, seen)
	}
	if len(f.Started()) != 1 || f.Started()[0].Commit != "base" {
		t.Errorf("started = %+v", f.Started())
	}
}

func TestFakeRunnerHoldAndCancel(t *testing.T) {
	f := &Fake{Hold: true}
	h, _ := f.Start(context.Background(), Spec{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := h.Wait(ctx); err == nil {
		t.Error("a held run must not finish by itself")
	}
	if err := h.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	o, err := h.Wait(context.Background())
	if err != nil || !o.Cancelled || !o.Failed {
		t.Errorf("o=%+v err=%v", o, err)
	}
}

func TestFakeRunnerStartError(t *testing.T) {
	f := &Fake{StartErr: errors.New("no")}
	if _, err := f.Start(context.Background(), Spec{}); err == nil {
		t.Error("want the start error")
	}
}
