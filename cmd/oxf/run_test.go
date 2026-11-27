package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var oxfBinary string

// generous is the overall limit of every wait. A wait ends on an event; the
// limit only keeps a broken test from hanging.
const generous = 4 * time.Minute

func TestMain(m *testing.M) {
	if os.Getenv("OXF_TEST_HELPER") == "1" {
		os.Exit(helperMain())
	}
	dir, err := os.MkdirTemp("", "cmd-oxf-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	oxfBinary = filepath.Join(dir, "oxf")
	if out, err := exec.Command("go", "build", "-o", oxfBinary, "github.com/openagentix/factory/cmd/oxf").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building oxf: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// helperMain is "oxf run --demo" in a process of its own, with the real pipes
// and signals, a port and a temp root given by the test. It does what the
// command does and nothing else.
func helperMain() int {
	port, _ := strconv.Atoi(os.Getenv("OXF_TEST_PORT"))
	return runCommandWith([]string{"--demo"}, os.Stdout, os.Stderr, commandEnv{
		Port: port, TempRoot: os.Getenv("OXF_TEST_ROOT"), Program: os.Getenv("OXF_TEST_PROG"), Delays: true,
	})
}

// syncBuf is a buffer that is safe to use from two goroutines. A reader can
// wait for text to appear.
type syncBuf struct {
	mu      sync.Mutex
	b       bytes.Buffer
	changed chan struct{}
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.b.Write(p)
	if s.changed != nil {
		close(s.changed)
		s.changed = nil
	}
	return n, err
}

func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// waitFor returns when the text is in the buffer. It fails the test at the
// generous limit.
func (s *syncBuf) waitFor(t *testing.T, text string) {
	t.Helper()
	deadline := time.After(generous)
	for {
		s.mu.Lock()
		if strings.Contains(s.b.String(), text) {
			s.mu.Unlock()
			return
		}
		if s.changed == nil {
			s.changed = make(chan struct{})
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("waited for %q; have %q", text, s.String())
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func onlyDemoLines(t *testing.T, name, out string) {
	t.Helper()
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l != "" && !strings.HasPrefix(l, "DEMO") {
			t.Errorf("%s: a line does not start with DEMO: %q", name, l)
		}
	}
}

func rootEmpty(t *testing.T, name, root string) {
	t.Helper()
	if ents, _ := os.ReadDir(root); len(ents) != 0 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("%s: the data directory was not removed: %v", name, names)
	}
}

func TestRunWithoutDemoIsNotBuilt(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"run"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "not built yet") || strings.Count(strings.TrimSpace(errOut.String()), "\n") != 0 {
		t.Errorf("stderr %q", errOut.String())
	}
	if strings.HasPrefix(errOut.String(), "DEMO") {
		t.Error("the not built line has a DEMO prefix")
	}
}

func TestRunDemoUsage(t *testing.T) {
	for _, args := range [][]string{{"run", "--demo", "--port", "9"}, {"run", "--demo", "--once", "x"}, {"run", "--once"}, {"run", "--demo", "--bogus"}, {"run", "--bogus", "--demo"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
		hasDemo := false
		for _, a := range args {
			hasDemo = hasDemo || a == "--demo"
		}
		if hasDemo {
			onlyDemoLines(t, strings.Join(args, " "), out.String()+errOut.String())
			if !strings.HasPrefix(errOut.String(), "DEMO oxf run: unknown argument") {
				t.Errorf("%v: stderr %q", args, errOut.String())
			}
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--bogus"}, &out, &errOut); code != 2 || strings.HasPrefix(errOut.String(), "DEMO") {
		t.Errorf("without --demo: %d %q", code, errOut.String())
	}
}

// secretEnv holds tokens a demo must never read or print.
var secretEnv = map[string]string{"GITHUB_TOKEN": "tok-github-9f8e7d", "GH_TOKEN": "tok-gh-1a2b3c", "ANTHROPIC_API_KEY": "tok-anthropic-4d5e6f", "OPENAI_API_KEY": "tok-openai-7a8b9c"}

func TestRunDemoOnce(t *testing.T) {
	for k, v := range secretEnv {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	var out, errOut syncBuf
	code := runDemo(context.Background(), demoOptions{Once: true, Program: oxfBinary, TempRoot: root, Stdout: &out, Stderr: &errOut})
	if code != 0 {
		t.Fatalf("code %d\nout: %s\nerr: %s", code, out.String(), errOut.String())
	}
	all := out.String() + errOut.String()
	onlyDemoLines(t, "once", all)
	for _, v := range secretEnv {
		if strings.Contains(all, v) {
			t.Fatal("a token reached the output")
		}
	}
	if !strings.Contains(out.String(), "DEMO summary") || !strings.Contains(out.String(), "t3") {
		t.Errorf("no summary of the line:\n%s", out.String())
	}
	if strings.Contains(all, "#t=") {
		t.Error("--once printed a link")
	}
	rootEmpty(t, "once", root)
	noStrays(t)
}

// F5: work that fails for real is not a good end.
func TestRunDemoOnceExitsOneWhenTheStoryFails(t *testing.T) {
	failing, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no false program")
	}
	root := t.TempDir()
	var out, errOut syncBuf
	code := runDemo(context.Background(), demoOptions{Once: true, Program: failing, TempRoot: root, Stdout: &out, Stderr: &errOut})
	if code != 1 {
		t.Fatalf("code %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "did not go as written") || strings.Contains(out.String(), "waiting point") {
		t.Errorf("out: %s", out.String())
	}
	onlyDemoLines(t, "failing once", out.String()+errOut.String())
	rootEmpty(t, "failing once", root)
	noStrays(t)
}

// F6: --once that is stopped before the story ends did not complete.
func TestRunDemoOnceStoppedBeforeTheEndExitsOne(t *testing.T) {
	for _, at := range []string{"", "beat:write_checks", "beat:build_move", "beat:checks_fail"} {
		t.Run("at "+at, func(t *testing.T) {
			root := t.TempDir()
			var out, errOut syncBuf
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if at == "" {
				cancel() // stopped before the first beat
			}
			code := runDemo(ctx, demoOptions{Once: true, Program: oxfBinary, TempRoot: root, Stdout: &out, Stderr: &errOut,
				Progress: func(ev string) {
					if ev == at {
						cancel()
					}
				}})
			if code != 1 {
				t.Fatalf("code %d\n%s", code, out.String())
			}
			if !strings.Contains(out.String(), "stopped before the story ended") {
				t.Errorf("no DEMO line about the stop:\n%s", out.String())
			}
			onlyDemoLines(t, "once stopped", out.String()+errOut.String())
			rootEmpty(t, "once stopped", root)
			noStrays(t)
		})
	}
}

// served is one demo that serves, started in this process.
type served struct {
	t      *testing.T
	port   int
	root   string
	out    syncBuf
	errOut syncBuf
	cancel context.CancelFunc
	done   chan int
}

// startServed starts a serving demo. The port is taken from the system and
// handed to the demo, and when it is busy by then the start is repeated with
// another one. oxf itself never picks another port.
func startServed(t *testing.T, root, program string, progress func(string, context.CancelFunc)) *served {
	t.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		s := &served{t: t, port: freePort(t), root: root, done: make(chan int, 1)}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		go func() {
			s.done <- runDemo(ctx, demoOptions{Port: s.port, Program: program, TempRoot: root, Stdout: &s.out, Stderr: &s.errOut,
				Progress: func(ev string) {
					if progress != nil {
						progress(ev, cancel)
					}
				}})
		}()
		select {
		case code := <-s.done:
			cancel()
			if code == 1 && strings.Contains(s.out.String(), "is busy") {
				continue // another process took the port in between
			}
			s.done <- code
			return s
		case <-readyOrBusy(&s.out, &s.errOut):
			return s
		case <-time.After(generous):
			cancel()
			t.Fatalf("the demo did not start.\nout: %s\nerr: %s", s.out.String(), s.errOut.String())
		}
	}
	t.Fatal("no free port")
	return nil
}

// readyOrBusy closes its channel when the demo says it is ready.
func readyOrBusy(out, errOut *syncBuf) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		deadline := time.Now().Add(generous)
		for time.Now().Before(deadline) {
			if strings.Contains(out.String(), "The demo line is ready") {
				return
			}
			time.Sleep(20 * time.Millisecond) // a poll of a buffer; the limit above is the only wall-clock rule
		}
	}()
	return ch
}

func (s *served) stop() int {
	s.t.Helper()
	s.cancel()
	select {
	case c := <-s.done:
		return c
	case <-time.After(generous):
		s.t.Fatal("the demo did not stop")
		return -1
	}
}

func (s *served) floor(key string) (int, string) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/v1/floor", s.port), nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (s *served) linkFile() string {
	s.t.Helper()
	s.errOut.waitFor(s.t, `in "`)
	m := regexp.MustCompile(`in "([^"]+)"`).FindStringSubmatch(s.errOut.String())
	return m[1]
}

func TestRunDemoServesRefusesASecondAndCleansUp(t *testing.T) {
	root := t.TempDir()
	s := startServed(t, root, oxfBinary, nil)
	linkFile := s.linkFile()
	raw, err := os.ReadFile(linkFile)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(raw[strings.Index(string(raw), "#t=")+3:]))
	var code int
	var body string
	for i := 0; i < 600; i++ { // the demo says ready first; the story runs on
		if code, body = s.floor(key); code == 200 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code != 200 || !strings.Contains(body, `"mode":"demo"`) {
		t.Fatalf("floor: %d %s", code, body)
	}
	// A second demo on the same port is refused, and the first keeps serving.
	var out2, err2 syncBuf
	if c := runDemo(context.Background(), demoOptions{Port: s.port, Program: oxfBinary, TempRoot: root, Stdout: &out2, Stderr: &err2}); c != 1 {
		t.Errorf("second demo code %d", c)
	}
	onlyDemoLines(t, "second", out2.String()+err2.String())
	if _, err := os.Stat(linkFile); err != nil {
		t.Errorf("the refused second demo removed the first one's link file: %v", err)
	}
	if code, _ = s.floor(key); code != 200 {
		t.Errorf("the first demo stopped serving: %d", code)
	}
	if c := s.stop(); c != 0 {
		t.Errorf("exit code %d", c)
	}
	all := s.out.String() + s.errOut.String()
	onlyDemoLines(t, "serve", all)
	if strings.Contains(all, key) {
		t.Error("the key was printed")
	}
	rootEmpty(t, "serve", root)
	noStrays(t)
	if c, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", s.port), 500*time.Millisecond); err == nil {
		c.Close()
		t.Error("the port is still open")
	}
}

