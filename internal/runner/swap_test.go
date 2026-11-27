package runner

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// swapIterations is how many runs each racing test makes. OXF_SWAP_ITER
// changes it.
func swapIterations() int {
	if v, err := strconv.Atoi(os.Getenv("OXF_SWAP_ITER")); err == nil && v > 0 {
		return v
	}
	return 300
}

// racer plays the agent, or a child it left behind, that swaps a path again
// and again while the runner works. It always puts the path back at the end
// of a turn.
type racer struct {
	stop chan struct{}
	wg   sync.WaitGroup
}

func startRacer(turn func()) *racer {
	r := &racer{stop: make(chan struct{})}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-r.stop:
				return
			default:
			}
			turn()
			runtime.Gosched()
		}
	}()
	return r
}

func (r *racer) end() { close(r.stop); r.wg.Wait() }

func linkTo(target string) func(string) error {
	return func(p string) error { return os.Symlink(target, p) }
}

// leaks lists what in the exported commit is not this run's own. It is safe
// to call from a goroutine.
func leaks(t *testing.T, f *fixture, commit string, secrets ...string) []string {
	dest := filepath.Join(t.TempDir(), "x")
	if _, err := f.repo.Export(testCtx(), commit, dest, repoLimits()); err != nil {
		return []string{"export: " + err.Error()}
	}
	var bad []string
	own := map[string]bool{"main.go": true, "mine.txt": true}
	filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dest {
			return nil
		}
		rel, _ := filepath.Rel(dest, p)
		if !own[rel] {
			bad = append(bad, rel)
		}
		if d.Type().IsRegular() {
			b, _ := os.ReadFile(p)
			for _, s := range secrets {
				if strings.Contains(string(b), s) {
					bad = append(bad, rel+" holds "+s)
				}
			}
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(dest, "mine.txt")); err != nil {
		bad = append(bad, "mine.txt missing")
	}
	return bad
}

const ownScript = `{"files":[{"path":"mine.txt","content":"mine"}],"result":` + resultA + `}`

