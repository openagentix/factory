package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/openagentix/factory/internal/demo"
	"github.com/openagentix/factory/internal/server"
	"github.com/openagentix/factory/web"
)

// demoPort is the default port of the demo. It is in the unprivileged range.
// Record 0004 leaves the number open ("Not settled"). There is no flag to
// change it: a busy port stops the start.
const demoPort = 7477

// Delays of the story at normal speed. The whole story is visible in about a
// minute. With --once there are none.
const (
	demoDwell     = 700 * time.Millisecond
	demoBeatDelay = 1500 * time.Millisecond
)

// commandEnv is for tests. The command line has no flag for any of it.
type commandEnv struct {
	Port     int
	TempRoot string
	Program  string
	// Delays says that Dwell and BeatDelay are used as they are, also at zero.
	Delays           bool
	Dwell, BeatDelay time.Duration
}

// runCommand is "oxf run". Only --demo is built.
func runCommand(args []string, stdout, stderr io.Writer) int {
	return runCommandWith(args, stdout, stderr, commandEnv{})
}

func runCommandWith(args []string, stdout, stderr io.Writer, env commandEnv) int {
	demoMode, once := false, false
	for _, a := range args {
		if a == "--demo" {
			demoMode = true
		}
	}
	for _, a := range args {
		switch a {
		case "--demo":
		case "--once":
			once = true
		default:
			w := stderr
			if demoMode {
				w = &prefixWriter{w: stderr} // every line of a demo starts with DEMO
			}
			fmt.Fprintf(w, "oxf run: unknown argument %q. Usage: oxf run --demo [--once]\n", a)
			return 2
		}
	}
	if !demoMode {
		fmt.Fprintln(stderr, "oxf run: not built yet. Only 'oxf run --demo' works now.")
		return 2
	}
	prog := env.Program
	if prog == "" {
		var err error
		if prog, err = os.Executable(); err != nil {
			fmt.Fprintln(&prefixWriter{w: stdout}, "The demo cannot find its own program, so it cannot start the stub agent.")
			return 1
		}
	}
	port := env.Port
	if port == 0 {
		port = demoPort
	}
	dwell, delay := demoDwell, demoBeatDelay
	if env.Delays {
		dwell, delay = env.Dwell, env.BeatDelay
	}
	// A closed output pipe must give an error from the write, not kill the
	// process: the cleanup has to run. The signal is taken and dropped.
	pipe := make(chan os.Signal, 1)
	signal.Notify(pipe, syscall.SIGPIPE)
	defer signal.Stop(pipe)
	go func() {
		for range pipe {
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// The first signal stops the demo cleanly. A second one is not caught.
	go func() { <-ctx.Done(); stop() }()
	defer stop()
	return runDemo(ctx, demoOptions{Once: once, Program: prog, Port: port, TempRoot: env.TempRoot, Terminal: isTerminal(os.Stderr),
		Dwell: dwell, BeatDelay: delay, Stdout: stdout, Stderr: stderr})
}

// demoOptions is for tests. The command line has no flag for any of it.
type demoOptions struct {
	Once             bool
	Program          string
	Port             int
	TempRoot         string // where the data directory is made; empty means the system default
	Terminal         bool
	Dwell, BeatDelay time.Duration
	Stdout, Stderr   io.Writer
	// Progress is told of named events: "link", "ready", "beat:NAME" and
	// "waiting". It must return quickly. Tests use it to act at an event.
	Progress func(event string)
}

func (o demoOptions) progress(event string) {
	if o.Progress != nil {
		o.Progress(event)
	}
}

// prefixWriter puts "DEMO " before every line, so no line of the demo can
// look like something else.
type prefixWriter struct {
	mu   sync.Mutex
	w    io.Writer
	open bool // inside a line already
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, part := range strings.SplitAfter(string(b), "\n") {
		if part == "" {
			continue
		}
		if !p.open {
			if _, err := io.WriteString(p.w, "DEMO "); err != nil {
				return 0, err
			}
		}
		if _, err := io.WriteString(p.w, part); err != nil {
			return 0, err
		}
		p.open = !strings.HasSuffix(part, "\n")
	}
	return len(b), nil
}

// guardWriter turns a failed write into a stop request. After the first
// failure every write fails, so nothing is half printed.
type guardWriter struct {
	w      io.Writer
	failed func()
	broken atomic.Bool
}

func (g *guardWriter) Write(b []byte) (int, error) {
	if g.broken.Load() {
		return 0, errors.New("the output is closed")
	}
	n, err := g.w.Write(b)
	if err != nil {
		g.broken.Store(true)
		g.failed()
	}
	return n, err
}

// lazySource answers 503 until the demo is ready.
type lazySource struct {
	mu sync.RWMutex
	s  server.Source
}

func (l *lazySource) set(s server.Source) { l.mu.Lock(); l.s = s; l.mu.Unlock() }
func (l *lazySource) Floor(ctx context.Context) ([]byte, error) {
	l.mu.RLock()
	s := l.s
	l.mu.RUnlock()
	if s == nil {
		return nil, errors.New("the demo is not ready")
	}
	return s.Floor(ctx)
}

// removeAll removes a directory tree, also when a run left read-only folders.
func removeAll(dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

// Names of the data directories. A demo that serves holds a link file with a
// key, so an earlier one that was killed is cleaned at the next start. A
// --once run has a name of its own: it holds no key and binds no port, so
// another demo must never clean it away while it runs.
const (
	servedDirPrefix = "oxf-demo-"
	onceDirPrefix   = "oxf-demo-once-"
)

var servedDirName = regexp.MustCompile(`^oxf-demo-[0-9]+$`)

// removeStaleDemos removes what a killed demo left in root, and returns how
// many it removed. Only an entry that Lstat says is a real directory (never a
// link), owned by this user, with mode 0700 exactly and a name a serving demo
// makes, is removed. The directory of the caller (keep) is never touched. The
// caller must have bound the port first, so that no other serving demo is
// alive.
func removeStaleDemos(root, keep string) int {
	ents, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		name := e.Name()
		if !servedDirName.MatchString(name) || name == filepath.Base(keep) {
			continue
		}
		full := filepath.Join(root, name)
		fi, err := os.Lstat(full)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			continue
		}
		if fi.Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0o700 || !ownedByMe(fi) {
			continue
		}
		removeAll(full)
		if _, err := os.Lstat(full); err != nil {
			n++
		}
	}
	return n
}

// runDemo runs the demo until ctx ends, or with Once to the waiting point of
// the story. Exit codes (record 0003): 0 for a clean stop, and for --once only
// when the story completed. 1 when the start is refused, the story did not go
// as written, an output failed, or --once was stopped before it completed.
func runDemo(parent context.Context, o demoOptions) (code int) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var outBroken, errBroken atomic.Bool
	outG := &guardWriter{w: o.Stdout, failed: func() { outBroken.Store(true); cancel() }}
	errG := &guardWriter{w: o.Stderr, failed: func() { errBroken.Store(true); cancel() }}
	out := &prefixWriter{w: outG}
	errw := &prefixWriter{w: errG}
	say := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	defer func() {
		if code == 0 && (outG.broken.Load() || errG.broken.Load()) {
			code = 1 // an output that failed is a failure, whatever else happened
		}
	}()

	prefix := servedDirPrefix
	if o.Once {
		prefix = onceDirPrefix
	}
	dir, err := os.MkdirTemp(o.TempRoot, prefix)
	if err != nil {
		say("Cannot make a data directory: %v", err)
		return 1
	}
	// The directory goes away on every way out of this function.
	defer removeAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		say("Cannot protect the data directory.")
		return 1
	}
	say("Demo mode. A made-up project on a fake git host. No token is read and nothing leaves this machine.")

	if o.Once {
		return runOnce(ctx, o, dir, say)
	}

	hub := server.NewHub("demo")
	lazy := &lazySource{}
	srv, err := server.New(server.Config{Port: o.Port, Mode: "demo", Source: lazy, Static: web.FS, Hub: hub})
	if err != nil {
		say("Cannot start the web server: %v", err)
		return 1
	}
	ln, err := srv.Listen()
	if err != nil {
		say("Port %d is busy. Another demo may be running. Stop it first.", o.Port)
		return 1
	}
	// The port is ours, so no other serving demo is alive. What a killed one
	// left behind is removed now.
	root := o.TempRoot
	if root == "" {
		root = os.TempDir()
	}
	if n := removeStaleDemos(root, dir); n > 0 {
		say("Removed %d data directories that an earlier demo left behind.", n)
	}
	if _, err := server.DeliverLink(errw, o.Terminal, dir, srv.Link()); err != nil {
		_ = ln.Close()
		say("Cannot hand over the link to the web app.")
		return 1
	}
	defer func() { _ = server.RemoveLinkFile(dir) }()
	o.progress("link")

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx, ln) }()

	d, err := demo.New(ctx, demo.Config{DataDir: dir, Program: o.Program, Dwell: o.Dwell, BeatDelay: o.BeatDelay, Hub: hub,
		OnBeat: func(name string) { o.progress("beat:" + name) }})
	var play sync.WaitGroup
	if err != nil {
		if ctx.Err() == nil {
			say("Cannot build the demo: %v", err)
		}
		_ = ln.Close()
		<-served
		if ctx.Err() != nil {
			return 0
		}
		return 1
	}
	lazy.set(d)
	say("The demo line is ready. Open the link. The story runs for about a minute, then waits for you.")
	o.progress("ready")
	var storyFailed atomic.Bool
	play.Add(1)
	go func() {
		defer play.Done()
		switch err := d.Play(ctx); {
		case err == nil:
			say("The story is at its waiting point. It will not answer for you.")
			o.progress("waiting")
		case ctx.Err() != nil:
			// stopped by the person
		default:
			storyFailed.Store(true)
			say("The story did not go as written: %v. The screen shows what is true.", err)
		}
	}()
	<-ctx.Done()
	say("Stopping.")
	<-served
	play.Wait()
	d.Close()
	if storyFailed.Load() {
		return 1
	}
	return 0
}

