package harness

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func fixedCodex() *Codex {
	c := NewCodex()
	c.Now = func() time.Time { return time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC) }
	return c
}

func TestCodexCommand(t *testing.T) {
	s := baseSpec()
	s.Model = "gpt-test"
	s.Billing = api.BillingMetered
	s.MaxCostUSD = 3
	cmd, err := NewCodex().Command(s)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Program != "codex" || cmd.Dir != "/work" || cmd.StdinFile != "/factory/in/prompt.md" {
		t.Errorf("command = %+v", cmd)
	}
	want := []string{"exec", "--json", "--sandbox=workspace-write", "--add-dir=/factory/out",
		"--ephemeral", "--output-last-message=/factory/out/last-message.txt", "--model=gpt-test"}
	for _, w := range want {
		if index(cmd.Args, w) < 0 {
			t.Errorf("args %v lack %q", cmd.Args, w)
		}
	}
	if cmd.Args[0] != "exec" {
		t.Errorf("first arg = %q", cmd.Args[0])
	}
	if cmd.Args[len(cmd.Args)-1] != "-" {
		t.Errorf("the prompt must come from standard input, last arg = %q", cmd.Args[len(cmd.Args)-1])
	}
	for _, a := range cmd.Args {
		if strings.Contains(a, "budget") {
			t.Errorf("codex has no budget flag: %v", cmd.Args)
		}
	}
}

func TestCodexCommandNeedsDirectories(t *testing.T) {
	s := baseSpec()
	s.OutDir = ""
	if _, err := NewCodex().Command(s); err == nil {
		t.Error("want an error without an output directory")
	}
}

func TestCodexReadSandboxRun(t *testing.T) {
	sum, events := readFixture(t, fixedCodex(), "codex-sandbox.jsonl")
	if !sum.Complete || sum.Failed {
		t.Errorf("complete=%v failed=%v", sum.Complete, sum.Failed)
	}
	if sum.Usage.Billing != api.BillingUnknown || sum.Usage.CostUSD != nil {
		t.Errorf("usage = %+v", sum.Usage)
	}
	if sum.Usage.InputTokens != 358115 || sum.Usage.OutputTokens != 2392 {
		t.Errorf("tokens = %d in, %d out", sum.Usage.InputTokens, sum.Usage.OutputTokens)
	}
	if sum.Turns != 1 || sum.Skipped != 0 || len(sum.Denials) != 0 {
		t.Errorf("summary = %+v", sum)
	}
	if sum.Harness.Name != "codex" {
		t.Errorf("harness = %+v", sum.Harness)
	}
	k := kinds(events)
	if k[api.EventMessage] != 4 || k[api.EventTool] != 20 || k[api.EventStatus] < 3 {
		t.Errorf("kinds = %v", k)
	}
	for _, e := range events {
		if !e.ValidKind() || e.Time.IsZero() || e.Text == "" || len(e.Text) > 260 {
			t.Errorf("bad event %+v", e)
		}
	}
}

func TestCodexBillingWhenTold(t *testing.T) {
	c := fixedCodex()
	c.Billing = api.BillingPlan
	sum, _ := readFixture(t, c, "codex-sandbox.jsonl")
	if sum.Usage.Billing != api.BillingPlan || sum.Usage.CostUSD != nil {
		t.Errorf("usage = %+v", sum.Usage)
	}
}

func TestCodexReadCutShortAndBadLines(t *testing.T) {
	in := "garbage\n" +
		`{"type":"thread.started","thread_id":"t"}` + "\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}` + "\n" +
		`{"type":"item.completed","item":{"type":"new_thing"}}` + "\n" +
		`{"type":"what"}` + "\n"
	var n int
	sum, err := fixedCodex().Read(strings.NewReader(in), func(api.Event) { n++ })
	if err != nil {
		t.Fatal(err)
	}
	if sum.Complete {
		t.Error("no turn.completed, so the summary must be incomplete")
	}
	if sum.Skipped != 3 || n != 2 {
		t.Errorf("skipped=%d events=%d", sum.Skipped, n)
	}
}

func TestCodexReadTurnFailed(t *testing.T) {
	in := `{"type":"turn.failed","error":{"message":"boom"}}` + "\n"
	var events []api.Event
	sum, _ := fixedCodex().Read(strings.NewReader(in), func(e api.Event) { events = append(events, e) })
	if sum.Complete || !sum.Failed {
		t.Errorf("a failed turn is not a clean end: %+v", sum)
	}
	if len(events) != 1 || !strings.Contains(events[0].Text, "boom") {
		t.Errorf("events = %+v", events)
	}
}

func TestCodexOnlyACleanEndIsComplete(t *testing.T) {
	ok := `{"type":"turn.started"}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}` + "\n"
	tests := []struct {
		name     string
		in       string
		complete bool
		failed   bool
	}{
		{"clean", ok, true, false},
		{"completed then failed", ok + `{"type":"turn.failed","error":{"message":"x"}}` + "\n", false, true},
		{"completed then a new turn that never ends", ok + `{"type":"turn.started"}` + "\n", false, false},
		{"two clean turns", ok + ok, true, false},
		{"started only", `{"type":"turn.started"}` + "\n", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sum, _ := fixedCodex().Read(strings.NewReader(tc.in), func(api.Event) {})
			if sum.Complete != tc.complete || sum.Failed != tc.failed {
				t.Errorf("complete=%v failed=%v", sum.Complete, sum.Failed)
			}
		})
	}
}

func TestCodexTokenSumsSaturate(t *testing.T) {
	big := `{"type":"turn.completed","usage":{"input_tokens":9223372036854775807,"output_tokens":9223372036854775807}}` + "\n"
	sum, _ := fixedCodex().Read(strings.NewReader(big+big), func(api.Event) {})
	if sum.Usage.InputTokens != math.MaxInt64 || sum.Usage.OutputTokens != math.MaxInt64 || !sum.TokensSaturated {
		t.Errorf("summary = %+v", sum)
	}
}
