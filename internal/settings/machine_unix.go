//go:build darwin || linux

package settings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	if err := checkAncestors(dir, uid); err != nil {
		return nil, err
	}
	if err := checkOxfDir(dir, uid); err != nil {
		return nil, err
	}
	cfgRoot, err := openChecked(dir, uid, oxfDirInfo)
	if err != nil {
		return nil, err
	}
	defer cfgRoot.Close()
	m := &Machine{}
	if m.cfg, err = lockIn(cfgRoot, "oxf.lock", filepath.Join(dir, "oxf.lock"), uid); err != nil {
		return nil, err
	}
	fail := func(err error) (*Machine, error) {
		m.Close()
		return nil, err
	}
	data, err := readSettingsFile(cfgRoot, "settings.yaml", filepath.Join(dir, "settings.yaml"), uid)
	if err != nil {
		return fail(err)
	}
	if m.Settings, err = Parse(data); err != nil {
		return fail(err)
	}
	dataRoot, undo, err := openDataDir(m.Settings.DataDir, uid)
	if err != nil {
		return fail(err)
	}
	defer dataRoot.Close()
	if m.dat, err = lockIn(dataRoot, "oxf.lock", filepath.Join(m.Settings.DataDir, "oxf.lock"), uid); err != nil {
		undo()
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
	return oxfDirInfo(dir, fi, uid)
}

func oxfDirInfo(dir string, fi os.FileInfo, uid int) error {
	if o, ok := owner(fi); !ok || o != uid {
		return fmt.Errorf("settings: %s does not belong to the current user", dir)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("settings: %s must not be writable by group or others", dir)
	}
	return nil
}

const maxLinks = 16

// checkAncestors walks the path from the root and checks every directory above
// the last element, with Lstat, one element at a time.
//
//   - A symbolic link is refused, with one exception: a link that belongs to
//     root. Only root can make or change it, and the system has such links
//     (on macOS /var and /tmp point into /private). The walk then goes on at
//     the target.
//   - A directory must belong to the user or to root.
//   - A directory that group or others can write to is refused, unless it has
//     the sticky bit and belongs to root or to the user (the usual /tmp).
//   - The last element itself must not be a link. Its own owner and mode are
//     checked by the caller, on the open directory.
//   - An element that does not exist ends the walk without an error: the
//     caller makes the rest.
func checkAncestors(p string, uid int) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("settings: %s is not an absolute path", p)
	}
	todo := splitPath(p)
	cur := "/"
	links := 0
	if err := checkParent("/", uid); err != nil {
		return err
	}
	for i := 0; i < len(todo); i++ {
		next := filepath.Join(cur, todo[i])
		last := i == len(todo)-1
		fi, err := os.Lstat(next)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("settings: cannot check %s: %w", next, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if o, ok := owner(fi); last || !ok || o != 0 {
				return fmt.Errorf("settings: %s is a symbolic link; a link is refused anywhere in the path (only links that belong to root are followed)", next)
			}
			if links++; links > maxLinks {
				return fmt.Errorf("settings: too many links in the path %s", p)
			}
			target, err := os.Readlink(next)
			if err != nil {
				return fmt.Errorf("settings: cannot read the link %s: %w", next, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(cur, target)
			}
			todo = append(splitPath(target), todo[i+1:]...)
			cur, i = "/", -1
			continue
		}
		if !fi.IsDir() {
			if last {
				return nil
			}
			return fmt.Errorf("settings: %s is not a directory", next)
		}
		if !last {
			if err := checkParent(next, uid); err != nil {
				return err
			}
		}
		cur = next
	}
	return nil
}

func splitPath(p string) []string {
	var out []string
	for _, e := range strings.Split(filepath.Clean(p), "/") {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// checkParent is the rule for a directory above the one we use.
func checkParent(dir string, uid int) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("settings: cannot check %s: %w", dir, err)
	}
	o, ok := owner(fi)
	if !ok || (o != uid && o != 0) {
		return fmt.Errorf("settings: %s belongs to neither the current user nor root", dir)
	}
	g := -1
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		g = int(st.Gid)
	}
	if !parentAllowed(uid, o, g, os.Getgid(), fi.Mode(), uid == os.Getuid() && ownGroupIsPrivate()) {
		return fmt.Errorf("settings: %s is writable by group or others", dir)
	}
	return nil
}

// parentAllowed says whether a directory above ours may have this mode. The
// owner is the user or root already.
//
//   - Writable by others: only with the sticky bit.
//   - Writable by its group: only with the sticky bit, or when the directory
//     is the user's own, its group is the user's primary group, and that group
//     is private, with the same name as the user. Then umask 002 and a mode
//     of 0775 on ~/.config is normal. A shared primary group (staff on macOS,
//     users on some Linux systems) would trust every local user, so it is not
//     enough. A directory of root keeps the strict rule.
func parentAllowed(me, owner, gid, myGid int, mode os.FileMode, privateGroup bool) bool {
	if mode&os.ModeSticky != 0 {
		return true
	}
	if mode.Perm()&0o002 != 0 {
		return false
	}
	if mode.Perm()&0o020 != 0 {
		return owner == me && me != 0 && gid == myGid && privateGroup
	}
	return true
}

var privateGroupOnce = sync.OnceValue(func() bool {
	u, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return false
	}
	g, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	return err == nil && u.Username != "" && u.Username == g.Name
})

// ownGroupIsPrivate says whether the primary group of this process has the
// name of the user. It is false on any lookup error.
func ownGroupIsPrivate() bool { return privateGroupOnce() }