// runOnce runs the story to its waiting point with no delays, prints what is
// on the line and returns. It returns 0 only when the story completed.
func runOnce(ctx context.Context, o demoOptions, dir string, say func(string, ...any)) int {
	d, err := demo.New(ctx, demo.Config{DataDir: dir, Program: o.Program, OnBeat: func(name string) { o.progress("beat:" + name) }})
	if err != nil {
		if ctx.Err() != nil {
			say("The run was stopped before the story ended, so it is not complete.")
			return 1
		}
		say("Cannot build the demo: %v", err)
		return 1
	}
	defer d.Close()
	if err := d.Play(ctx); err != nil {
		if ctx.Err() != nil {
			say("The run was stopped before the story ended, so it is not complete.")
			return 1
		}
		say("The story did not go as written: %v. The screen shows what is true.", err)
		if b, ferr := d.Floor(ctx); ferr == nil {
			summarize(b, say)
		}
		return 1
	}
	o.progress("waiting")
	b, err := d.Floor(ctx)
	if err != nil {
		say("Cannot read the line: %v", err)
		return 1
	}
	summarize(b, say)
	return 0
}

type summaryPiece struct {
	ID      string `json:"id"`
	Station int    `json:"station"`
	State   string `json:"state"`
	Reason  *struct {
		Code string `json:"code"`
	} `json:"reason"`
}

