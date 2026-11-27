//go:build darwin || linux

package settings

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fixture struct {
	t    *testing.T
	cfg  string // the config dir
	oxf  string // <cfg>/oxf
	data string // the data dir named in the settings
	env  env
}

func settingsFor(data string) string {
	return strings.Replace(goodYAML, "/home/alice/.local/share/oxf", data, 1)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, cfg: filepath.Join(root, "config"), data: filepath.Join(root, "data", "oxf")}
	f.oxf = filepath.Join(f.cfg, "oxf")
	if err := os.MkdirAll(f.oxf, 0o700); err != nil {
		t.Fatal(err)
	}
	f.write(settingsFor(f.data), 0o600)
	f.env = env{configDir: func() (string, error) { return f.cfg, nil }, uid: os.Getuid}
	return f
}

func (f *fixture) write(content string, mode os.FileMode) {
	f.t.Helper()
	p := filepath.Join(f.oxf, "settings.yaml")
	_ = os.Remove(p)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) open() (*Machine, error) { return open(f.env) }

func mustOpen(t *testing.T, f *fixture) *Machine {
	t.Helper()
	m, err := f.open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func TestOpenGood(t *testing.T) {
	f := newFixture(t)
	m := mustOpen(t, f)
	if m.Settings.MachineID != "alice-laptop" || m.Settings.DataDir != f.data || len(m.Settings.Harnesses) != 2 {
		t.Fatalf("settings %+v", m.Settings)
	}
	fi, err := os.Lstat(f.data)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("data dir %v %v", fi, err)
	}
	for _, p := range []string{filepath.Join(f.oxf, "oxf.lock"), filepath.Join(f.data, "oxf.lock")} {
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("lock file %s: %v %v", p, fi, err)
		}
	}
}

