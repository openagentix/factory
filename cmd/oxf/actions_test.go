package main

import (
	"context"
	"errors"
	"testing"

	"github.com/openagentix/factory/internal/server"
)

type okActions struct{ n int }

func (o *okActions) Answer(context.Context, server.AnswerRequest) (server.ActionResult, error) {
	o.n++
	return server.ActionResult{Result: server.ResultDone, JournalSeq: 1}, nil
}
func (o *okActions) Consequence(context.Context, server.ConsequenceRequest) (server.ActionResult, error) {
	o.n++
	return server.ActionResult{Result: server.ResultDone, JournalSeq: 1}, nil
}
func (o *okActions) Approve(context.Context, server.ApproveRequest) (server.ActionResult, error) {
	o.n++
	return server.ActionResult{Result: server.ResultDone, JournalSeq: 1}, nil
}

// The server takes its routes before the demo exists. Until the demo is
// ready every action is "not ready", and nothing reaches it.
func TestLazyActionsAreNotReadyUntilSet(t *testing.T) {
	l := &lazyActions{}
	ctx := context.Background()
	if _, err := l.Answer(ctx, server.AnswerRequest{}); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("Answer: %v", err)
	}
	if _, err := l.Consequence(ctx, server.ConsequenceRequest{}); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("Consequence: %v", err)
	}
	if _, err := l.Approve(ctx, server.ApproveRequest{}); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("Approve: %v", err)
	}
	a := &okActions{}
	l.set(a)
	for _, f := range []func() error{
		func() error { _, err := l.Answer(ctx, server.AnswerRequest{}); return err },
		func() error { _, err := l.Consequence(ctx, server.ConsequenceRequest{}); return err },
		func() error { _, err := l.Approve(ctx, server.ApproveRequest{}); return err },
	} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	if a.n != 3 {
		t.Fatalf("calls = %d", a.n)
	}
}
