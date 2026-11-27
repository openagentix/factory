package harness

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if w := sc.SwapRunDir; w != nil {
		switch w.Mode {
		case "link", "loop":
		case "away":
			if !filepath.IsAbs(w.MoveTo) {
				return errors.New("stub script: swapRunDir away needs an absolute moveTo")
			}
		default:
			return fmt.Errorf("stub script: swapRunDir mode %q is not known", w.Mode)
		}
		if !filepath.IsAbs(w.Target) {
			return errors.New("stub script: swapRunDir needs an absolute target")
		}
		if w.Mode == "loop" && w.LoopMS <= 0 {
			return errors.New("stub script: swapRunDir loop needs loopMs")
		}
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

// checkAttackRoot refuses a behaviour that acts outside the stub's own
// directories unless every path it touches lies under the directory named in
// OXF_STUB_ATTACK_ROOT. With the variable unset they are all refused. The
// runner tests set it. Nothing is changed before this check.
func (sc StubScript) checkAttackRoot(opts StubOptions) error {
	var paths []string
	if sc.WriteIn {
		paths = append(paths, os.Getenv("FACTORY_IN"))
	}
	if sc.OutAsLink != "" {
		paths = append(paths, opts.OutDir, opts.OutDir+".moved", sc.OutAsLink)
	}
	switch sc.ReplaceWorkspace {
	case "remove":
		paths = append(paths, opts.Workspace)
	case "link":
		paths = append(paths, opts.Workspace, opts.Workspace+".moved", sc.WorkspaceTarget)
	}
	if w := sc.SwapRunDir; w != nil {
		run := filepath.Dir(opts.Workspace)
		paths = append(paths, run, run+".moved", w.Target)
		if w.MoveTo != "" {
			paths = append(paths, w.MoveTo)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	root := os.Getenv("OXF_STUB_ATTACK_ROOT")
	if root == "" {
		return errors.New("stub script: a hostile behaviour needs OXF_STUB_ATTACK_ROOT, which is not set")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(realRoot) {
		return errors.New("stub script: OXF_STUB_ATTACK_ROOT is not a usable directory")
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fmt.Errorf("stub script: %q is not an absolute, clean path", p)
		}
		// Resolve the deepest part that exists, then add the rest.
		existing, rest := p, ""
		for {
			if _, err := os.Lstat(existing); err == nil {
				break
			}
			next := filepath.Dir(existing)
			if next == existing {
				break
			}
			rest = filepath.Join(filepath.Base(existing), rest)
			existing = next
		}
		// A link at the path itself is judged by where its parent is.
		if fi, err := os.Lstat(existing); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			existing, rest = filepath.Dir(existing), filepath.Join(filepath.Base(existing), rest)
		}
		real, err := filepath.EvalSymlinks(existing)
		if err != nil {
			return err
		}
		if !inside(realRoot, filepath.Join(real, rest)) {
			return fmt.Errorf("stub script: %q is outside OXF_STUB_ATTACK_ROOT", p)
		}
	}
	return nil
}

// swapRunDir moves the run directory away and puts a link in its place.
func swapRunDir(w StubSwap, workspace string) error {
	run := filepath.Dir(workspace)
	swap := func(moved string) error {
		if err := os.Rename(run, moved); err != nil {
			return err
		}
		return os.Symlink(w.Target, run)
	}
	switch w.Mode {
	case "link":
		return swap(run + ".moved")
	case "away":
		return swap(w.MoveTo)
	}
	// loop: swap and restore until the time is up, and end restored.
	end := time.Now().Add(time.Duration(w.LoopMS) * time.Millisecond)
	for time.Now().Before(end) {
		if err := swap(run + ".moved"); err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
		if err := os.Remove(run); err != nil {
			return err
		}
		if err := os.Rename(run+".moved", run); err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	return nil
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
	if sc.SwapRunDir != nil {
		if err := swapRunDir(*sc.SwapRunDir, opts.Workspace); err != nil {
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
