package sqlite

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

// The record has no field for a credential. This test keeps it that way: a
// token-looking value in a free-text field is kept as given, because the
// store does not rewrite records, and a field that is not in the record is
// refused. The tables have no column for a secret.
func TestNoCredentialFields(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	const token = "ghp_0123456789abcdefghijklmnopqrstuvwxyz"

	r := newRun("r1", 1)
	r.Result = &api.Result{SchemaVersion: 1, Status: api.StatusSucceeded, Summary: "saw " + token,
		Harness: api.HarnessInfo{Name: "claude-code"}}
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetRun(ctx, "r1")
	if got.Result.Summary != "saw "+token {
		t.Errorf("a defined free-text field was changed: %q", got.Result.Summary)
	}

	// A credential field is not part of the record, so the api parser refuses
	// it, and so the store can never be handed one through a record.
	for _, field := range []string{"token", "secret", "password", "apiKey", "credential"} {
		body := fmt.Sprintf(`{"id":"x","%s":"%s"}`, field, token)
		if _, err := api.ParseRun([]byte(body)); err == nil {
			t.Errorf("record accepts an unknown %q field", field)
		}
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"token", "secret", "password", "key", "credential", "auth"} {
			if strings.Contains(strings.ToLower(column), bad) {
				t.Errorf("column %s.%s looks like a credential", table, column)
			}
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	const workers, perWorker = 8, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker*4)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("r-%d-%d", w, i)
				r := newRun(id, 7)
				if err := s.CreateRun(ctx, r); err != nil {
					errs <- err
					continue
				}
				r.Phase = api.PhaseWorking
				if err := s.UpdateRun(ctx, r); err != nil {
					errs <- err
				}
				if err := s.SetLevel(ctx, "implement", api.LevelGuided, api.LevelAutopilot); err != nil {
					errs <- err
				}
				if _, _, err := s.ListRuns(ctx, store.RunFilter{Ticket: 7}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n, _ := s.CountAttempts(ctx, 7); n != workers*perWorker {
		t.Errorf("attempts = %d, want %d", n, workers*perWorker)
	}
	if log, _ := s.LevelLog(ctx, "implement"); len(log) != workers*perWorker {
		t.Errorf("log has %d lines, want %d", len(log), workers*perWorker)
	}
}

func TestCancelledContext(t *testing.T) {
	s := openTemp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.CreateRun(ctx, newRun("r1", 1)); err == nil {
		t.Error("CreateRun ignored a cancelled context")
	}
}

var _ store.Store = (*Store)(nil)
