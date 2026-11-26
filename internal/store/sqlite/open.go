// Package sqlite is the SQLite implementation of store.Store. It uses a pure
// Go driver, so the build needs no C toolchain.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/openagentix/factory/internal/store"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	sql     string
}

var migrations = loadMigrations()

// latestVersion is the schema version this program writes.
var latestVersion = migrations[len(migrations)-1].version

// loadMigrations reads the embedded files. A file is named NNN_name.sql and
// NNN is its version. Versions must run 1, 2, 3 and so on.
func loadMigrations() []migration {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		panic(err)
	}
	var list []migration
	for _, e := range entries {
		num, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if err != nil {
			panic(fmt.Sprintf("migration %q has no version prefix", e.Name()))
		}
		body, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			panic(err)
		}
		list = append(list, migration{version: v, sql: string(body)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	for i, m := range list {
		if m.version != i+1 {
			panic(fmt.Sprintf("migration versions must run from 1 without gaps, got %d at %d", m.version, i+1))
		}
	}
	return list
}

// Store is a SQLite backed store.Store.
type Store struct {
	db *sql.DB
	// keep is one connection held open for the life of the store. While it
	// is open the -wal and -shm files stay, so their modes stay as set.
	keep *sql.Conn
	now  func() time.Time
}

// Open opens the database file, makes it if needed, and upgrades the schema.
// A database made by a newer program fails with store.ErrNewerSchema.
func Open(ctx context.Context, path string) (*Store, error) {
	// The store holds run history, so only the owner may read it.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: make directory for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: create %s: %w", path, err)
	}
	f.Close()
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_txlock", "immediate")
	db, err := sql.Open("sqlite3", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	s := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if s.keep, err = db.Conn(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	// The driver makes the side files with the default mode. Narrow them.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.Close()
			return nil, fmt.Errorf("store: protect %s: %w", path+suffix, err)
		}
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s.keep != nil {
		s.keep.Close()
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer tx.Rollback()
	var have int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&have); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if have > latestVersion {
		return fmt.Errorf("%w: database is version %d, this program knows version %d", store.ErrNewerSchema, have, latestVersion)
	}
	for _, m := range migrations[have:] {
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("store: migration %d: %w", m.version, err)
		}
	}
	if have < latestVersion {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", latestVersion)); err != nil {
			return fmt.Errorf("store: set schema version: %w", err)
		}
	}
	return tx.Commit()
}
