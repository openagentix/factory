package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

var errEmptyTask = errors.New("store: task name must not be empty")

// GetLevel returns the dial level of a task, or the default when none is set.
func (s *Store) GetLevel(ctx context.Context, task string) (api.Level, bool, error) {
	if task == "" {
		return 0, false, errEmptyTask
	}
	var level int
	err := s.db.QueryRowContext(ctx, "SELECT level FROM dial_levels WHERE task = ?", task).Scan(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return api.DefaultLevel, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: get level of %q: %w", task, err)
	}
	return api.Level(level), true, nil
}

// SetLevel stores a level at or below the ceiling and logs the change.
func (s *Store) SetLevel(ctx context.Context, task string, level, ceiling api.Level) error {
	if task == "" {
		return errEmptyTask
	}
	if err := api.CheckLevel(level, ceiling); err != nil {
		if level.Valid() && ceiling.Valid() {
			return fmt.Errorf("%w: task %q: %v", store.ErrAboveCeiling, task, err)
		}
		return fmt.Errorf("store: task %q: %w", task, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set level of %q: %w", task, err)
	}
	defer tx.Rollback()
	prev := int(api.DefaultLevel)
	err = tx.QueryRowContext(ctx, "SELECT level FROM dial_levels WHERE task = ?", task).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: set level of %q: %w", task, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO dial_levels (task, level) VALUES (?, ?)
		 ON CONFLICT (task) DO UPDATE SET level = excluded.level`, task, int(level)); err != nil {
		return fmt.Errorf("store: set level of %q: %w", task, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO dial_log (task, at, previous, level) VALUES (?, ?, ?, ?)",
		task, formatTime(s.now()), prev, int(level)); err != nil {
		return fmt.Errorf("store: log level of %q: %w", task, err)
	}
	return tx.Commit()
}

// LevelLog lists the changes to the level of a task, oldest first.
func (s *Store) LevelLog(ctx context.Context, task string) ([]store.LevelChange, error) {
	if task == "" {
		return nil, errEmptyTask
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT at, previous, level FROM dial_log WHERE task = ? ORDER BY seq", task)
	if err != nil {
		return nil, fmt.Errorf("store: level log of %q: %w", task, err)
	}
	defer rows.Close()
	out := []store.LevelChange{}
	for rows.Next() {
		var at string
		var prev, level int
		if err := rows.Scan(&at, &prev, &level); err != nil {
			return nil, fmt.Errorf("store: level log of %q: %w", task, err)
		}
		t, err := time.Parse(timeFormat, at)
		if err != nil {
			return nil, fmt.Errorf("store: level log of %q: bad time %q: %w", task, at, err)
		}
		out = append(out, store.LevelChange{Task: task, Time: t.UTC(), Previous: api.Level(prev), Level: api.Level(level)})
	}
	return out, rows.Err()
}