// F12: a stop at each named event, with the exact exit code and cleanup.
func TestRunDemoStopAtNamedEvents(t *testing.T) {
	for _, at := range []string{"link", "ready", "beat:build_move", "waiting"} {
		t.Run(at, func(t *testing.T) {
			root := t.TempDir()
			s := startServed(t, root, oxfBinary, func(ev string, cancel context.CancelFunc) {
				if ev == at {
					cancel()
				}
			})
			code := <-s.done
			if code != 0 {
				t.Fatalf("stopped at %s: code %d\nout: %s\nerr: %s", at, code, s.out.String(), s.errOut.String())
			}
			onlyDemoLines(t, at, s.out.String()+s.errOut.String())
			if at == "waiting" && !strings.Contains(s.out.String(), "waiting point") {
				t.Errorf("no waiting point line:\n%s", s.out.String())
			}
			rootEmpty(t, at, root)
			noStrays(t)
			if c, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", s.port), 500*time.Millisecond); err == nil {
				c.Close()
				t.Errorf("%s: the port is still open", at)
			}
		})
	}
}

// F5 and F6: the story fails for real, the screen is still served, and the
// stop that follows exits 1.
func TestRunDemoStoryFailureExitsOneAtTheEnd(t *testing.T) {
	failing, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no false program")
	}
	root := t.TempDir()
	s := startServed(t, root, failing, nil)
	s.out.waitFor(t, "did not go as written")
	if strings.Contains(s.out.String(), "waiting point") {
		t.Error("a failed story said it is at its waiting point")
	}
	raw, err := os.ReadFile(s.linkFile())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(raw[strings.Index(string(raw), "#t=")+3:]))
	if code, body := s.floor(key); code != 200 {
		t.Errorf("the honest screen is not served: %d %s", code, body)
	}
	if c := s.stop(); c != 1 {
		t.Errorf("exit code %d, want 1", c)
	}
	onlyDemoLines(t, "failed story", s.out.String()+s.errOut.String())
	rootEmpty(t, "failed story", root)
	noStrays(t)
}

