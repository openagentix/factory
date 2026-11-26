package harness

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func readFixture(t *testing.T, a Adapter, name string) (Summary, []api.Event) {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var events []api.Event
	sum, err := a.Read(f, func(e api.Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return sum, events
}

func fixedClaude() *Claude {
	c := NewClaude()
	c.Now = func() time.Time { return time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC) }
	return c
}

func kinds(events []api.Event) map[string]int {
	m := map[string]int{}
	for _, e := range events {
		m[e.Kind]++
	}
	return m
}

func TestClaudeReadMeteredRunWithADenial(t *testing.T) {
	sum, events := readFixture(t, fixedClaude(), "claude-denial.jsonl")
	if !sum.Complete || sum.Failed {
		t.Errorf("complete=%v failed=%v", sum.Complete, sum.Failed)
	}
	if sum.Usage.Billing != api.BillingMetered {
		t.Errorf("billing = %q", sum.Usage.Billing)
	}
	if sum.Usage.CostUSD == nil || *sum.Usage.CostUSD < 0.057 || *sum.Usage.CostUSD > 0.058 {
		t.Errorf("cost = %v", sum.Usage.CostUSD)
	}
	if sum.Usage.InputTokens != 12+9869+118178 || sum.Usage.OutputTokens != 2052 {
		t.Errorf("tokens = %d in, %d out", sum.Usage.InputTokens, sum.Usage.OutputTokens)
	}
	if sum.Turns != 9 || sum.Duration != 22236*time.Millisecond {
		t.Errorf("turns=%d duration=%v", sum.Turns, sum.Duration)
	}
	if len(sum.Denials) != 1 || !strings.HasPrefix(sum.Denials[0].Request, "Bash: cat > test_words.py") || sum.Denials[0].Reason == "" {
		t.Errorf("denials = %+v", sum.Denials)
	}
	if sum.Harness.Name != "claude-code" || sum.Harness.Version != "2.1.326" || sum.Harness.Model != "claude-sonnet-5-5" {
		t.Errorf("harness = %+v", sum.Harness)
	}
	k := kinds(events)
	if k[api.EventDenied] != 1 || k[api.EventTool] != 8 || k[api.EventMessage] != 3 || k[api.EventStatus] < 2 {
		t.Errorf("kinds = %v", k)
	}
	for _, e := range events {
		if !e.ValidKind() || e.Time.IsZero() || e.Text == "" {
			t.Errorf("bad event %+v", e)
		}
		if len(e.Text) > 260 {
			t.Errorf("event text too long: %d", len(e.Text))
		}
	}
	for _, e := range events {
		if e.Kind == api.EventDenied && !strings.Contains(e.Text, "cat > test_words.py") {
			t.Errorf("denied event should name the request: %q", e.Text)
		}
	}
}

func TestClaudeReadPlanBillingKeepsNoDollarFigure(t *testing.T) {
	sum, _ := readFixture(t, fixedClaude(), "claude-plan.jsonl")
	if sum.Usage.Billing != api.BillingPlan {
		t.Errorf("billing = %q", sum.Usage.Billing)
	}
	if sum.Usage.CostUSD != nil {
		t.Errorf("a plan run must keep no dollar figure, got %v", *sum.Usage.CostUSD)
	}
	if sum.Usage.OutputTokens != 4 || sum.Turns != 1 || !sum.Complete {
		t.Errorf("summary = %+v", sum)
	}
	// The rate limit event is a kind the adapter does not know.
	if sum.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", sum.Skipped)
	}
}

func TestClaudeReadPluginRun(t *testing.T) {
	sum, events := readFixture(t, fixedClaude(), "claude-plugin.jsonl")
	if len(sum.Denials) != 1 || !strings.HasPrefix(sum.Denials[0].Request, "Bash: sed -i") {
		t.Errorf("denials = %+v", sum.Denials)
	}
	var skill bool
	for _, e := range events {
		if e.Kind == api.EventTool && strings.HasPrefix(e.Text, "Skill: superpowers:test-driven-development") {
			skill = true
		}
	}
	if !skill {
		t.Error("expected a tool event for the skill call")
	}
}

