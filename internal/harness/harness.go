// Package harness holds thin adapters for the coding agent programs a worker
// starts: Claude Code, Codex CLI and a stub for tests and demo mode.
//
// An adapter does two things. It builds the command to start from a run
// specification, and it reads the program's event stream and turns it into
// events and a final summary. An adapter never starts a process, never reads
// a file and never touches a login. The runner does the starting.
package harness

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Spec says what one run needs. The orchestrator fills it in.
type Spec struct {
	// Workspace is the checkout the worker edits. It is the working directory.
	Workspace string
	// InDir holds task.json, prompt.md and trigger.json. OutDir is empty and
	// writable. They are named in FACTORY_IN and FACTORY_OUT.
	InDir  string
	OutDir string
	// PromptFile is the rendered prompt.md. It goes to the program on
	// standard input, not on the command line, so ticket text never shows up
	// in a process list.
	PromptFile string
	// Commands are the commands the run may use. Anything else is denied.
	Commands []string
	// Writable lists extra paths the run may write, besides Workspace and
	// OutDir.
	Writable []string
	// Method is the method that will run: builtin, plugin:<name> or a file
	// path. The orchestrator has already chosen between asked and used.
	Method string
	// PluginDirs maps a plugin name to the path of its installed copy.
	PluginDirs map[string]string
	Model      string
	Deadline   time.Duration
	// Billing is how the run will be paid for, when the orchestrator knows
	// before the start. Empty means not known. A cost budget is passed to
	// the program only for metered billing.
	Billing    string
	MaxCostUSD float64
	// StubScript is the script file of the stub harness.
	StubScript string
}

// Command is how to start a program. The runner starts it.
type Command struct {
	Program string
	Args    []string
	// Env is extra environment, as KEY=value, added to the runner's own.
	Env []string
	// Dir is the working directory.
	Dir string
	// StdinFile is a file whose content goes on standard input. Empty means
	// standard input is closed.
	StdinFile string
}

// Summary is what a stream said at the end.
type Summary struct {
	Harness api.HarnessInfo
	Usage   api.Usage
	Denials []api.Denial
	Turns   int
	// Duration is how long the harness says the run took.
	Duration time.Duration
	// Complete is false when the stream ended without a final event.
	Complete bool
	// Failed is true when the final event reports an error.
	Failed bool
	// Skipped counts lines that were not valid JSON or of a kind the adapter
	// does not know. They are ignored, never fatal.
	Skipped int
}

// Adapter is the one interface the three harnesses implement.
type Adapter interface {
	// Name is the harness name used in result.json.
	Name() string
	// Command builds the command to start for a run.
	Command(Spec) (Command, error)
	// Read consumes the program's standard output until it ends. It calls
	// emit for each event, in order. It returns a summary even when the
	// stream is cut short. The error is only for a failing reader.
	Read(r io.Reader, emit func(api.Event)) (Summary, error)
}

// forbidden lists text that must never appear in a command. Each one skips
// permission checks or the sandbox.
var forbidden = []string{
	"--dangerously-skip-permissions",
	"--dangerously-bypass-approvals-and-sandbox",
	"bypassPermissions",
	"danger-full-access",
	"--yolo",
}

// ErrForbidden is returned when a command would hold a flag that skips
// permission checks or the sandbox.
var ErrForbidden = errors.New("harness: command holds a flag that skips permission checks")

// gitReadOnly are the git subcommands a policy may allow. The worker never
// commits, so no other git command is allowed.
var gitReadOnly = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true,
	"rev-parse": true, "ls-files": true, "blame": true,
}

func (s Spec) validate() error {
	var missing []string
	if s.Workspace == "" {
		missing = append(missing, "Workspace")
	}
	if s.OutDir == "" {
		missing = append(missing, "OutDir")
	}
	if len(missing) > 0 {
		return fmt.Errorf("harness: spec needs %s", strings.Join(missing, " and "))
	}
	for i, c := range s.Commands {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("harness: spec.Commands[%d] is empty", i)
		}
		if err := checkGit(c); err != nil {
			return err
		}
	}
	return nil
}

// checkGit refuses a policy command that would let the worker change git
// state. The worker does not commit. The orchestrator does.
func checkGit(c string) error {
	f := strings.Fields(c)
	if len(f) == 0 || f[0] != "git" {
		return nil
	}
	if len(f) < 2 || !gitReadOnly[f[1]] {
		return fmt.Errorf("harness: command %q is not allowed: the worker does not use git to change state", c)
	}
	return nil
}

// env builds the extra environment shared by every harness.
func (s Spec) env() []string {
	var e []string
	if s.InDir != "" {
		e = append(e, "FACTORY_IN="+s.InDir)
	}
	e = append(e, "FACTORY_OUT="+s.OutDir)
	if s.Deadline > 0 {
		e = append(e, "FACTORY_DEADLINE_SECONDS="+strconv.Itoa(int(s.Deadline.Seconds())))
	}
	if s.Billing == api.BillingMetered && s.MaxCostUSD > 0 {
		e = append(e, "FACTORY_MAX_COST_USD="+strconv.FormatFloat(s.MaxCostUSD, 'f', -1, 64))
	}
	sort.Strings(e)
	return e
}

// pluginName returns the plugin named by the method, or "".
func (s Spec) pluginName() string {
	if n, ok := strings.CutPrefix(s.Method, api.MethodPluginPrefix); ok {
		return n
	}
	return ""
}

// checkForbidden refuses a finished command that holds a forbidden string.
func checkForbidden(c Command) error {
	all := append([]string{c.Program, c.Dir, c.StdinFile}, c.Args...)
	all = append(all, c.Env...)
	for _, a := range all {
		for _, f := range forbidden {
			if strings.Contains(a, f) {
				return fmt.Errorf("%w: %q", ErrForbidden, f)
			}
		}
	}
	return nil
}
