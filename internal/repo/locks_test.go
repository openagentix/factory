package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The wait for a lock file is short in tests. The default is checked below.
func TestMain(m *testing.M) {
	lockWait = 300 * time.Millisecond
	os.Exit(m.Run())
}

func TestLockWaitDefault(t *testing.T) {
	if defaultLockWait != 2*time.Second {
		t.Errorf("default wait = %v", defaultLockWait)
	}
}

func touch(t *testing.T, p string) {
	t.Helper()
	put(t, p, "", 0o644)
}

func TestGuard_ALockThatGoesAwayWithinTheWaitPasses(t *testing.T) {
	tr := newTrapRepo(t)
	lock := filepath.Join(tr.dir, "HEAD.lock")
	touch(t, lock)
	go func() { time.Sleep(60 * time.Millisecond); os.Remove(lock) }()
	if err := checkTrusted(context.Background(), tr.dir); err != nil {
		t.Fatalf("err = %v", err)
	}
	if TrustOf(nil) != Trusted {
		t.Error("nil must be trusted")
	}
}

func TestGuard_ALockThatStaysIsAStaleLockError(t *testing.T) {
	tr := newTrapRepo(t)
	files := []string{"HEAD.lock", "config.lock", "packed-refs.lock", "gc.pid", "objects/maintenance.lock", "refs/heads/master.lock"}
	for _, f := range files {
		touch(t, filepath.Join(tr.dir, filepath.FromSlash(f)))
	}
	start := time.Now()
	err := checkTrusted(context.Background(), tr.dir)
	var sl *StaleLockError
	if !errors.As(err, &sl) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if time.Since(start) < lockWait {
		t.Error("it did not wait")
	}
	for _, f := range files {
		found := false
		for _, g := range sl.Files {
			found = found || g == f
		}
		if !found {
			t.Errorf("%s not listed in %v", f, sl.Files)
		}
		if _, err := os.Stat(filepath.Join(tr.dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s was deleted", f)
		}
	}
	for _, want := range []string{"lock file was left behind", "git process", "remove"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	if TrustOf(err) != StaleLock {
		t.Errorf("TrustOf = %v", TrustOf(err))
	}
	// Every method reports it the same way, and runs nothing.
	for name, e := range allMethods(t, tr) {
		if TrustOf(e) != StaleLock {
			t.Errorf("%s: %v", name, e)
		}
	}
}

func TestGuard_ALockBesideAnotherProblemIsNotTrusted(t *testing.T) {
	tr := newTrapRepo(t)
	touch(t, filepath.Join(tr.dir, "HEAD.lock"))
	tr.git("config", "foo.bar", "x")
	err := checkTrusted(context.Background(), tr.dir)
	if err == nil || TrustOf(err) != NotTrusted || !strings.Contains(err.Error(), "foo.bar") {
		t.Errorf("err = %v", err)
	}
	// Also with an offending entry that is not a config key.
	tr2 := newTrapRepo(t)
	touch(t, filepath.Join(tr2.dir, "config.lock"))
	touch(t, filepath.Join(tr2.dir, "surprise"))
	if err := checkTrusted(context.Background(), tr2.dir); TrustOf(err) != NotTrusted {
		t.Errorf("err = %v", err)
	}
}

func TestGuard_LockLookingNamesInOtherPlacesAreNotTrusted(t *testing.T) {
	for name, setup := range map[string]func(tr *trapRepo){
		"unknown top-level":          func(tr *trapRepo) { touch(t, filepath.Join(tr.dir, "evil.lock")) },
		"in info":                    func(tr *trapRepo) { touch(t, filepath.Join(tr.dir, "info", "x.lock")) },
		"in objects":                 func(tr *trapRepo) { touch(t, filepath.Join(tr.dir, "objects", "ab.lock")) },
		"in hooks, executable":       func(tr *trapRepo) { put(t, filepath.Join(tr.dir, "hooks", "post.lock"), "#!/bin/sh\n", 0o755) },
		"a lock name as a directory": func(tr *trapRepo) { os.Mkdir(filepath.Join(tr.dir, "HEAD.lock"), 0o755) },
		"a lock name as a link": func(tr *trapRepo) {
			os.Symlink("/etc/passwd", filepath.Join(tr.dir, "config.lock"))
		},
		"a link named like a ref lock": func(tr *trapRepo) {
			os.Symlink("/etc/passwd", filepath.Join(tr.dir, "refs", "heads", "x.lock"))
		},
		"maintenance lock as a link": func(tr *trapRepo) {
			os.Symlink("/etc/passwd", filepath.Join(tr.dir, "objects", "maintenance.lock"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTrapRepo(t)
			setup(tr)
			err := checkTrusted(context.Background(), tr.dir)
			if err == nil || TrustOf(err) != NotTrusted {
				t.Errorf("err = %v, trust = %v", err, TrustOf(err))
			}
		})
	}
}
