package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

var t0 = time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// newRun returns a valid queued run for the implement task.
func newRun(id string, ticket int) *api.Run {
	return &api.Run{
		ID:         id,
		Task:       api.TaskImplement,
		Attempt:    1,
		Goal:       40,
		Ticket:     ticket,
		Mode:       api.ModeUnattended,
		Method:     api.MethodBuiltin,
		MethodUsed: api.MethodBuiltin,
		Trigger:    api.TriggerRef{Kind: "issueLabeled", Ref: "#42"},
		Phase:      api.PhaseQueued,
		CreatedAt:  t0,
	}
}
