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
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
//
// Command adds only the variables of its own to Command.Env (the FACTORY_*
// variables). It does not copy, filter or remove anything in the inherited
// environment. Removing git host tokens, the credential helper and the agent
// socket from the environment is the runner's job, and the runner must do it
// before it starts the program.
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

var (
	modelPattern  = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]][A-Za-z0-9._:/\[\]-]*$`)
	pluginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// refusedPrograms are shells and wrappers. A policy command that starts
// with one of them could run anything, so it is never an allowlist entry.
// git is refused too: the worker does not use git, and the harness allows
// its own read-only commands by itself.
var refusedPrograms = map[string]bool{
	"git": true, "sh": true, "bash": true, "zsh": true, "dash": true, "fish": true,
	"ksh": true, "csh": true, "tcsh": true, "env": true, "sudo": true, "doas": true,
	"xargs": true, "eval": true, "exec": true, "nohup": true, "time": true,
	"command": true, "builtin": true, "nice": true, "timeout": true, "setsid": true,
}

// shellMeta are characters that mean something to a shell. A policy command
// is a list of plain words, so none may appear.
const shellMeta = ";|&$<>(){}`'\"\\*?!#~"

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// checkCommand validates one policy command as a list of plain words.
//
// This is not a sandbox. A permitted test command still runs the
// repository's own code, which can do what the user can do. The check only
// keeps the allowlist entry honest: one program with its arguments, with no
// shell syntax and no wrapper that would run something else.
func checkCommand(c string) error {
	if strings.TrimSpace(c) == "" {
		return errors.New("is empty")
	}
	if hasControl(c) {
		return errors.New("holds a control character")
	}
	if i := strings.IndexAny(c, shellMeta); i >= 0 {
		return fmt.Errorf("holds the shell character %q", c[i])
	}
	first := strings.Fields(c)[0]
	if refusedPrograms[strings.ToLower(filepath.Base(first))] {
		return fmt.Errorf("starts with %q, which is a shell, a wrapper or git", first)
	}
	if strings.HasPrefix(first, "-") {
		return errors.New("must start with a program")
	}
	return nil
}

// checkDir validates a directory or file path that enters a command: an
// absolute, clean path with no component that starts with a dash.
func checkDir(name, p string) error {
	switch {
	case hasControl(p):
		return fmt.Errorf("harness: %s holds a control character", name)
	case !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/":
		return fmt.Errorf("harness: %s must be an absolute, clean path: %q", name, p)
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, "-") {
			return fmt.Errorf("harness: %s has a component that starts with a dash: %q", name, p)
		}
	}
	return nil
}

func (s Spec) validate() error {
	if s.Workspace == "" || s.OutDir == "" {
		return errors.New("harness: spec needs Workspace and OutDir")
	}
	for name, p := range map[string]string{"Workspace": s.Workspace, "OutDir": s.OutDir} {
		if err := checkDir(name, p); err != nil {
			return err
		}
	}
	for name, p := range map[string]string{"InDir": s.InDir, "PromptFile": s.PromptFile, "StubScript": s.StubScript} {
		if p == "" {
			continue
		}
		if err := checkDir(name, p); err != nil {
			return err
		}
	}
	for i, w := range s.Writable {
		if err := checkDir(fmt.Sprintf("Writable[%d]", i), w); err != nil {
			return err
		}
	}
	if s.Model != "" && !modelPattern.MatchString(s.Model) {
		return fmt.Errorf("harness: model %q is not a plain model name", s.Model)
	}
	if hasControl(s.Method) {
		return errors.New("harness: method holds a control character")
	}
	if name, ok := strings.CutPrefix(s.Method, api.MethodPluginPrefix); ok && !pluginPattern.MatchString(name) {
		return fmt.Errorf("harness: plugin name %q is not a plain name", name)
	}
	for name, p := range s.PluginDirs {
		if !pluginPattern.MatchString(name) {
			return fmt.Errorf("harness: plugin name %q is not a plain name", name)
		}
		if err := checkDir("PluginDirs["+name+"]", p); err != nil {
			return err
		}
	}
	switch s.Billing {
	case "", api.BillingMetered, api.BillingPlan, api.BillingUnknown:
	default:
		return fmt.Errorf("harness: billing %q is not known", s.Billing)
	}
	if math.IsNaN(s.MaxCostUSD) || math.IsInf(s.MaxCostUSD, 0) || s.MaxCostUSD < 0 {
		return errors.New("harness: MaxCostUSD must be a finite number, zero or more")
	}
	for i, c := range s.Commands {
		for _, f := range forbidden {
			if strings.Contains(c, f) {
				return fmt.Errorf("%w: policy command %q", ErrForbidden, c)
			}
		}
		if err := checkCommand(c); err != nil {
			return fmt.Errorf("harness: Commands[%d] %q %w", i, c, err)
		}
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
