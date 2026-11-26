package repo

// Why Import writes loose objects itself, in Go, and does not start a git
// process for each file or drive git fast-import:
//
//   - The number of files is chosen by the agent. A process for each file
//     took 56 seconds for 5,000 files. Writing objects here costs no process.
//   - A loose object is only a header, the content, zlib, and a name that is
//     the SHA-1 of the header and content. That is small enough to review.
//     fast-import would need a ref to write, a pack, and a second parser for
//     paths, and its failure modes are harder to bound.
//   - It relies on the object format being sha1. Init makes the repository
//     with --object-format=sha1, and the guard refuses a repository that has
//     a repository format version other than 0, which is the only way to name
//     another object format.
//
// Objects are written through an os.Root on the repository directory, so a
// link planted in it cannot send a write elsewhere. The content goes to a
// temporary file in objects/info (the guard allows files there), with a
// random name made with O_EXCL, so two imports never share one. The final name
// objects/xx/yyyy is made with a hard link from the temporary file, which
// fails if the name exists: an object file, once present, is never replaced
// or written again, and "already exists" counts as success. The temporary
// file is removed on every path.
//
// Nothing is deleted after a failed import. An object is named by its
// content, so another import may be using the very same file, and an
// unreferenced object does no harm. Unreferenced objects are left for Prune,
// which the orchestrator calls on its own schedule.

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"time"
)

type objWriter struct {
	root *os.Root
}

func openObjWriter(repoDir string) (*objWriter, error) {
	root, err := os.OpenRoot(repoDir)
	if err != nil {
		return nil, err
	}
	return &objWriter{root: root}, nil
}

func (w *objWriter) close() { _ = w.root.Close() }

// ctxReader stops reading when the context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// write stores an object of exactly size bytes read from content and returns
// its id. If content has fewer or more bytes than size, nothing is stored.
func (w *objWriter) write(ctx context.Context, typ string, size int64, content io.Reader) (string, error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	if err := w.root.Mkdir("objects/info", 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	tmp := "objects/info/oxf-tmp-" + hex.EncodeToString(rnd[:])
	f, err := w.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
		_ = w.root.Remove(tmp)
	}()
	h := sha1.New()
	zw := zlib.NewWriter(f)
	header := typ + " " + strconv.FormatInt(size, 10) + "\x00"
	out := io.MultiWriter(h, zw)
	if _, err := io.WriteString(out, header); err != nil {
		return "", err
	}
	n, err := io.Copy(out, ctxReader{ctx, io.LimitReader(content, size)})
	if err != nil {
		return "", err
	}
	if n != size {
		return "", fmt.Errorf("the content has %d bytes, not the %d that were expected", n, size)
	}
	var extra [1]byte
	if m, _ := content.Read(extra[:]); m > 0 {
		return "", errors.New("the content grew while it was read")
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	oid := hex.EncodeToString(h.Sum(nil))
	dir := "objects/" + oid[:2]
	if err := w.root.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	final := dir + "/" + oid[2:]
	err = w.root.Link(tmp, final)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrExist):
		// The object is there already. Make it look new, as git does, so a
		// prune that runs before the commit exists does not take it. Setting
		// the time works on a read-only file, because it is ours.
		now := time.Now()
		if err := w.root.Chtimes(final, now, now); err != nil {
			return "", err
		}
	default:
		return "", err
	}
	return oid, nil
}

// treeItem is one line of a tree object.
type treeItem struct {
	mode string // "100644", "100755", "120000" or "40000"
	name string
	oid  string // hex
}

// writeTree stores a tree. Git sorts names as bytes, and compares a
// directory as if its name ended in a slash.
func (w *objWriter) writeTree(ctx context.Context, items []treeItem) (string, error) {
	key := func(it treeItem) string {
		if it.mode == "40000" {
			return it.name + "/"
		}
		return it.name
	}
	sort.Slice(items, func(i, j int) bool { return key(items[i]) < key(items[j]) })
	var body []byte
	for _, it := range items {
		raw, err := hex.DecodeString(it.oid)
		if err != nil || len(raw) != sha1.Size {
			return "", fmt.Errorf("bad object id %q", it.oid)
		}
		body = append(body, it.mode...)
		body = append(body, ' ')
		body = append(body, it.name...)
		body = append(body, 0)
		body = append(body, raw...)
	}
	return w.write(ctx, "tree", int64(len(body)), bytes.NewReader(body))
}
