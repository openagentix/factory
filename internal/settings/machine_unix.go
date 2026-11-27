//go:build darwin || linux

package settings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// env is what Open needs from the system. Code sets it; no flag, variable or
// file can reach it.
type env struct {
	configDir func() (string, error)
	uid       func() int
}

// Open takes the locks and loads the settings, in the order of record 0003:
// the config lock, then the settings, then the data lock. It uses the user's
// config dir, as os.UserConfigDir gives it.
func Open() (*Machine, error) {
	return open(env{configDir: os.UserConfigDir, uid: os.Getuid})
}

func open(e env) (*Machine, error) {
	uid := e.uid()
	base, err := e.configDir()
	if err != nil || base == "" || !filepath.IsAbs(base) {
		return nil, errors.New("settings: cannot find the config directory")
	}
	dir := filepath.Join(base, "oxf")
	if err := checkOxfDir(dir, uid); err != nil {
		return nil, err
	}
	m := &Machine{}
	if m.cfg, err = lockFile(filepath.Join(dir, "oxf.lock"), uid); err != nil {
		return nil, err
	}
	fail := func(err error) (*Machine, error) {
		m.Close()
		return nil, err
	}
	data, err := readSettingsFile(filepath.Join(dir, "settings.yaml"), uid)
	if err != nil {
		return fail(err)
	}
	if m.Settings, err = Parse(data); err != nil {
		return fail(err)
	}
	if err := ensureDataDir(m.Settings.DataDir, uid); err != nil {
		return fail(err)
	}
	if m.dat, err = lockFile(filepath.Join(m.Settings.DataDir, "oxf.lock"), uid); err != nil {
		return fail(err)
	}
	return m, nil
}

func owner(fi os.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// checkOxfDir wants a real directory of the user that nobody else can write to.
func checkOxfDir(dir string, uid int) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("settings: the directory %s is missing or cannot be read: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("settings: %s must be a directory and not a link", dir)
	}
	if o, ok := owner(fi); !ok || o != uid {
		return fmt.Errorf("settings: %s does not belong to the current user", dir)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("settings: %s must not be writable by group or others", dir)
	}
	return nil
}

// lockFile opens (and makes, mode 0600) a lock file and takes an exclusive,
// non-blocking lock on it. A link is never followed and the file is never
// truncated or changed. The lock goes when the file is closed or the process
// dies.
func lockFile(path string, uid int) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("settings: cannot open the lock file %s (a link is refused): %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("settings: cannot check the lock file %s: %w", path, err)
	}
	if o, ok := owner(fi); !fi.Mode().IsRegular() || !ok || o != uid {
		f.Close()
		return nil, fmt.Errorf("settings: the lock file %s must be a regular file of the current user", path)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("settings: another oxf process is running (it holds %s)", path)
		}
		return nil, fmt.Errorf("settings: cannot lock %s: %w", path, err)
	}
	return f, nil
}

// readSettingsFile reads a file that must be a regular file of the user, not
// writable by group or others, and not larger than MaxSize. All checks are on
// the open file, so they cannot be raced. A link is not followed and a FIFO
// does not block.
func readSettingsFile(path string, uid int) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("settings: cannot open %s (it must exist and must not be a link): %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("settings: cannot check %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("settings: %s must be a regular file", path)
	}
	if o, ok := owner(fi); !ok || o != uid {
		return nil, fmt.Errorf("settings: %s does not belong to the current user", path)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("settings: %s must not be writable by group or others", path)
	}
	if fi.Size() > MaxSize {
		return nil, fmt.Errorf("settings: %s is larger than %d bytes", path, MaxSize)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("settings: cannot read %s: %w", path, err)
	}
	return b, nil
}

// ensureDataDir makes the data directory with mode 0700, or checks the one
// that exists. It never changes the mode of a directory that exists.
func ensureDataDir(dir string, uid int) error {
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("settings: cannot make the data directory %s: %w", dir, err)
		}
	}
	return checkDataDir(dir, uid)
}

// checkDataDir wants a real directory of the user with mode exactly 0700.
func checkDataDir(dir string, uid int) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("settings: cannot check the data directory %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("settings: the data directory %s must be a directory and not a link", dir)
	}
	if o, ok := owner(fi); !ok || o != uid {
		return fmt.Errorf("settings: the data directory %s does not belong to the current user", dir)
	}
	if fi.Mode().Perm() != 0o700 {
		return fmt.Errorf("settings: the data directory %s must have mode 0700, not %04o", dir, fi.Mode().Perm())
	}
	return nil
}