// openChecked opens a directory that was checked by Lstat, and checks it again
// on the open handle. A link is not followed: the handle must be the very
// directory that Lstat saw. Files are then made and opened relative to the
// returned Root, so a swap of the path afterwards changes nothing.
func openChecked(dir string, uid int, check func(string, os.FileInfo, int) error) (*os.Root, error) {
	lfi, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("settings: cannot check %s: %w", dir, err)
	}
	if !lfi.IsDir() {
		return nil, fmt.Errorf("settings: %s must be a directory and not a link", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("settings: cannot open %s: %w", dir, err)
	}
	h, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("settings: cannot open %s: %w", dir, err)
	}
	hfi, err := h.Stat()
	h.Close()
	if err != nil || !hfi.IsDir() || !os.SameFile(lfi, hfi) {
		root.Close()
		return nil, fmt.Errorf("settings: %s changed while it was opened", dir)
	}
	if err := check(dir, hfi, uid); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

// lockFile opens (and makes, mode 0600) a lock file and takes an exclusive,
// non-blocking lock on it. See lockIn.
func lockFile(path string, uid int) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("settings: cannot open the directory of the lock file %s: %w", path, err)
	}
	defer root.Close()
	return lockIn(root, filepath.Base(path), path, uid)
}

// lockIn does the work of lockFile inside a directory that is already open. A
// link is never followed and the file is never truncated or changed. The lock
// goes when the file is closed or the process dies. A root does follow links
// that stay inside it, so the name is also checked with Lstat before and
// after the open.
func lockIn(root *os.Root, name, shown string, uid int) (*os.File, error) {
	notLink := func() (os.FileInfo, error) {
		fi, err := root.Lstat(name)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("settings: cannot open the lock file %s (a link is refused)", shown)
		}
		return fi, nil
	}
	if _, err := notLink(); err != nil {
		return nil, err
	}
	f, err := root.OpenFile(name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		if fi, lerr := root.Lstat(name); lerr == nil && !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("settings: the lock file %s must be a regular file", shown)
		}
		return nil, fmt.Errorf("settings: cannot open the lock file %s: %w", shown, err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("settings: cannot check the lock file %s: %w", shown, err)
	}
	after, err := notLink()
	if err != nil || after == nil || !os.SameFile(fi, after) {
		f.Close()
		return nil, fmt.Errorf("settings: the lock file %s changed while it was opened", shown)
	}
	if o, ok := owner(fi); !fi.Mode().IsRegular() || !ok || o != uid {
		f.Close()
		return nil, fmt.Errorf("settings: the lock file %s must be a regular file of the current user", shown)
	}
	sc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("settings: cannot lock %s: %w", shown, err)
	}
	var lerr error
	if err := sc.Control(func(fd uintptr) { lerr = syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB) }); err != nil {
		lerr = err
	}
	if lerr != nil {
		f.Close()
		if errors.Is(lerr, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("settings: another oxf process is running (it holds %s)", shown)
		}
		return nil, fmt.Errorf("settings: cannot lock %s: %w", shown, lerr)
	}
	return f, nil
}

// readSettingsFile reads a file that must be a regular file of the user, not
// writable by group or others, and not larger than MaxSize. The file is opened
// inside the checked directory and all checks are on the open file, so they
// cannot be raced. A link is not followed and a FIFO does not block.
func readSettingsFile(root *os.Root, name, path string, uid int) ([]byte, error) {
	if fi, err := root.Lstat(name); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("settings: cannot open %s (it must exist and must not be a link)", path)
	}
	f, err := root.OpenFile(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("settings: cannot open %s (it must exist and must not be a link): %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("settings: cannot check %s: %w", path, err)
	}
	if after, err := root.Lstat(name); err != nil || !os.SameFile(fi, after) {
		return nil, fmt.Errorf("settings: %s changed while it was opened", path)
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

// openDataDir makes the data directory with mode 0700, or checks the one that
// exists, and returns it open. It never changes the mode of a directory that
// exists. Every directory above it must pass checkAncestors, before anything is
// made. When it fails after it made directories, it removes them again (only
// empty ones, deepest first), so a failed start leaves nothing behind. The
// second result removes what this call made, for a later failure.
func openDataDir(dir string, uid int) (*os.Root, func(), error) {
	nothing := func() {}
	if err := checkAncestors(dir, uid); err != nil {
		return nil, nothing, err
	}
	var made []string
	undo := func() {
		if len(made) > 0 {
			_ = os.Remove(filepath.Join(dir, "oxf.lock")) // made by this start, in a directory made by it
		}
		for i := len(made) - 1; i >= 0; i-- {
			_ = os.Remove(made[i])
		}
	}
	// Make the missing directories one at a time, so the ones we made are known.
	var missing []string
	for p := dir; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil {
			break
		}
		missing = append(missing, p)
		if p == filepath.Dir(p) {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil {
			undo()
			return nil, nothing, fmt.Errorf("settings: cannot make the data directory %s: %w", dir, err)
		}
		made = append(made, missing[i])
	}
	if len(made) > 0 {
		// What was just made is ours, but walk again: a link may have been
		// put in the way between the first walk and now.
		if err := checkAncestors(dir, uid); err != nil {
			undo()
			return nil, nothing, err
		}
	}
	root, err := openChecked(dir, uid, dataDirInfo)
	if err != nil {
		undo()
		return nil, nothing, err
	}
	return root, undo, nil
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
	return dataDirInfo(dir, fi, uid)
}

func dataDirInfo(dir string, fi os.FileInfo, uid int) error {
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
