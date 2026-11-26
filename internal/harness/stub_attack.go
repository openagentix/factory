package harness

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openagentix/factory/internal/api"
)

// This file holds the hostile behaviours of the stub. They are test tooling,
// as the rest of the stub: they run only when OXF_ALLOW_STUB=1 is set.

const defaultHugeResult = 500 << 20

// checkAttacks refuses a bad value of an attack field before the stub acts.
func (sc StubScript) checkAttacks() error {
	if sc.FloodStdout < 0 || sc.FloodStderr < 0 || sc.LongLine < 0 || sc.ResultSize < 0 {
		return errors.New("stub script: a size must be zero or more")
	}
	switch sc.ResultAs {
	case "", "dir", "fifo", "huge":
	case "link", "hardlink":
		if !filepath.IsAbs(sc.ResultTarget) {
			return fmt.Errorf("stub script: resultAs %s needs an absolute resultTarget", sc.ResultAs)
		}
	default:
		return fmt.Errorf("stub script: resultAs %q is not known", sc.ResultAs)
	}
	switch sc.ReplaceWorkspace {
	case "", "remove":
	case "link":
		if !filepath.IsAbs(sc.WorkspaceTarget) {
			return errors.New("stub script: replaceWorkspace link needs an absolute workspaceTarget")
		}
	default:
		return fmt.Errorf("stub script: replaceWorkspace %q is not known", sc.ReplaceWorkspace)
	}
	if sc.OutAsLink != "" && !filepath.IsAbs(sc.OutAsLink) {
		return errors.New("stub script: outAsLink must be an absolute path")
	}
	if sc.EventsLink != "" && !filepath.IsAbs(sc.EventsLink) {
		return errors.New("stub script: eventsLink must be an absolute path")
	}
	if sc.Child != nil && sc.Child.SleepSeconds <= 0 {
		return errors.New("stub script: child.sleepSeconds must be more than zero")
	}
	return nil
}

// flood prints junk on standard output and standard error, and one long
// line on standard output.
func (sc StubScript) flood(stdout, stderr io.Writer) error {
	junk := []byte(strings.Repeat("x", 1023) + "\n")
	for left := sc.FloodStdout; left > 0; left -= len(junk) {
		if _, err := stdout.Write(junk); err != nil {
			return err
		}
	}
	if sc.LongLine > 0 {
		chunk := []byte(strings.Repeat("y", 64<<10))
		for left := sc.LongLine; left > 0; {
			n := min(left, len(chunk))
			if _, err := stdout.Write(chunk[:n]); err != nil {
				return err
			}
			left -= n
		}
		if _, err := stdout.Write([]byte("\n")); err != nil {
			return err
		}
	}
	if stderr != nil {
		for left := sc.FloodStderr; left > 0; left -= len(junk) {
			if _, err := stderr.Write(junk); err != nil {
				return err
			}
		}
	}
	return nil
}

// makeResultAs makes result.json something that is not a plain file.
func makeResultAs(outRoot, as, target string, size int64) error {
	p := filepath.Join(outRoot, "result.json")
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	switch as {
	case "link":
		return os.Symlink(target, p)
	case "hardlink":
		return os.Link(target, p)
	case "dir":
		return os.Mkdir(p, 0o755)
	case "fifo":
		return makeFifo(p)
	case "huge":
		if size == 0 {
			size = defaultHugeResult
		}
		f, err := os.Create(p)
		if err != nil {
			return err
		}
		return errors.Join(f.Truncate(size), f.Close())
	}
	return fmt.Errorf("stub script: resultAs %q is not known", as)
}

// tamper acts after the outputs are written, as a worker that attacks the
// runner's view of them does.
func (sc StubScript) tamper(outRoot string, opts StubOptions) error {
	events := filepath.Join(outRoot, "events.jsonl")
	if sc.ForgeEvents != "" {
		if err := os.WriteFile(events, []byte(sc.ForgeEvents), 0o644); err != nil {
			return err
		}
	}
	if sc.EventsLink != "" {
		if err := os.RemoveAll(events); err != nil {
			return err
		}
		if err := os.Symlink(sc.EventsLink, events); err != nil {
			return err
		}
	}
	switch sc.ReplaceWorkspace {
	case "remove":
		if err := os.RemoveAll(opts.Workspace); err != nil {
			return err
		}
	case "link":
		if err := os.Rename(opts.Workspace, opts.Workspace+".moved"); err != nil {
			return err
		}
		if err := os.Symlink(sc.WorkspaceTarget, opts.Workspace); err != nil {
			return err
		}
	}
	if sc.OutAsLink != "" {
		if err := os.Rename(opts.OutDir, opts.OutDir+".moved"); err != nil {
			return err
		}
		if err := os.Symlink(sc.OutAsLink, opts.OutDir); err != nil {
			return err
		}
	}
	return nil
}

// tryWriteIn tries to change the input directory and says what happened.
func tryWriteIn() []StubEvent {
	in := os.Getenv("FACTORY_IN")
	report := func(what string, err error) StubEvent {
		state := "refused"
		if err == nil {
			state = "allowed"
		}
		return StubEvent{Kind: api.EventStatus, Text: "write-in: " + what + ": " + state}
	}
	var evs []StubEvent
	f, err := os.OpenFile(filepath.Join(in, "task.json"), os.O_WRONLY|os.O_TRUNC, 0)
	if err == nil {
		_, err = f.WriteString("changed")
		f.Close()
	}
	evs = append(evs, report("task.json", err))
	g, err := os.OpenFile(filepath.Join(in, "extra.txt"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		g.Close()
	}
	return append(evs, report("new file", err))
}
