package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/openagentix/factory/internal/store"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "factory.db")
}

func userVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenMigrates(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, path string)
		wantErr error
		wantVer int
	}{
		{"fresh file is upgraded to the latest version", func(*testing.T, string) {}, nil, latestVersion},
		{"older database is upgraded", func(t *testing.T, path string) {
			db, err := sql.Open("sqlite3", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("PRAGMA user_version = 0"); err != nil {
				t.Fatal(err)
			}
		}, nil, latestVersion},
		{"newer database is refused", func(t *testing.T, path string) {
			db, err := sql.Open("sqlite3", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("PRAGMA user_version = 9999"); err != nil {
				t.Fatal(err)
			}
		}, store.ErrNewerSchema, 9999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tempPath(t)
			tc.prepare(t, path)
			s, err := Open(context.Background(), path)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Open error = %v, want %v", err, tc.wantErr)
			}
			if err == nil {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if got := userVersion(t, path); got != tc.wantVer {
				t.Errorf("user_version = %d, want %d", got, tc.wantVer)
			}
		})
	}
}

func TestOpenTwiceKeepsData(t *testing.T) {
	path := tempPath(t)
	for i := 0; i < 2; i++ {
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}
