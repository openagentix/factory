package main

import (
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
	"strings"
	"sync"
	"testing"
	"time"
)

var oxfBinary string

func TestMain(m *testing.M) {
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

// syncBuf is a buffer that is safe to use from two goroutines.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

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

func TestRunWithoutDemoIsNotBuilt(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"run"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "not built yet") || strings.Count(strings.TrimSpace(errOut.String()), "\n") != 0 {
		t.Errorf("stderr %q", errOut.String())
	}
}

func TestRunDemoUsage(t *testing.T) {
	for _, args := range [][]string{{"run", "--demo", "--port", "9"}, {"run", "--demo", "--once", "x"}, {"run", "--once"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
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
	if ents, _ := os.ReadDir(root); len(ents) != 0 {
		t.Errorf("the data directory was not removed: %v", ents)
	}
}

func TestRunDemoServesRefusesASecondAndCleansUp(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	var out, errOut syncBuf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- runDemo(ctx, demoOptions{Port: port, Program: oxfBinary, TempRoot: root, Stdout: &out, Stderr: &errOut})
	}()
	re := regexp.MustCompile(`in "([^"]+)"`)
	var linkFile string
	for i := 0; i < 200 && linkFile == ""; i++ {
		if m := re.FindStringSubmatch(errOut.String()); m != nil {
			linkFile = m[1]
		}
		time.Sleep(50 * time.Millisecond)
	}
	if linkFile == "" {
		cancel()
		<-done
		t.Fatalf("no link file path printed.\nout: %s\nerr: %s", out.String(), errOut.String())
	}
	raw, err := os.ReadFile(linkFile)
	if err != nil {
		t.Fatal(err)
	}
	key := string(raw[strings.Index(string(raw), "#t=")+3:])
	key = strings.TrimSpace(key)
	get := func() (int, string) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/v1/floor", port), nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	var code int
	var body string
	for i := 0; i < 100; i++ {
		if code, body = get(); code == 200 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code != 200 || !strings.Contains(body, `"mode":"demo"`) {
		t.Fatalf("floor: %d %s", code, body)
	}
	// A second demo on the same port is refused, and the first keeps serving.
	var out2, err2 syncBuf
	if c := runDemo(context.Background(), demoOptions{Port: port, Program: oxfBinary, TempRoot: root, Stdout: &out2, Stderr: &err2}); c != 1 {
		t.Errorf("second demo code %d", c)
	}
	onlyDemoLines(t, "second", out2.String()+err2.String())
	if code, _ = get(); code != 200 {
		t.Errorf("the first demo stopped serving: %d", code)
	}
	cancel()
	select {
	case c := <-done:
		if c != 0 {
			t.Errorf("exit code %d", c)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the demo did not stop")
	}
	all := out.String() + errOut.String()
	onlyDemoLines(t, "serve", all)
	if strings.Contains(all, key) {
		t.Error("the key was printed")
	}
	if ents, _ := os.ReadDir(root); len(ents) != 0 {
		t.Errorf("the data directory was not removed: %v", ents)
	}
	if c, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond); err == nil {
		c.Close()
		t.Error("the port is still open")
	}
}

func TestRunDemoCancelAtEveryPoint(t *testing.T) {
	for _, after := range []time.Duration{0, 300 * time.Millisecond, 2 * time.Second, 5 * time.Second} {
		root := t.TempDir()
		var out, errOut syncBuf
		ctx, cancel := context.WithTimeout(context.Background(), after)
		code := runDemo(ctx, demoOptions{Port: freePort(t), Program: oxfBinary, TempRoot: root, Stdout: &out, Stderr: &errOut})
		cancel()
		if code != 0 && code != 1 {
			t.Errorf("after %v: code %d\n%s%s", after, code, out.String(), errOut.String())
		}
		onlyDemoLines(t, "cancel", out.String()+errOut.String())
		if ents, _ := os.ReadDir(root); len(ents) != 0 {
			t.Errorf("after %v: the data directory was not removed: %v", after, ents)
		}
	}
}
