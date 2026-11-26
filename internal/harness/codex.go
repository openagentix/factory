package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Codex is the adapter for Codex CLI. The flags are the ones used in the
// trial of 2026-11-26 with Codex CLI 0.170.0.
type Codex struct {
	// Program is the program to start. Empty means "codex".
	Program string
	// Billing is how the run is paid for, when the orchestrator knows. The
	// event stream does not say, so the default is unknown.
	Billing string
	// Now gives the time for events. Empty means time.Now.
	Now func() time.Time
}

// NewCodex returns the Codex CLI adapter.
func NewCodex() *Codex { return &Codex{} }

// Name implements Adapter.
func (c *Codex) Name() string { return "codex" }

// Command implements Adapter. The sandbox is always workspace-write. The
// prompt comes on standard input, named by the final "-".
func (c *Codex) Command(s Spec) (Command, error) {
	if err := s.validate(); err != nil {
		return Command{}, err
	}
	prog := c.Program
	if prog == "" {
		prog = "codex"
	}
	args := []string{"exec", "--json", "--sandbox", "workspace-write", "--add-dir", s.OutDir}
	for _, w := range s.Writable {
		args = append(args, "--add-dir", w)
	}
	args = append(args, "--ephemeral", "-o", filepath.Join(s.OutDir, "last-message.txt"))
	if s.Model != "" {
		args = append(args, "-m", s.Model)
	}
	args = append(args, "-")
	cmd := Command{Program: prog, Args: args, Env: s.env(), Dir: s.Workspace, StdinFile: s.PromptFile}
	if err := checkForbidden(cmd); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

type codexLine struct {
	Type  string `json:"type"`
	Usage *struct {
		Input  int64 `json:"input_tokens"`
		Output int64 `json:"output_tokens"`
	} `json:"usage"`
	Item    *codexItem `json:"item"`
	Message string     `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type codexItem struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Command  string `json:"command"`
	ExitCode *int   `json:"exit_code"`
	Message  string `json:"message"`
	Changes  []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
}

// Read implements Adapter. Codex prints no cost, so none is kept, and it
// prints no denial list, so Denials stays empty.
func (c *Codex) Read(r io.Reader, emit func(api.Event)) (Summary, error) {
	now := clock(c.Now)
	started := now()
	var sum Summary
	sum.Harness = api.HarnessInfo{Name: c.Name()}
	sum.Usage.Billing = c.Billing
	if sum.Usage.Billing == "" {
		sum.Usage.Billing = api.BillingUnknown
	}
	send := func(kind, text string) { emit(api.Event{Time: now(), Kind: kind, Text: clip(text)}) }

	err := eachLine(r, func(line []byte) {
		var l codexLine
		if json.Unmarshal(line, &l) != nil || l.Type == "" {
			sum.Skipped++
			return
		}
		switch l.Type {
		case "thread.started":
			send(api.EventStatus, "started "+c.Name())
		case "turn.started":
		case "turn.completed":
			sum.Complete = true
			sum.Turns++
			if l.Usage != nil {
				sum.Usage.InputTokens += l.Usage.Input
				sum.Usage.OutputTokens += l.Usage.Output
			}
			send(api.EventStatus, "finished")
		case "turn.failed":
			sum.Complete, sum.Failed = true, true
			msg := l.Message
			if l.Error != nil {
				msg = l.Error.Message
			}
			send(api.EventStatus, "failed: "+msg)
		case "error":
			send(api.EventStatus, "error: "+l.Message)
		case "item.started", "item.updated":
			// The completed item says everything the live view needs.
		case "item.completed":
			if l.Item == nil {
				sum.Skipped++
				return
			}
			switch it := l.Item; it.Type {
			case "agent_message":
				if it.Text != "" {
					send(api.EventMessage, it.Text)
				}
			case "command_execution":
				txt := "command: " + it.Command
				if it.ExitCode != nil {
					txt = fmt.Sprintf("%s (exit %d)", txt, *it.ExitCode)
				}
				send(api.EventTool, txt)
			case "file_change":
				var paths []string
				for _, ch := range it.Changes {
					paths = append(paths, ch.Kind+" "+ch.Path)
				}
				send(api.EventTool, "file change: "+strings.Join(paths, ", "))
			case "error":
				send(api.EventStatus, "error: "+it.Message)
			case "reasoning":
			default:
				sum.Skipped++
			}
		default:
			sum.Skipped++
		}
	}, func() { sum.Skipped++ })

	sum.Duration = now().Sub(started)
	return sum, err
}