// preparedRun makes a run up to the point where the agent would start, writes
// the agent's own file into the workspace, and returns it. The agent is not
// started: the tests below act as the agent and as the child it left behind.
func preparedRun(t *testing.T, f *fixture) *run {
	t.Helper()
	spec := f.spec(ownScript)
	r := &run{spec: spec, lim: spec.Limits.withDefaults()}
	if err := r.prepare(testCtx()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "workspace", "mine.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

// raceLoop imports a prepared run n times, spread over several workers, each
// with its own repository and its own victim run. A racer swaps a name all the
// while. An import may be refused. It may never carry foreign files.
func raceLoop(t *testing.T, n int, mkTurn func(b Outcome) func(r *run), secrets ...string) {
	t.Helper()
	workers := min(8, max(2, runtime.NumCPU()))
	type worker struct {
		f    *fixture
		r    *run
		turn func(r *run)
	}
	var ws []worker
	for i := 0; i < workers; i++ {
		f := newFixture(t)
		b := keptRun(f)
		if b.RunDir == "" {
			t.Fatal("run B was not kept")
		}
		r := preparedRun(t, f)
		t.Cleanup(func() { r.cleanup() })
		ws = append(ws, worker{f, r, mkTurn(b)})
	}
	var mu sync.Mutex
	leaked, refused, done := 0, 0, 0
	var wg sync.WaitGroup
	for i, w := range ws {
		share := n / workers
		if i < n%workers {
			share++
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < share; j++ {
				rc := startRacer(func() { w.turn(w.r) })
				var in verdictInput
				commit, _, early := w.r.importWorkspace(testCtx(), &in)
				rc.end()
				var bad []string
				no := commit == "" || early != "" || in.ImportErr != nil
				if !no {
					bad = leaks(t, w.f, commit, secrets...)
				}
				mu.Lock()
				done++
				switch {
				case no:
					refused++
				case len(bad) > 0:
					leaked++
					t.Errorf("import leaked: %v", bad)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	t.Logf("iterations %d, leaked %d, refused %d", done, leaked, refused)
}

// swapHeld puts the fake in place for a short time and the real directory back
// for longer, so the runner's own checks mostly see the real one and the
// moment of opening a path can still land on the fake.
func swapHeld(path string, put func(string) error) {
	moved := path + ".moved"
	if os.Rename(path, moved) != nil {
		return
	}
	_ = put(path)
	time.Sleep(60 * time.Microsecond)
	os.RemoveAll(path)
	os.Rename(moved, path)
	time.Sleep(100 * time.Microsecond)
}

func workspaceFake(mode, target string) func(string) error {
	if mode == "link" {
		return linkTo(target)
	}
	return func(p string) error {
		if err := os.Mkdir(p, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(p, "evil.txt"), []byte("EVIL"), 0o600)
	}
}

// Attack 1, free running: the run directory's name is swapped for a link to
// another run all through the import.
func TestRacingRunDirSwapNeverLeaks(t *testing.T) {
	raceLoop(t, swapIterations(), func(b Outcome) func(r *run) {
		return func(r *run) { swapHeld(r.dir, linkTo(b.RunDir)) }
	}, "B")
}

// Attack 2, free running: only the workspace name is swapped, for a link to
// another run's workspace, or for a different real directory.
func TestRacingWorkspaceSwapNeverLeaks(t *testing.T) {
	for _, mode := range []string{"link", "real"} {
		t.Run(mode, func(t *testing.T) {
			raceLoop(t, swapIterations(), func(b Outcome) func(r *run) {
				put := workspaceFake(mode, filepath.Join(b.RunDir, "workspace"))
				return func(r *run) { swapHeld(filepath.Join(r.dir, "workspace"), put) }
			}, "B", "EVIL")
		})
	}
}

// The same attacks through whole runs, with a racer that starts when the
// agent has ended. Fewer iterations, because each one starts a process.
func TestRacingSwapWholeRunsNeverLeak(t *testing.T) {
	f := newFixture(t)
	b := keptRun(f)
	if b.RunDir == "" {
		t.Fatal("run B was not kept")
	}
	n := min(swapIterations()/10, 20)
	leaked := 0
	for i := 0; i < n; i++ {
		var rc *racer
		beforeImport = func(r *run) {
			if i%2 == 0 {
				rc = startRacer(func() { swapHeld(r.dir, linkTo(b.RunDir)) })
			} else {
				rc = startRacer(func() { swapHeld(filepath.Join(r.dir, "workspace"), workspaceFake("real", "")) })
			}
		}
		o := f.doWithin(f.spec(ownScript), 30*time.Second)
		beforeImport = nil
		if rc != nil {
			rc.end()
		}
		if o.Failed() {
			if o.Commit != "" {
				t.Fatalf("iteration %d: a failed run has a commit", i)
			}
			continue
		}
		if bad := leaks(t, f, o.Commit, "B", "EVIL"); len(bad) > 0 {
			leaked++
			t.Logf("iteration %d leaked: %v", i, bad)
		}
	}
	if leaked > 0 {
		t.Fatalf("%d of %d runs had foreign files in the commit", leaked, n)
	}
}

// Deterministic forms of attack 1 and 2. The swap happens just after the
// runner's own checks. "stays" is never undone. "flash" is undone after a
// short time, so a check made after the import sees nothing wrong, which is
// what a careful attacker does. The held handles decide, so the commit is this
// run's own files.
func TestSwapAfterTheChecksStillImportsOnlyTheHeldDirectory(t *testing.T) {
	type swap struct {
		do      func(t *testing.T, r *run, b Outcome)
		restore func(r *run)
	}
	cases := map[string]swap{
		"run directory to another run stays": {do: func(t *testing.T, r *run, b Outcome) {
			must(t, os.Rename(r.dir, r.dir+".moved"))
			must(t, os.Symlink(b.RunDir, r.dir))
		}},
		"run directory to another run flash": {do: func(t *testing.T, r *run, b Outcome) {
			must(t, os.Rename(r.dir, r.dir+".moved"))
			must(t, os.Symlink(b.RunDir, r.dir))
		}, restore: func(r *run) {
			os.Remove(r.dir)
			os.Rename(r.dir+".moved", r.dir)
		}},
		"workspace to another run stays": {do: func(t *testing.T, r *run, b Outcome) {
			ws := filepath.Join(r.dir, "workspace")
			must(t, os.Rename(ws, ws+".moved"))
			must(t, os.Symlink(filepath.Join(b.RunDir, "workspace"), ws))
		}},
		"workspace to another run flash": {do: func(t *testing.T, r *run, b Outcome) {
			ws := filepath.Join(r.dir, "workspace")
			must(t, os.Rename(ws, ws+".moved"))
			must(t, os.Symlink(filepath.Join(b.RunDir, "workspace"), ws))
		}, restore: func(r *run) {
			ws := filepath.Join(r.dir, "workspace")
			os.Remove(ws)
			os.Rename(ws+".moved", ws)
		}},
		"workspace to a real directory stays": {do: func(t *testing.T, r *run, b Outcome) {
			ws := filepath.Join(r.dir, "workspace")
			must(t, os.Rename(ws, ws+".moved"))
			must(t, workspaceFake("real", "")(ws))
		}},
		"workspace to a real directory flash": {do: func(t *testing.T, r *run, b Outcome) {
			ws := filepath.Join(r.dir, "workspace")
			must(t, os.Rename(ws, ws+".moved"))
			must(t, workspaceFake("real", "")(ws))
		}, restore: func(r *run) {
			ws := filepath.Join(r.dir, "workspace")
			os.RemoveAll(ws)
			os.Rename(ws+".moved", ws)
		}},
	}
	for name, sw := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			b := keptRun(f)
			var wg sync.WaitGroup
			beforeImport = func(r *run) {
				sw.do(t, r, b)
				if sw.restore != nil {
					wg.Add(1)
					go func() {
						defer wg.Done()
						time.Sleep(40 * time.Millisecond)
						sw.restore(r)
					}()
				}
			}
			t.Cleanup(func() { beforeImport = nil })
			o := f.doWithin(f.spec(ownScript), 30*time.Second)
			wg.Wait()
			if o.Failed() {
				if o.Commit != "" {
					t.Fatalf("a failed run has a commit: %+v", o)
				}
				return // refusing is allowed
			}
			if bad := leaks(t, f, o.Commit, "B", "EVIL"); len(bad) > 0 {
				t.Fatalf("foreign content in the commit: %v", bad)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Attack 4: links inside the workspace that point out of it.
func TestLinksInsideTheWorkspaceAreNotFollowed(t *testing.T) {
	f := newFixture(t)
	b := keptRun(f)
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "PRIVATE")
	beforeImport = func(r *run) {
		ws := filepath.Join(r.dir, "workspace")
		must(t, os.Symlink(filepath.Join(b.RunDir, "workspace", "b-secret.txt"), filepath.Join(ws, "to-run.txt")))
		must(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(ws, "to-abs.txt")))
		must(t, os.Symlink(outside, filepath.Join(ws, "to-dir")))
		must(t, os.Symlink("../../../../etc/passwd", filepath.Join(ws, "to-rel.txt")))
	}
	t.Cleanup(func() { beforeImport = nil })
	o := f.doWithin(f.spec(ownScript), 30*time.Second)
	if !o.Failed() {
		dest := exportTo(t, f, o.Commit)
		filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if bb, _ := os.ReadFile(p); strings.Contains(string(bb), "PRIVATE") || string(bb) == "B" {
					t.Errorf("%s holds foreign content", p)
				}
			}
			return nil
		})
	} else if o.Commit != "" {
		t.Errorf("a failed run has a commit")
	}
	if bb, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(bb) != "PRIVATE" {
		t.Errorf("a file outside was changed")
	}
}

// Attack 5: the agent swaps workspace for a link and then says it succeeded.
func TestWorkspaceLinkWithSucceededClaimNeverCarriesForeignContent(t *testing.T) {
	f := newFixture(t)
	b := keptRun(f)
	o := f.doWithin(f.spec(`{"files":[{"path":"mine.txt","content":"mine"}],"replaceWorkspace":"link","workspaceTarget":"`+filepath.Join(b.RunDir, "workspace")+`","result":`+resultA+`}`), 30*time.Second)
	if o.Failed() {
		if o.Commit != "" {
			t.Fatalf("failed run with a commit: %+v", o)
		}
		return
	}
	if bad := leaks(t, f, o.Commit, "B"); len(bad) > 0 {
		t.Fatalf("foreign content in a successful run: %v", bad)
	}
}

// Attack 6: the held directory is gone, closed or missing.
func TestHeldWorkspaceGoneFailsTheRun(t *testing.T) {
	cases := map[string]func(t *testing.T, r *run){
		"agent removed its workspace": func(t *testing.T, r *run) {
			must(t, os.RemoveAll(filepath.Join(r.dir, "workspace")))
		},
		"agent emptied and removed it by rename": func(t *testing.T, r *run) {
			ws := filepath.Join(r.dir, "workspace")
			must(t, os.Rename(ws, filepath.Join(t.TempDir(), "gone")))
		},
		"root closed": func(t *testing.T, r *run) {
			if r.wsRoot != nil {
				r.wsRoot.Close()
			}
		},
		"root nil": func(t *testing.T, r *run) { r.wsRoot = nil },
	}
	for name, act := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			beforeImport = func(r *run) { act(t, r) }
			t.Cleanup(func() { beforeImport = nil })
			o := f.doWithin(f.spec(ownScript), 30*time.Second)
			requireFailed(t, o, name)
			if o.Status == StatusSucceededNoChange || o.Status == StatusSucceeded {
				t.Fatalf("status %v", o.Status)
			}
		})
	}
}

// Attack 3: after the agent starts, the runner never imports or exports by
// path. Guard against someone putting it back.
func TestRunnerNeverUsesPathImportOrExport(t *testing.T) {
	src, err := os.ReadFile("local.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Repo.Import(", "Repo.Export(", "OpenRoot(r.ws)", "OpenRoot(r.dir"} {
		if strings.Contains(string(src), bad) {
			t.Errorf("local.go uses %s", bad)
		}
	}
}
