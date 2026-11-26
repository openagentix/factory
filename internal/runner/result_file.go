package runner

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// readResult reads out/result.json of a run directory. The agent controls
// everything under out, so:
//   - out itself must be a real directory, not a link, and the directory
//     opened must be the one that was looked at;
//   - the file is looked at first, then opened without following a link and
//     without blocking on a named pipe, and what was opened must be the same
//     file, a regular file with one hard link;
//   - the size is checked, and the read is cut at the limit anyway.
//
// A swap between the look and the open is narrowed, not ruled out, without an
// operating system sandbox. Reading never leaves the run directory: it uses
// os.Root.
func readResult(runDir string, max int64) ([]byte, error) {
	root, err := os.OpenRoot(runDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	outInfo, err := root.Lstat("out")
	if err != nil {
		return nil, err
	}
	if !outInfo.IsDir() || outInfo.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("out is not a plain directory")
	}
	out, err := root.OpenRoot("out")
	if err != nil {
		return nil, fmt.Errorf("out cannot be opened: %w", err)
	}
	defer out.Close()
	if now, err := out.Stat("."); err != nil || !os.SameFile(outInfo, now) {
		return nil, errors.New("out was replaced")
	}
	before, err := out.Lstat("result.json")
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("result.json is not a regular file")
	}
	f, err := out.OpenFile("result.json", os.O_RDONLY|openNoFollow|openNonBlock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || !os.SameFile(before, st) {
		return nil, errors.New("result.json was replaced")
	}
	if n, ok := linkCount(st); !ok || n != 1 {
		return nil, errors.New("result.json has more than one hard link")
	}
	if st.Size() > max {
		return nil, fmt.Errorf("result.json is %d bytes, over the limit of %d", st.Size(), max)
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("result.json grew over the limit of %d", max)
	}
	return data, nil
}
