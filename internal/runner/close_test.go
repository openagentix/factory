//go:build unix

package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/harness"
)

// openFDs counts the descriptors this process has open, by asking about each
// number. It opens nothing itself.
func openFDs(t *testing.T) int {
	t.Helper()
	n := 0
	for fd := 0; fd < 4096; fd++ {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil {
			n++
		}
	}
	return n
}

// With the collector off, a run must leave no descriptor open when it ends,
// whether it ran, failed to start, panicked in the callback, was cancelled
// or ran out of time. Nothing may wait for the collector.
func TestRunsLeaveNoDescriptorsOpen(t *testing.T) {
	old := debug.SetGCPercent(-1)
	t.Cleanup(func() { debug.SetGCPercent(old) })
	f := newFixture(t)
	cases := map[string]func() Spec{
		"good run": func() Spec { return f.spec(ownScript) },
		"start fails": func() Spec {
			s := f.spec(ownScript)
			s.Adapter = &harness.Stub{Program: filepath.Join(t.TempDir(), "missing")}
			return s
		},
		"callback panics": func() Spec {
			s := f.spec(`{"events":[{"kind":"status","text":"x"}],"result":` + resultA + `}`)
			s.OnEvent = func(apiEvent) { panic("boom") }
			return s
		},
		"deadline": func() Spec {
			s := f.spec(`{"delayMs":60000,"result":` + resultA + `}`)
			s.Deadline = 300 * time.Millisecond
			return s
		},
		"failed exit": func() Spec { return f.spec(`{"exitCode":2,"result":` + resultA + `}`) },
	}
	const n = 5
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			run := func() {
				h, err := NewLocal().Start(context.Background(), mk())
				if err != nil {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if _, err := h.Wait(ctx); err != nil {
					t.Fatal(err)
				}
			}
			run() // warm up anything that opens once
			before := openFDs(t)
			for i := 0; i < n; i++ {
				run()
			}
			after := openFDs(t)
			t.Logf("before %d after %d", before, after)
			if after > before {
				t.Fatalf("%d descriptors were left open by %d runs", after-before, n)
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		run := func() {
			h, err := NewLocal().Start(context.Background(), f.spec(`{"delayMs":60000,"result":`+resultA+`}`))
			if err != nil {
				t.Fatal(err)
			}
			h.Cancel(context.Background())
		}
		run()
		before := openFDs(t)
		for i := 0; i < n; i++ {
			run()
		}
		if after := openFDs(t); after > before {
			t.Fatalf("%d descriptors were left open", after-before)
		}
	})
	t.Run("prepare fails", func(t *testing.T) {
		run := func() {
			s := f.spec(ownScript)
			s.Commit = "no-such-commit"
			if _, err := NewLocal().Start(context.Background(), s); err == nil {
				t.Fatal("Start must fail")
			}
		}
		run()
		before := openFDs(t)
		for i := 0; i < n; i++ {
			run()
		}
		if after := openFDs(t); after > before {
			t.Fatalf("%d descriptors were left open", after-before)
		}
	})
}

// A kept run's events and stderr paths are the agent's to replace. The runner
// says so when they are not regular files.
func TestKeptLogPathsThatAreNotRegularFilesGetANote(t *testing.T) {
	for _, which := range []string{"events.jsonl", "stderr.log", "missing"} {
		t.Run(which, func(t *testing.T) {
			f := newFixture(t)
			spec := f.spec(`{"exitCode":1,"result":` + resultA + `}`)
			spec.KeepFailed = true
			beforeKeep = func(r *run) {
				switch which {
				case "missing":
					os.Remove(filepath.Join(r.dir, "stderr.log"))
				default:
					p := filepath.Join(r.dir, which)
					os.Remove(p)
					os.Symlink("/etc/hostname", p)
				}
			}
			t.Cleanup(func() { beforeKeep = nil })
			o := f.do(spec)
			if o.RunDir == "" {
				t.Fatal("the run was not kept")
			}
			if !strings.Contains(strings.Join(o.Notes, "\n"), "not a regular file") {
				t.Errorf("no note about the replaced log: %v", o.Notes)
			}
		})
	}
	t.Run("untouched", func(t *testing.T) {
		f := newFixture(t)
		spec := f.spec(`{"exitCode":1,"result":` + resultA + `}`)
		spec.KeepFailed = true
		o := f.do(spec)
		if strings.Contains(strings.Join(o.Notes, "\n"), "not a regular file") {
			t.Errorf("a false note: %v", o.Notes)
		}
	})
}
