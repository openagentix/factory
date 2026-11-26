package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

// timeFormat is fixed width UTC, so text order is time order.
const timeFormat = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// encodeRun returns the stored form of a run: the JSON body and the run as
// read back from it. The body is parsed again with the api parser, so a run
// is stored only when it is valid. Times are made UTC on a copy.
func encodeRun(r *api.Run) ([]byte, *api.Run, error) {
	if r == nil {
		return nil, nil, errors.New("store: run is nil")
	}
	c := *r
	c.CreatedAt = c.CreatedAt.UTC()
	if c.StartedAt != nil {
		t := c.StartedAt.UTC()
		c.StartedAt = &t
	}
	if c.FinishedAt != nil {
		t := c.FinishedAt.UTC()
		c.FinishedAt = &t
	}
	body, err := json.Marshal(&c)
	if err != nil {
		return nil, nil, fmt.Errorf("store: encode run: %w", err)
	}
	parsed, err := api.ParseRun(body)
	if err != nil {
		return nil, nil, fmt.Errorf("store: %w", err)
	}
	return body, parsed, nil
}

// reachedWork reports whether a phase can only be reached through the work
// stage. Failed and cancelled can also come from queued or held.
func reachedWork(r *api.Run) bool {
	switch r.Phase {
	case api.PhaseWorking, api.PhaseVerifying, api.PhasePublishing, api.PhaseNeedsHuman, api.PhaseSucceeded:
		return true
	case api.PhaseFailed, api.PhaseCancelled:
		return r.StartedAt != nil
	}
	return false
}

// CreateRun validates and stores a new run.
func (s *Store) CreateRun(ctx context.Context, r *api.Run) error {
	body, p, err := encodeRun(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO runs (id, task, ticket, goal, phase, created_at, started_at, finished_at, worked, body)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Task, p.Ticket, p.Goal, string(p.Phase), formatTime(p.CreatedAt),
		formatTimePtr(p.StartedAt), formatTimePtr(p.FinishedAt), reachedWork(p), string(body))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return fmt.Errorf("%w: run %q", store.ErrExists, p.ID)
		}
		return fmt.Errorf("store: create run %q: %w", p.ID, err)
	}
	return nil
}

// GetRun returns one run.
func (s *Store) GetRun(ctx context.Context, id string) (*api.Run, error) {
	var body string
	err := s.db.QueryRowContext(ctx, "SELECT body FROM runs WHERE id = ?", id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: run %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get run %q: %w", id, err)
	}
	return decodeRun(body)
}

func decodeRun(body string) (*api.Run, error) {
	r, err := api.ParseRun([]byte(body))
	if err != nil {
		return nil, fmt.Errorf("store: stored run is not valid: %w", err)
	}
	return r, nil
}

// UpdateRun replaces a stored run. The phase may stay or follow an allowed
// transition. Task, goal, ticket and creation time stay as they were.
func (s *Store) UpdateRun(ctx context.Context, r *api.Run) error {
	body, p, err := encodeRun(r)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: update run %q: %w", p.ID, err)
	}
	defer tx.Rollback()
	var (
		phase, task, created string
		ticket, goal         int
		worked               bool
	)
	err = tx.QueryRowContext(ctx, "SELECT phase, task, ticket, goal, created_at, worked FROM runs WHERE id = ?", p.ID).
		Scan(&phase, &task, &ticket, &goal, &created, &worked)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: run %q", store.ErrNotFound, p.ID)
	}
	if err != nil {
		return fmt.Errorf("store: update run %q: %w", p.ID, err)
	}
	if task != p.Task || ticket != p.Ticket || goal != p.Goal || created != formatTime(p.CreatedAt) {
		return fmt.Errorf("store: run %q: task, goal, ticket and createdAt cannot change", p.ID)
	}
	if from := api.Phase(phase); from != p.Phase && !api.CanTransition(from, p.Phase) {
		return fmt.Errorf("%w: run %q from %s to %s", store.ErrBadTransition, p.ID, from, p.Phase)
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE runs SET phase = ?, started_at = ?, finished_at = ?, worked = ?, body = ? WHERE id = ?`,
		string(p.Phase), formatTimePtr(p.StartedAt), formatTimePtr(p.FinishedAt),
		worked || reachedWork(p), string(body), p.ID)
	if err != nil {
		return fmt.Errorf("store: update run %q: %w", p.ID, err)
	}
	return tx.Commit()
}
