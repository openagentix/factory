package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LinkFileName is the name of the link file inside the data directory.
const LinkFileName = "web-link.txt"

// DeliverLink hands the person the link with the key (record 0004).
//
// When w is a terminal, the link is printed once and shown is the link.
// Otherwise nothing with the key is printed. The link is written to a file in
// dataDir, which must have mode 0700 exactly. The file is made with exclusive
// create and mode 0600, after any older file or symbolic link at that name is
// removed. A symbolic link is never written through. Only the file's path is
// printed, and shown is that path.
//
// No error text from this function holds the link.
func DeliverLink(w io.Writer, isTerminal bool, dataDir, link string) (shown string, err error) {
	if link == "" {
		return "", errors.New("server: no link to deliver")
	}
	if isTerminal {
		if _, err := fmt.Fprintln(w, link); err != nil {
			return "", errors.New("server: cannot print the link")
		}
		return link, nil
	}
	if dataDir == "" {
		return "", errors.New("server: no data directory for the link file")
	}
	fi, err := os.Stat(dataDir)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("server: data directory %q is not a usable directory", dataDir)
	}
	if fi.Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0o700 {
		return "", fmt.Errorf("server: data directory %q must have mode 0700", dataDir)
	}
	path := filepath.Join(dataDir, LinkFileName)
	// Remove, never follow: a symbolic link at this name is removed itself.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("server: cannot remove the old link file %q", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("server: cannot create the link file %q", path)
	}
	fail := func() (string, error) {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("server: cannot write the link file %q", path)
	}
	if err := f.Chmod(0o600); err != nil {
		return fail()
	}
	if _, err := io.WriteString(f, link+"\n"); err != nil {
		return fail()
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("server: cannot write the link file %q", path)
	}
	if _, err := fmt.Fprintf(w, "The link to the web app is in %s\n", path); err != nil {
		return "", errors.New("server: cannot print the path of the link file")
	}
	return path, nil
}

// RemoveLinkFile removes the link file, at exit and before the next start. A
// missing file is not an error. A symbolic link at the name is removed, not
// followed.
func RemoveLinkFile(dataDir string) error {
	if dataDir == "" {
		return errors.New("server: no data directory")
	}
	path := filepath.Join(dataDir, LinkFileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("server: cannot remove the link file %q", path)
	}
	return nil
}
