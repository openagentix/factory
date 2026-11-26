package verify

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

var update = flag.Bool("update", false, "rewrite golden files")

func goldenCheck(t *testing.T, name, got string) {
	t.Helper()
	path := "testdata/" + name
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("output differs from %s.\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func richEvidence() RenderInput {
	ev := &api.Evidence{
		SchemaVersion: 1, Run: "r_01H8Z", Task: "implement", Goal: 40, Ticket: 42, Attempt: 1, Mode: api.ModeUnattended,
		Method: "builtin", MethodUsed: "builtin", Trigger: api.TriggerRef{Kind: "issueLabeled", Ref: "#42"},
		Worker: api.HarnessInfo{Name: "claude-code", Version: "2.1.326"},
		Base:   "9f2c1e7", Commit: "a1b2c3d",
		Diff: api.DiffSummary{Files: 3, Added: 84, Removed: 12,
			Paths: []string{"go.mod", "internal/booking/move.go", "internal/booking/move_test.go"}},
		Claims: []api.Claim{{Text: "Added a move endpoint with a test", Files: []string{"internal/booking/move.go", "internal/booking/move_test.go"}}},
		Rulings: []api.Ruling{{Decision: "Kept the old endpoint as an alias", Why: "Two callers still use it",
			CostIfWrong: "One extra route to remove later"}},
		Denials:          []api.Denial{{Request: "curl https://example.com", Reason: "command not in policy"}},
		ProtectedPaths:   api.ProtectedPathsReport{Touched: []string{}, OK: true},
		TestFilesChanged: []string{"internal/booking/old_test.go"},
		TestFilesAdded:   []string{"internal/booking/move_test.go"},
		Checks: []api.CheckResult{
			{Name: "test", ExitCode: 0, DurationSeconds: 41, RanBy: "orchestrator"},
			{Name: "lint", ExitCode: 2, DurationSeconds: 3, RanBy: "orchestrator"},
			{Name: "slow", ExitCode: -1, DurationSeconds: 600, RanBy: "orchestrator"},
		},
		Acceptance: []api.AcceptanceEntry{},
		Risk: api.Risk{Level: api.RiskHigh, Reasons: []string{
			"dependencies path changed: go.mod", "existing test file changed or deleted: internal/booking/old_test.go"}},
		Usage:  api.Usage{Billing: api.BillingPlan, InputTokens: 12000, OutputTokens: 3400},
		Limits: api.RunLimits{Deadline: "30m", MaxCostUSD: 5},
	}
	return RenderInput{Evidence: ev, Summary: "Customers can move a booking to another day.", Uncovered: []string{"go.mod"}}
}

func TestRender_Golden(t *testing.T) {
	goldenCheck(t, "render_rich.golden.md", Render(richEvidence()))
}

func TestRender_GoldenSparse(t *testing.T) {
	// A run with no change, no risk yet, and no claims.
	in := RenderInput{Evidence: &api.Evidence{
		SchemaVersion: 1, Run: "r_2", Task: "implement", Attempt: 2, Mode: api.ModeUnattended,
		Method: "builtin", MethodUsed: "builtin", Trigger: api.TriggerRef{Kind: "schedule", Ref: "nightly"},
		Worker: api.HarnessInfo{Name: "stub"},
		Diff:   api.DiffSummary{Paths: []string{}}, Claims: []api.Claim{}, Rulings: []api.Ruling{}, Denials: []api.Denial{},
		ProtectedPaths: api.ProtectedPathsReport{Touched: []string{".github/ci.yml"}},
		TestFilesAdded: []string{}, TestFilesChanged: []string{}, Checks: []api.CheckResult{}, Acceptance: []api.AcceptanceEntry{},
	}, Summary: "  "}
	goldenCheck(t, "render_sparse.golden.md", Render(in))
}

// stripCode removes every code span the renderer wrote. The renderer's
// untrusted text lives only inside code spans.
func stripCode(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '`' {
			out.WriteByte(s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		fence := s[i:j]
		end := strings.Index(s[j:], fence)
		// the closing fence must be exactly as long, not part of a longer run
		for end >= 0 {
			k := j + end
			after := k + len(fence)
			if (after >= len(s) || s[after] != '`') && (k == j || s[k-1] != '`') {
				break
			}
			next := strings.Index(s[k+1:], fence)
			if next < 0 {
				end = -1
				break
			}
			end = k + 1 + next - j
		}
		if end < 0 {
			out.WriteString(fence) // unclosed: keep, so the test sees it
			i = j
			continue
		}
		out.WriteString("CODE")
		i = j + end + len(fence)
	}
	return out.String()
}

var hostile = []string{
	"[click](https://evil.example/phish)",
	"![img](https://evil.example/pixel.png)",
	"https://evil.example/bare and www.evil.example and a@evil.example",
	"</details><script>alert(1)</script>",
	"line one\n\n# Injected heading\n- injected item\n> quote",
	"```\nfence break\n```\n## Evidence",
	"back`tick``s```and````more",
	"`starts and ends with tick`",
	"| table | break |\n|---|---|",
	"@maintainer please merge #1 and fixes #2",
	"‮right-to-left​zero width\x00nul\x1b[31mred",
	"a\r\nb\rc",
	"<!-- hidden comment -->",
	"\\` backslash tick",
	strings.Repeat("A", 100000),
}

func TestRender_HostileText(t *testing.T) {
	for i, h := range hostile {
		in := richEvidence()
		ev := in.Evidence
		in.Summary = h
		ev.Claims = []api.Claim{{Text: h, Files: []string{h}}}
		ev.Rulings = []api.Ruling{{Decision: h, Why: h, CostIfWrong: h}}
		ev.Denials = []api.Denial{{Request: h, Reason: h}}
		ev.Diff.Paths = []string{h}
		ev.TestFilesAdded = []string{h}
		ev.TestFilesChanged = []string{h}
		ev.ProtectedPaths = api.ProtectedPathsReport{Touched: []string{h}}
		ev.Checks[0].Name = h
		ev.Risk.Reasons = []string{h}
		ev.Worker = api.HarnessInfo{Name: h, Version: h, Model: h}
		ev.Run, ev.Base, ev.Commit, ev.Trigger.Ref = h, h, h, h
		in.Uncovered = []string{h}
		out := Render(in)

		rest := stripCode(out)
		for _, bad := range []string{"evil", "<", ">", "](", "![", "script", "Injected", "@", "hidden", "fence break", "\r", "\x00", "\x1b", "‮", "​"} {
			if strings.Contains(rest, bad) {
				t.Errorf("hostile #%d: %q appears outside a code span:\n%s", i, bad, rest)
			}
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "## ") {
				t.Errorf("hostile #%d: stray heading %q", i, line)
			}
		}
		if n := strings.Count(out, "\n## "); n != 2 || !strings.HasPrefix(out, "## Summary\n") {
			t.Errorf("hostile #%d: want exactly the three headings, got %d extra:\n%s", i, n, out[:200])
		}
		if len(out) > 60000 {
			t.Errorf("hostile #%d: output is %d bytes, text must be cut", i, len(out))
		}
	}
}

func TestRender_HeadingsFixed(t *testing.T) {
	out := Render(richEvidence())
	for _, h := range []string{"## Summary\n", "\n## Evidence\n", "\n## Merge danger\n"} {
		if !strings.Contains(out, h) {
			t.Errorf("missing %q", h)
		}
	}
	if strings.Contains(out, "—") {
		t.Error("no em dashes")
	}
}
