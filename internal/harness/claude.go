package harness

import (
	"strconv"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Claude is the adapter for Claude Code. The flags are the ones used in the
// trial of 2026-11-26 with Claude Code 2.1.326.
type Claude struct {
	// Program is the program to start. Empty means "claude".
	Program string
	// Now gives the time for events that carry none. Empty means time.Now.
	Now func() time.Time
}

// NewClaude returns the Claude Code adapter.
func NewClaude() *Claude { return &Claude{} }

// Name implements Adapter.
func (c *Claude) Name() string { return "claude-code" }

// Command implements Adapter. It always uses print mode and a permission
// mode that denies without asking, and an allowlist built from the policy.
func (c *Claude) Command(s Spec) (Command, error) {
	if err := s.validate(); err != nil {
		return Command{}, err
	}
	prog := c.Program
	if prog == "" {
		prog = "claude"
	}
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "dontAsk"}
	tools := []string{"Read", "Glob", "Grep", "Edit", "Write"}
	plugin := s.pluginName()
	var pluginPath string
	if plugin != "" {
		pluginPath = s.PluginDirs[plugin]
	}
	if pluginPath != "" {
		tools = append(tools, "Skill")
	}
	for _, cmd := range s.Commands {
		tools = append(tools, bashRule(cmd))
	}
	args = append(args, "--allowedTools")
	args = append(args, tools...)
	args = append(args, "--add-dir", s.OutDir)
	for _, w := range s.Writable {
		args = append(args, "--add-dir", w)
	}
	args = append(args, "--no-session-persistence")
	if s.Billing == api.BillingMetered && s.MaxCostUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(s.MaxCostUSD, 'f', -1, 64))
	}
	if pluginPath != "" {
		args = append(args, "--plugin-dir", pluginPath)
	}
	if s.Model != "" {
		args = append(args, "--model", s.Model)
	}
	cmd := Command{Program: prog, Args: args, Env: s.env(), Dir: s.Workspace, StdinFile: s.PromptFile}
	if err := checkForbidden(cmd); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

// bashRule turns a policy command into a Claude Code rule. A command that
// already ends in a wildcard is kept as it is.
func bashRule(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasSuffix(cmd, ":*") {
		return "Bash(" + cmd + ")"
	}
	return "Bash(" + cmd + ":*)"
}