func TestClaudeReadUsageValidates(t *testing.T) {
	for _, name := range []string{"claude-denial.jsonl", "claude-plan.jsonl", "claude-plugin.jsonl"} {
		sum, _ := readFixture(t, fixedClaude(), name)
		r := api.Result{SchemaVersion: 1, Status: api.StatusNoChange, Summary: "x", Usage: sum.Usage, Harness: sum.Harness}
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestClaudeReadCutShortStreamIsIncomplete(t *testing.T) {
	in := `{"type":"system","subtype":"init","apiKeySource":"none","claude_code_version":"2.1.326","model":"m"}
{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}
`
	var n int
	sum, err := fixedClaude().Read(strings.NewReader(in), func(api.Event) { n++ })
	if err != nil {
		t.Fatal(err)
	}
	if sum.Complete {
		t.Error("a stream with no final event must be incomplete")
	}
	if sum.Usage.Billing != api.BillingPlan {
		t.Errorf("billing from the first event should still be known, got %q", sum.Usage.Billing)
	}
	if n != 2 {
		t.Errorf("events = %d", n)
	}
}

func TestClaudeReadSkipsAndCountsBadLines(t *testing.T) {
	in := "not json at all\n" +
		"{\"type\":\"mystery\"}\n" +
		"[1,2,3]\n" +
		"\n" +
		"{\"type\":\"assistant\",\"message\":\"oops a string\"}\n" +
		"{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"},{\"type\":\"surprise\"},7]}}\n" +
		"{\"type\":\"result\",\"num_turns\":\"many\"}\n" +
		"{\"type\":\"result\",\"subtype\":\"success\",\"num_turns\":2,\"duration_ms\":5,\"usage\":{\"output_tokens\":3}}\n"
	var events []api.Event
	sum, err := fixedClaude().Read(strings.NewReader(in), func(e api.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Complete || sum.Turns != 2 || sum.Usage.OutputTokens != 3 {
		t.Errorf("summary = %+v", sum)
	}
	if sum.Skipped < 4 {
		t.Errorf("skipped = %d", sum.Skipped)
	}
	if len(events) != 2 { // the message and the final status
		t.Errorf("events = %+v", events)
	}
}

func TestClaudeReadHandlesVeryLongLine(t *testing.T) {
	long := strings.Repeat("a", 3<<20)
	in := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + long + `"}]}}` + "\n" +
		`{"type":"result","subtype":"success","num_turns":1}` + "\n"
	var events []api.Event
	sum, err := fixedClaude().Read(strings.NewReader(in), func(e api.Event) { events = append(events, e) })
	if err != nil || !sum.Complete {
		t.Fatalf("err=%v complete=%v", err, sum.Complete)
	}
	if len(events[0].Text) > 300 {
		t.Errorf("event text must be cut, got %d", len(events[0].Text))
	}
}

func TestClaudeReadErrorResult(t *testing.T) {
	in := `{"type":"result","subtype":"error_max_budget_usd","is_error":true,"num_turns":3}` + "\n"
	sum, _ := fixedClaude().Read(strings.NewReader(in), func(api.Event) {})
	if !sum.Complete || !sum.Failed {
		t.Errorf("summary = %+v", sum)
	}
}

func TestReadDropsAHugeLineAndKeepsGoing(t *testing.T) {
	huge := strings.Repeat("b", maxLine+10)
	in := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + huge + `"}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"after"}]}}` + "\n"
	var events []api.Event
	sum, err := fixedClaude().Read(strings.NewReader(in), func(e api.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if sum.Skipped != 1 || len(events) != 1 || events[0].Text != "after" {
		t.Errorf("skipped=%d events=%+v", sum.Skipped, events)
	}
}