// summarize prints a short account of the line from a snapshot.
func summarize(b []byte, say func(string, ...any)) {
	var f struct {
		Signal struct {
			Lamp string `json:"lamp"`
			Word string `json:"word"`
		} `json:"signal"`
		Stations []struct {
			No     int    `json:"no"`
			Key    string `json:"key"`
			Source string `json:"source"`
			State  string `json:"state"`
		} `json:"stations"`
		Pieces []summaryPiece `json:"pieces"`
		Goals  []struct {
			Goal  int `json:"goal"`
			Lines []struct {
				ID    int    `json:"id"`
				State string `json:"state"`
			} `json:"lines"`
		} `json:"goals"`
		Calls []struct {
			Kind string `json:"kind"`
		} `json:"calls"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		say("summary: the snapshot could not be read")
		return
	}
	say("summary: the lamp is %s (%s).", f.Signal.Lamp, f.Signal.Word)
	for _, st := range f.Stations {
		say("summary: station %02d %s, %s, source %s.", st.No, st.Key, st.State, st.Source)
	}
	sort.SliceStable(f.Pieces, func(i, j int) bool { return f.Pieces[i].ID < f.Pieces[j].ID })
	for _, p := range f.Pieces {
		reason := ""
		if p.Reason != nil {
			reason = ", " + p.Reason.Code
		}
		say("summary: piece %s at station %02d is %s%s.", p.ID, p.Station, p.State, reason)
	}
	for _, g := range f.Goals {
		var parts []string
		for _, l := range g.Lines {
			parts = append(parts, fmt.Sprintf("line %d %s", l.ID, l.State))
		}
		if len(parts) > 0 {
			say("summary: goal %d: %s.", g.Goal, strings.Join(parts, ", "))
		}
	}
	kinds := map[string]int{}
	for _, c := range f.Calls {
		kinds[c.Kind]++
	}
	var ks []string
	for k, n := range kinds {
		ks = append(ks, fmt.Sprintf("%d %s", n, k))
	}
	sort.Strings(ks)
	say("summary: waiting for a person: %s.", strings.Join(ks, ", "))
}
