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
	"sort"
	"strings"
	"sync"
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

// runCommand is "oxf run". Only --demo is built.
func runCommand(args []string, stdout, stderr io.Writer) int {
	demoMode, once := false, false
	for _, a := range args {
		switch a {
		case "--demo":
			demoMode = true
		case "--once":
			once = true
		default:
			fmt.Fprintf(stderr, "oxf run: unknown argument %q. Usage: oxf run --demo [--once]\n", a)
			return 2
		}
	}
	if !demoMode {
		fmt.Fprintln(stderr, "oxf run: not built yet. Only 'oxf run --demo' works now.")
		return 2
	}
	prog, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stdout, "DEMO cannot find its own program, so it cannot start the stub agent.")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// The first signal stops the demo cleanly. A second one is not caught.
	go func() { <-ctx.Done(); stop() }()
	defer stop()
	return runDemo(ctx, demoOptions{Once: once, Program: prog, Port: demoPort, Terminal: isTerminal(os.Stderr),
		Dwell: demoDwell, BeatDelay: demoBeatDelay, Stdout: stdout, Stderr: stderr})
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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

// runDemo runs the demo until ctx ends, or with Once to the waiting point of
// the story. Exit codes: 0 for a clean stop, 1 when it cannot start.
func runDemo(ctx context.Context, o demoOptions) int {
	out := &prefixWriter{w: o.Stdout}
	errw := &prefixWriter{w: o.Stderr}
	say := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }

	dir, err := os.MkdirTemp(o.TempRoot, "oxf-demo-")
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
	if _, err := server.DeliverLink(errw, o.Terminal, dir, srv.Link()); err != nil {
		_ = ln.Close()
		say("Cannot hand over the link to the web app.")
		return 1
	}
	defer func() { _ = server.RemoveLinkFile(dir) }()

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx, ln) }()

	d, err := demo.New(ctx, demo.Config{DataDir: dir, Program: o.Program, Dwell: o.Dwell, BeatDelay: o.BeatDelay, Hub: hub})
	var play sync.WaitGroup
	if err != nil {
		if ctx.Err() == nil {
			say("Cannot build the demo: %v", err)
		}
		_ = ln.Close()
		<-served
		return map[bool]int{true: 0, false: 1}[ctx.Err() != nil]
	}
	lazy.set(d)
	say("The demo line is ready. Open the link. The story runs for about a minute, then waits for you.")
	play.Add(1)
	go func() {
		defer play.Done()
		if err := d.Play(ctx); err != nil && ctx.Err() == nil {
			say("The story stopped: %v", err)
		} else if err == nil {
			say("The story is at its waiting point. It will not answer for you.")
		}
	}()
	<-ctx.Done()
	say("Stopping.")
	<-served
	play.Wait()
	d.Close()
	return 0
}

// runOnce runs the story to its waiting point with no delays, prints what is
// on the line and returns.
func runOnce(ctx context.Context, o demoOptions, dir string, say func(string, ...any)) int {
	d, err := demo.New(ctx, demo.Config{DataDir: dir, Program: o.Program})
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		say("Cannot build the demo: %v", err)
		return 1
	}
	defer d.Close()
	if err := d.Play(ctx); err != nil {
		if ctx.Err() != nil {
			return 0
		}
		say("The story stopped: %v", err)
		return 1
	}
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
