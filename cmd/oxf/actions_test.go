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

func (o *okActions) Accept(context.Context, server.HoldingRequest) (server.ActionResult, error) {
	o.n++
	return server.ActionResult{Result: server.ResultDone, JournalSeq: 1}, nil
}
func (o *okActions) CancelAccept(context.Context, server.HoldingRequest) (server.ActionResult, error) {
	o.n++
	return server.ActionResult{Result: server.ResultDone, JournalSeq: 1}, nil
}
func (o *okActions) Undo(context.Context, server.UndoRequest) (server.ActionResult, error) {
	o.n++
	return server.ActionResult{Result: server.ResultDone, JournalSeq: 1}, nil
}
func (o *okActions) UndoPreview(context.Context, string) (server.UndoPreviewView, error) {
	o.n++
	return server.UndoPreviewView{}, nil
}
func (o *okActions) Piece(context.Context, string) ([]byte, bool, error) {
	o.n++
	return []byte(`{}`), true, nil
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
	if _, err := l.Accept(ctx, server.HoldingRequest{}); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := l.CancelAccept(ctx, server.HoldingRequest{}); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("CancelAccept: %v", err)
	}
	if _, err := l.Undo(ctx, server.UndoRequest{}); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("Undo: %v", err)
	}
	if _, err := l.UndoPreview(ctx, "run-1"); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("UndoPreview: %v", err)
	}
	if _, _, err := (&lazyDetail{}).Piece(ctx, "t1"); !errors.Is(err, server.ErrUnavailable) {
		t.Fatalf("Piece: %v", err)
	}
	a := &okActions{}
	l.set(a)
	ld := &lazyDetail{}
	ld.set(a)
	for _, f := range []func() error{
		func() error { _, err := l.Answer(ctx, server.AnswerRequest{}); return err },
		func() error { _, err := l.Consequence(ctx, server.ConsequenceRequest{}); return err },
		func() error { _, err := l.Approve(ctx, server.ApproveRequest{}); return err },
		func() error { _, err := l.Accept(ctx, server.HoldingRequest{}); return err },
		func() error { _, err := l.CancelAccept(ctx, server.HoldingRequest{}); return err },
		func() error { _, err := l.Undo(ctx, server.UndoRequest{}); return err },
		func() error { _, err := l.UndoPreview(ctx, "run-1"); return err },
		func() error { _, _, err := ld.Piece(ctx, "t1"); return err },
	} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	if a.n != 8 {
		t.Fatalf("calls = %d", a.n)
	}
}
