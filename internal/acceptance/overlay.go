package acceptance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/openagentix/factory/internal/repo"
)

// overlayAuthor is written on the overlay commit. It is not a person.
var overlayAuthor = repo.Author{Name: "oxf acceptance", Email: "acceptance@oxf.invalid"}

// buildOverlay makes a commit that holds the files of the judged commit,
// except that everything at the acceptance path comes from the source commit.
// Whatever the judged commit has at that path is removed first, in any
// letter case, so a file with a case variant of a check's name cannot win on
// a case-insensitive file system. The new commit has the judged commit as its
// parent. No ref points to it: the repository's prune removes it later.
func buildOverlay(ctx context.Context, r *repo.Repo, judged, source, acceptancePath string, lim repo.Limits) (string, int, error) {
	tmp, err := os.MkdirTemp("", "oxf-overlay-")
	if err != nil {
		return "", 0, fmt.Errorf("make a temporary directory: %w", err)
	}
	defer removeTree(tmp)
	a, b := filepath.Join(tmp, "judged"), filepath.Join(tmp, "source")
	if _, err := r.Export(ctx, judged, a, lim); err != nil {
		if ctx.Err() == nil && collides(err) {
			return "", 0, &NameCollisionError{Err: err}
		}
		return "", 0, err
	}
	if _, err := r.Export(ctx, source, b, lim); err != nil {
		return "", 0, err
	}
	parts := strings.Split(acceptancePath, "/")
	srcDir := filepath.Join(append([]string{b}, parts...)...)
	if fi, err := lstat(srcDir); err != nil || !fi.IsDir() {
		return "", 0, fmt.Errorf("the acceptance source has no directory at %s", acceptancePath)
	}
	if err := removeFolded(a, parts); err != nil {
		return "", 0, err
	}
	dstDir := filepath.Join(append([]string{a}, parts...)...)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", 0, err
	}
	if err := copyTree(srcDir, dstDir); err != nil {
		return "", 0, err
	}
	left := 1 // the commit
	_ = filepath.WalkDir(a, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && (d.IsDir() || d.Type().IsRegular()) {
			left++ // a tree or a blob: an upper bound, since known objects are shared
		}
		return nil
	})
	commit, _, err := r.Import(ctx, a, judged, "acceptance checks laid over the change under test", overlayAuthor, repo.ImportOptions{Limits: lim})
	return commit, left, err
}

// NameCollisionError says that the change under test holds names that collide
// on this file system, for example two names that differ only by letter case.
// It is a fault of the change, not of the factory, and the reason can be sent
// back to whoever made the change. The run is refused: nothing is shown as
// working.
type NameCollisionError struct{ Err error }

func (e *NameCollisionError) Error() string {
	return "the change contains names that collide on this file system, for example two names that differ only by letter case, so its files cannot be laid out to run the checks: " + e.Err.Error()
}

func (e *NameCollisionError) Unwrap() error { return e.Err }

func collides(err error) bool {
	if errors.Is(err, fs.ErrExist) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "file exists")
}

func lstat(p string) (fs.FileInfo, error) { return os.Lstat(p) }

// removeTree removes a directory tree, also when a mode got in the way.
func removeTree(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

func fold(s string) string { return strings.ToLower(strings.ToUpper(s)) }

// removeFolded removes the entry at parts below root, and any entry that
// differs from it only by letter case, at each level. Links are removed, never
// followed.
func removeFolded(root string, parts []string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if fold(e.Name()) != fold(parts[0]) {
			continue
		}
		p := filepath.Join(root, e.Name())
		if len(parts) == 1 || !e.Type().IsDir() {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			continue
		}
		if err := removeFolded(p, parts[1:]); err != nil {
			return err
		}
	}
	return nil
}

// copyTree copies plain files and directories. A link or any other kind of
// entry is left out.
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !plainName(e.Name()) {
			return fmt.Errorf("the acceptance source holds the name %q, which is not plain ASCII (letters, digits, - _ .)", repo.Visible(e.Name(), 100))
		}
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		fi, err := os.Lstat(s)
		if err != nil {
			return err
		}
		switch {
		case fi.IsDir():
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
			if err := copyTree(s, d); err != nil {
				return err
			}
		case fi.Mode().IsRegular():
			if err := copyFile(s, d, fi.Mode().Perm()&0o100 != 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string, exec bool) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := fs.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