func TestSettingsFilePermissions(t *testing.T) {
	for name, mode := range map[string]os.FileMode{"group writable": 0o620, "world writable": 0o602, "all writable": 0o666, "group and world writable": 0o660 | 0o002} {
		f := newFixture(t)
		f.write(settingsFor(f.data), mode)
		if _, err := f.open(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	for name, mode := range map[string]os.FileMode{"0600": 0o600, "0400": 0o400, "0644": 0o644, "0640": 0o640} {
		f := newFixture(t)
		f.write(settingsFor(f.data), mode)
		m, err := f.open()
		if err != nil {
			t.Errorf("%s refused: %v", name, err)
			continue
		}
		m.Close()
	}
}

func TestSettingsFileOwnedByAnotherUser(t *testing.T) {
	f := newFixture(t)
	f.env.uid = func() int { return os.Getuid() + 1 }
	_, err := f.open()
	if err == nil {
		t.Fatal("loaded")
	}
}

func TestSettingsFileMustBeRegular(t *testing.T) {
	t.Run("symlink to a good file", func(t *testing.T) {
		f := newFixture(t)
		real := filepath.Join(filepath.Dir(f.cfg), "elsewhere.yaml")
		if err := os.WriteFile(real, []byte(settingsFor(f.data)), 0o600); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(f.oxf, "settings.yaml")
		os.Remove(p)
		if err := os.Symlink(real, p); err != nil {
			t.Fatal(err)
		}
		if _, err := f.open(); err == nil {
			t.Fatal("loaded through a link")
		}
	})
	t.Run("directory", func(t *testing.T) {
		f := newFixture(t)
		p := filepath.Join(f.oxf, "settings.yaml")
		os.Remove(p)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := f.open(); err == nil {
			t.Fatal("loaded a directory")
		}
	})
	t.Run("fifo does not block", func(t *testing.T) {
		f := newFixture(t)
		p := filepath.Join(f.oxf, "settings.yaml")
		os.Remove(p)
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := f.open(); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("loaded a fifo")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("open blocked on a fifo")
		}
	})
	t.Run("missing", func(t *testing.T) {
		f := newFixture(t)
		os.Remove(filepath.Join(f.oxf, "settings.yaml"))
		if _, err := f.open(); err == nil {
			t.Fatal("started without settings")
		}
	})
	t.Run("too big", func(t *testing.T) {
		f := newFixture(t)
		p := filepath.Join(f.oxf, "settings.yaml")
		if err := os.Truncate(p, 10<<20); err != nil {
			t.Fatal(err)
		}
		if _, err := f.open(); err == nil {
			t.Fatal("loaded 10 MiB")
		}
	})
}

func TestOxfDirectoryMustBeSafe(t *testing.T) {
	for name, mode := range map[string]os.FileMode{"world writable": 0o777, "group writable": 0o770} {
		f := newFixture(t)
		if err := os.Chmod(f.oxf, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := f.open(); err == nil {
			t.Errorf("%s: started", name)
		}
	}
	f := newFixture(t)
	if err := os.RemoveAll(f.oxf); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(); err == nil {
		t.Error("started with no oxf directory")
	}
	f = newFixture(t)
	real := filepath.Join(filepath.Dir(f.cfg), "real-oxf")
	if err := os.Rename(f.oxf, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, f.oxf); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(); err == nil {
		t.Error("started with a linked oxf directory")
	}
	f = newFixture(t)
	f.env.uid = func() int { return os.Getuid() + 1 }
	if _, err := f.open(); err == nil {
		t.Error("started with an oxf directory of another user")
	}
}

func TestConfigDirMustBeAbsolute(t *testing.T) {
	f := newFixture(t)
	for _, dir := range []string{"", "relative/config"} {
		f.env.configDir = func() (string, error) { return dir, nil }
		if _, err := f.open(); err == nil {
			t.Errorf("config dir %q accepted", dir)
		}
	}
}

func TestSecondHolderIsRefusedAtOnce(t *testing.T) {
	f := newFixture(t)
	m := mustOpen(t, f)
	before := snapshot(t, f)
	done := make(chan error, 1)
	go func() {
		m2, err := f.open()
		if err == nil {
			m2.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "another oxf") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the second open blocked")
	}
	if after := snapshot(t, f); after != before {
		t.Fatalf("the refused start changed files:\nbefore %s\nafter  %s", before, after)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m3, err := f.open()
	if err != nil {
		t.Fatalf("not released on close: %v", err)
	}
	m3.Close()
	if err := m3.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func snapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var b strings.Builder
	filepath.Walk(filepath.Dir(f.cfg), func(p string, fi os.FileInfo, err error) error {
		if err == nil {
			b.WriteString(p + " " + fi.Mode().String() + " " + string(rune('0'+fi.Size()%10)) + "\n")
		}
		return nil
	})
	return b.String()
}

func TestHolderThatDiesReleasesTheLocks(t *testing.T) {
	f := newFixture(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHolder$")
	cmd.Env = append(os.Environ(), "OXF_TEST_HOLDER_CONFIG="+f.cfg)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || !strings.Contains(line, "ready") {
		t.Fatalf("helper said %q, %v", line, err)
	}
	if _, err := f.open(); err == nil || !strings.Contains(err.Error(), "another oxf") {
		t.Fatalf("second process was not refused: %v", err)
	}
	stdin.Close()
	cmd.Process.Kill()
	cmd.Wait()
	m, err := f.open()
	if err != nil {
		t.Fatalf("lock not released after the holder died: %v", err)
	}
	m.Close()
}

// TestHelperHolder is a child process of TestHolderThatDiesReleasesTheLocks.
func TestHelperHolder(t *testing.T) {
	cfg := os.Getenv("OXF_TEST_HOLDER_CONFIG")
	if cfg == "" {
		t.Skip("helper process only")
	}
	m, err := open(env{configDir: func() (string, error) { return cfg, nil }, uid: os.Getuid})
	if err != nil {
		os.Stdout.WriteString("error " + err.Error() + "\n")
		os.Exit(3)
	}
	defer m.Close()
	os.Stdout.WriteString("ready\n")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestLockFileThatIsALinkIsNotFollowed(t *testing.T) {
	for _, which := range []string{"config", "data"} {
		f := newFixture(t)
		target := filepath.Join(filepath.Dir(f.cfg), "target.txt")
		if err := os.WriteFile(target, []byte("keep me"), 0o640); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(f.oxf, "oxf.lock")
		if which == "data" {
			if err := os.MkdirAll(f.data, 0o700); err != nil {
				t.Fatal(err)
			}
			link = filepath.Join(f.data, "oxf.lock")
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if m, err := f.open(); err == nil {
			m.Close()
			t.Fatalf("%s: started over a linked lock file", which)
		}
		b, _ := os.ReadFile(target)
		fi, _ := os.Stat(target)
		if string(b) != "keep me" || fi.Mode().Perm() != 0o640 {
			t.Errorf("%s: the target changed: %q %v", which, b, fi.Mode())
		}
	}
	// A dangling link must not make the target.
	f := newFixture(t)
	target := filepath.Join(filepath.Dir(f.cfg), "not-yet")
	if err := os.Symlink(target, filepath.Join(f.oxf, "oxf.lock")); err != nil {
		t.Fatal(err)
	}
	if m, err := f.open(); err == nil {
		m.Close()
		t.Fatal("started")
	}
	if _, err := os.Lstat(target); err == nil {
		t.Fatal("a dangling link was followed and its target made")
	}
}

func TestLockFileOfAnotherUserIsRefused(t *testing.T) {
	f := newFixture(t)
	lock := filepath.Join(f.oxf, "oxf.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.env.uid = func() int { return os.Getuid() + 1 }
	if _, err := lockFile(lock, f.env.uid()); err == nil {
		t.Fatal("locked a file of another user")
	}
}

func TestConfigLockComesBeforeTheSettingsAreRead(t *testing.T) {
	f := newFixture(t)
	f.write("this is not settings: [", 0o600)
	held, err := lockFile(filepath.Join(f.oxf, "oxf.lock"), os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, err = f.open()
	if err == nil || !strings.Contains(err.Error(), "another oxf") {
		t.Fatalf("the settings were read before the lock: %v", err)
	}
}

func TestDataLockComesAfterTheSettingsAndLeavesNoSideEffect(t *testing.T) {
	f := newFixture(t)
	// bad settings: the data directory is never made
	f.write(strings.Replace(settingsFor(f.data), "name: codex", "name: stub", 1), 0o600)
	if _, err := f.open(); err == nil {
		t.Fatal("loaded")
	}
	if _, err := os.Lstat(f.data); err == nil {
		t.Fatal("the data dir was made for bad settings")
	}
	// the data lock is held by someone else
	f.write(settingsFor(f.data), 0o600)
	if err := os.MkdirAll(f.data, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := lockFile(filepath.Join(f.data, "oxf.lock"), os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(); err == nil || !strings.Contains(err.Error(), "another oxf") {
		t.Fatalf("err = %v", err)
	}
	held.Close()
	// the config lock was given back by the failed start
	m, err := f.open()
	if err != nil {
		t.Fatalf("a failed start kept the config lock: %v", err)
	}
	m.Close()
}

func TestDataDirRules(t *testing.T) {
	t.Run("0755 is refused and not changed", func(t *testing.T) {
		f := newFixture(t)
		if err := os.MkdirAll(f.data, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f.data, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := f.open(); err == nil {
			t.Fatal("started")
		}
		if fi, _ := os.Stat(f.data); fi.Mode().Perm() != 0o755 {
			t.Fatalf("the mode was changed to %v", fi.Mode().Perm())
		}
	})
	t.Run("0750 and 0500 are refused", func(t *testing.T) {
		for _, mode := range []os.FileMode{0o750, 0o500, 0o770} {
			f := newFixture(t)
			os.MkdirAll(f.data, 0o700)
			os.Chmod(f.data, mode)
			if _, err := f.open(); err == nil {
				t.Errorf("mode %v accepted", mode)
			}
		}
	})
	t.Run("another user", func(t *testing.T) {
		f := newFixture(t)
		os.MkdirAll(f.data, 0o700)
		if err := checkDataDir(f.data, os.Getuid()+1); err == nil {
			t.Fatal("a directory of another user was accepted")
		}
	})
	t.Run("a symbolic link", func(t *testing.T) {
		f := newFixture(t)
		real := filepath.Join(filepath.Dir(f.cfg), "real-data")
		os.Mkdir(real, 0o700)
		os.MkdirAll(filepath.Dir(f.data), 0o700)
		if err := os.Symlink(real, f.data); err != nil {
			t.Fatal(err)
		}
		if _, err := f.open(); err == nil {
			t.Fatal("started with a linked data dir")
		}
		if _, err := os.Lstat(filepath.Join(real, "oxf.lock")); err == nil {
			t.Fatal("a lock file was made through the link")
		}
	})
	t.Run("a file", func(t *testing.T) {
		f := newFixture(t)
		os.MkdirAll(filepath.Dir(f.data), 0o700)
		os.WriteFile(f.data, []byte("x"), 0o600)
		if _, err := f.open(); err == nil {
			t.Fatal("started")
		}
	})
	t.Run("0700 is fine and is made with parents", func(t *testing.T) {
		f := newFixture(t)
		m := mustOpen(t, f)
		_ = m
		fi, err := os.Stat(filepath.Dir(f.data))
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("parent %v %v", fi, err)
		}
	})
}