// F9
func TestStaleDirectoriesAreRemovedOnlyWhenTheyAreOursAndLookRight(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, mode os.FileMode) string {
		p := filepath.Join(root, name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "web-link.txt"), []byte("http://x#t=k\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale1 := mk("oxf-demo-111", 0o700)
	stale2 := mk("oxf-demo-222", 0o700)
	own := mk("oxf-demo-333", 0o700)
	wrongMode := mk("oxf-demo-444", 0o755)
	groupMode := mk("oxf-demo-445", 0o770)
	badName := mk("oxf-demo-abc", 0o700)
	onceName := mk("oxf-demo-once-555", 0o700)
	otherName := mk("other-666", 0o700)
	// A read-only sub folder inside a stale directory is removed too.
	sub := filepath.Join(stale2, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	// A regular file with the right name.
	file := filepath.Join(root, "oxf-demo-777")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link with the right name to a directory outside, with a file in it.
	outside := t.TempDir()
	precious := filepath.Join(outside, "precious")
	if err := os.WriteFile(precious, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "oxf-demo-888")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	// A dangling link, and a link to a stale-looking directory.
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "oxf-demo-889")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(own, filepath.Join(root, "oxf-demo-890")); err != nil {
		t.Fatal(err)
	}

	if n := removeStaleDemos(root, own); n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	for _, p := range []string{stale1, stale2} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s is still there", p)
		}
	}
	for name, p := range map[string]string{"own": own, "wrong mode": wrongMode, "group mode": groupMode, "bad name": badName,
		"once name": onceName, "other name": otherName, "file": file, "link": link, "target": precious} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
	if b, err := os.ReadFile(precious); err != nil || string(b) != "keep" {
		t.Errorf("a file outside was touched: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(own, "web-link.txt")); err != nil {
		t.Errorf("the own directory lost its link file: %v", err)
	}
	for _, p := range []string{wrongMode, groupMode} {
		_ = os.Chmod(p, 0o700)
	}
}

