package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

// ListRuns returns the runs that match the filter, oldest first.
func (s *Store) ListRuns(ctx context.Context, f store.RunFilter) ([]*api.Run, []string, error) {
	var (
		where []string
		args  []any
	)
	if f.Ticket != 0 {
		where, args = append(where, "ticket = ?"), append(args, f.Ticket)
	}
	if f.Goal != 0 {
		where, args = append(where, "goal = ?"), append(args, f.Goal)
	}
	if f.Phase != "" {
		if !f.Phase.Valid() {
			return nil, nil, fmt.Errorf("store: %q is not a run phase", f.Phase)
		}
		where, args = append(where, "phase = ?"), append(args, string(f.Phase))
	}
	if f.Task != "" {
		where, args = append(where, "task = ?"), append(args, f.Task)
	}
	q := "SELECT id, body FROM runs"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at, id"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("store: list runs: %w", err)
	}
	defer rows.Close()
	runs := []*api.Run{}
	var damaged []string
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, nil, fmt.Errorf("store: list runs: %w", err)
		}
		r, err := decodeRun(id, body)
		if err != nil {
			damaged = append(damaged, id)
			continue
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("store: list runs: %w", err)
	}
	return runs, damaged, nil
}

// CountAttempts counts runs of the tasks that share a ticket's attempts and
// that started their work stage. A run cut off by a restart is left out.
func (s *Store) CountAttempts(ctx context.Context, ticket int) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM runs
		 WHERE ticket = ? AND task IN (?, ?) AND worked = 1 AND cut_off = 0`,
		ticket, api.TaskImplement, api.TaskFixCI).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count attempts for ticket %d: %w", ticket, err)
	}
	return n, nil
}

// FailInterrupted ends the runs a previous process left in the middle of
// work. Queued and held runs are not touched.
func (s *Store) FailInterrupted(ctx context.Context) ([]string, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("store: fail interrupted runs: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT id, body FROM runs WHERE phase IN (?, ?, ?) ORDER BY created_at, id`,
		string(api.PhaseWorking), string(api.PhaseVerifying), string(api.PhasePublishing))
	if err != nil {
		return nil, nil, fmt.Errorf("store: fail interrupted runs: %w", err)
	}
	var (
		open    []*api.Run
		damaged []string
	)
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("store: fail interrupted runs: %w", err)
		}
		r, err := decodeRun(id, body)
		if err != nil {
			damaged = append(damaged, id)
			continue
		}
		open = append(open, r)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("store: fail interrupted runs: %w", err)
	}
	now := s.now().UTC()
	marked := []string{}
	for _, r := range open {
		end := latest(now, r.CreatedAt, r.StartedAt)
		r.Phase = api.PhaseFailed
		r.FinishedAt = &end
		body, p, err := encodeRun(r)
		if err != nil {
			return nil, nil, err
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE runs SET phase = ?, finished_at = ?, cut_off = 1, body = ? WHERE id = ?`,
			string(p.Phase), formatTime(end), string(body), p.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("store: fail interrupted run %q: %w", p.ID, err)
		}
		marked = append(marked, p.ID)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("store: fail interrupted runs: %w", err)
	}
	return marked, damaged, nil
}

// latest returns the latest of now, the creation time and the start time, so
// a run never finishes before it began, even if the clock moved back.
func latest(now, created time.Time, started *time.Time) time.Time {
	out := now
	if created.After(out) {
		out = created
	}
	if started != nil && started.After(out) {
		out = *started
	}
	return out.UTC()
}