// F9: a killed demo's directory is gone at the next start, once the next
// demo has its port. A demo that is refused removes nothing.
func TestNextStartRemovesWhatAKilledDemoLeft(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "oxf-demo-12345")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "web-link.txt"), []byte("http://x#t=oldkey\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := startServed(t, root, oxfBinary, nil)
	s.out.waitFor(t, "Removed 1 data directories")
	if _, err := os.Lstat(stale); err == nil {
		t.Error("the stale directory is still there")
	}
	if strings.Contains(s.out.String()+s.errOut.String(), "oldkey") || strings.Contains(s.out.String(), root) {
		t.Error("the line holds a key or a path")
	}
	linkFile := s.linkFile()
	// A second stale one appears. The second demo is refused for the busy port
	// and must remove nothing, and not the first one's own directory.
	stale2 := filepath.Join(root, "oxf-demo-54321")
	if err := os.Mkdir(stale2, 0o700); err != nil {
		t.Fatal(err)
	}
	var out2, err2 syncBuf
	if c := runDemo(context.Background(), demoOptions{Port: s.port, Program: oxfBinary, TempRoot: root, Stdout: &out2, Stderr: &err2}); c != 1 {
		t.Errorf("second demo code %d", c)
	}
	if _, err := os.Lstat(stale2); err != nil {
		t.Errorf("a refused demo removed a directory: %v", err)
	}
	if _, err := os.Stat(linkFile); err != nil {
		t.Errorf("a refused demo removed the first one's link file: %v", err)
	}
	if c := s.stop(); c != 0 {
		t.Errorf("exit code %d", c)
	}
	_ = os.RemoveAll(stale2)
	rootEmpty(t, "stale", root)
}

// F8
func TestIsTerminal(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if isTerminal(null) {
		t.Error("/dev/null is a terminal")
	}
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if isTerminal(r) || isTerminal(w) {
		t.Error("a pipe is a terminal")
	}
	reg, err := os.CreateTemp(t.TempDir(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	if isTerminal(reg) {
		t.Error("a regular file is a terminal")
	}
	if isTerminal(nil) {
		t.Error("nil is a terminal")
	}
	// The branches, with a stand-in for the attribute call.
	yes := func(uintptr) bool { return true }
	no := func(uintptr) bool { return false }
	if !isTerminalFile(null, yes) {
		t.Error("a character device that answers is not a terminal")
	}
	if isTerminalFile(null, no) {
		t.Error("a character device that does not answer is a terminal")
	}
	if isTerminalFile(reg, yes) || isTerminalFile(r, yes) {
		t.Error("a file that is not a character device is a terminal")
	}
	if isTerminalFile(null, nil) {
		t.Error("no attribute call is a terminal")
	}
	closed, _ := os.Open(os.DevNull)
	closed.Close()
	if isTerminalFile(closed, yes) {
		t.Error("a closed file is a terminal")
	}
}

// With stderr at /dev/null the link goes to a file, never to nowhere.
func TestLinkFileIsMadeWhenStderrIsDevNull(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	root := t.TempDir()
	var out syncBuf
	var sawLink bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for attempt := 0; attempt < 10; attempt++ {
		out = syncBuf{}
		sawLink = false
		code := runDemo(ctx, demoOptions{Port: freePort(t), Program: oxfBinary, TempRoot: root, Terminal: isTerminal(null), Stdout: &out, Stderr: null,
			Progress: func(ev string) {
				if ev == "link" {
					ents, _ := os.ReadDir(root)
					for _, e := range ents {
						if _, err := os.Stat(filepath.Join(root, e.Name(), "web-link.txt")); err == nil {
							sawLink = true
						}
					}
					cancel()
				}
			}})
		if code == 1 && strings.Contains(out.String(), "is busy") {
			continue
		}
		if code != 0 {
			t.Fatalf("code %d\n%s", code, out.String())
		}
		break
	}
	if !sawLink {
		t.Error("no link file was made with stderr at /dev/null")
	}
	rootEmpty(t, "devnull", root)
}

// F2: a closed output pipe is a stop with a cleanup, not a death by signal.
func TestClosedOutputPipeCleansUpAndExitsOne(t *testing.T) {
	failing := 0
	for attempt := 0; attempt < 10; attempt++ {
		root := t.TempDir()
		stderrFile, err := os.Create(filepath.Join(t.TempDir(), "err"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "OXF_TEST_HELPER=1", "OXF_TEST_PORT="+strconv.Itoa(freePort(t)), "OXF_TEST_ROOT="+root, "OXF_TEST_PROG="+oxfBinary)
		cmd.Stderr = stderrFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pgid := cmd.Process.Pid
		t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
		rd := bufio.NewReader(stdout)
		l1, _ := rd.ReadString('\n')
		l2, _ := rd.ReadString('\n')
		if !strings.HasPrefix(l1, "DEMO ") {
			t.Fatalf("first line %q", l1)
		}
		if strings.Contains(l2, "is busy") {
			_ = cmd.Wait()
			failing++
			continue
		}
		stdout.Close() // the reader goes away, as "head -1" does
		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()
		select {
		case err := <-waitCh:
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("the process ended with %v, want exit code 1", err)
			}
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				t.Fatalf("the process was killed by %v", ws.Signal())
			}
			if ee.ExitCode() != 1 {
				t.Fatalf("exit code %d, want 1", ee.ExitCode())
			}
		case <-time.After(generous):
			t.Fatal("the process did not end")
		}
		rootEmpty(t, "closed pipe", root)
		if err := syscall.Kill(-pgid, 0); err != syscall.ESRCH {
			t.Errorf("a process of the demo is left in its group: %v", err)
		}
		return
	}
	t.Fatalf("no free port after %d tries", failing)
}

// noStrays checks that no process that this test process started is left.
// Only descendants of this process count, never other processes of the machine.
func noStrays(t *testing.T) {
	t.Helper()
	ps := exec.Command("ps", "-eo", "pid=,ppid=,command=")
	out, err := ps.Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	children := map[int][]int{}
	cmdOf := map[int]string{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		children[ppid] = append(children[ppid], pid)
		cmdOf[pid] = strings.Join(f[2:], " ")
	}
	var walk func(int)
	walk = func(p int) {
		for _, c := range children[p] {
			if c == ps.Process.Pid {
				continue
			}
			t.Errorf("a process of this test is still running: %d %s", c, cmdOf[c])
			walk(c)
		}
	}
	walk(os.Getpid())
}
