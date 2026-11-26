package verify

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/openagentix/factory/internal/api"
)

// RenderInput is what Render needs. Summary is the worker's own summary. It
// is not part of the evidence bundle. Uncovered comes from BuildEvidence.
type RenderInput struct {
	Evidence  *api.Evidence
	Summary   string
	Uncovered []string
}

// Limits on untrusted text, so one hostile value cannot flood the page.
const (
	maxInlineRunes = 400
	maxSummaryRune = 2000
	maxListItems   = 50
)

// Render writes the engineering view of the evidence as a markdown body with
// three parts: Summary, Evidence and Merge danger.
//
// Every value that came from the worker, a file name or a git ref is
// untrusted. It is written only inside an inline code span, after control and
// invisible characters are removed, newlines are folded into spaces and the
// text is cut to a fixed length. Inside a code span markdown has no effect:
// no link, image, heading, HTML, table or mention can start there, and the
// span is fenced with more backticks than the text holds, so it cannot be
// closed early. The page's own words and headings are fixed text.
func Render(in RenderInput) string {
	ev := in.Evidence
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("## Summary\n\n")
	if s := clean(in.Summary, maxSummaryRune); s == "" {
		w("The worker gave no summary.\n")
	} else {
		w("%s\n", span(s))
	}

	w("\n## Evidence\n\n")
	w("- Task: %s, attempt %d, run %s\n", code(ev.Task), ev.Attempt, code(ev.Run))
	w("- Started by: %s %s\n", code(ev.Trigger.Kind), code(ev.Trigger.Ref))
	w("- Worker: %s\n", code(strings.TrimSpace(ev.Worker.Name+" "+ev.Worker.Version+" "+ev.Worker.Model)))
	if ev.Commit != "" || ev.Base != "" {
		w("- Commit: %s on top of %s\n", code(ev.Commit), code(ev.Base))
	}
	w("- Change: %d files, %d lines added, %d removed\n", ev.Diff.Files, ev.Diff.Added, ev.Diff.Removed)
	pathList(&b, "  ", ev.Diff.Paths)
	if len(ev.ProtectedPaths.Touched) == 0 {
		w("- Protected paths: none touched\n")
	} else {
		w("- Protected paths touched, so verification fails:\n")
		pathList(&b, "  ", ev.ProtectedPaths.Touched)
	}
	w("- Test files added: %s\n", inlineList(ev.TestFilesAdded))
	w("- Existing test files changed or deleted: %s\n", inlineList(ev.TestFilesChanged))

	if len(ev.Checks) == 0 {
		w("- Checks: none ran\n")
	} else {
		w("- Checks, run by the orchestrator:\n")
		for _, c := range ev.Checks {
			w("  - %s: %s, exit code %d, %d s\n", code(c.Name), checkWord(c.ExitCode), c.ExitCode, c.DurationSeconds)
		}
	}
	if len(ev.Claims) == 0 {
		w("- Claims: none\n")
	} else {
		w("- Claims, as the worker stated them:\n")
		for _, c := range firstClaims(ev.Claims) {
			w("  - %s (files: %s)\n", code(c.Text), inlineList(c.Files))
		}
		more(&b, "  ", len(ev.Claims))
	}
	if len(ev.Rulings) == 0 {
		w("- Rulings: none\n")
	} else {
		w("- Rulings, choices the worker made alone:\n")
		for _, r := range ev.Rulings {
			w("  - Decision %s. Why: %s. Cost if wrong: %s.\n", code(r.Decision), code(r.Why), code(r.CostIfWrong))
		}
	}
	if len(ev.Denials) > 0 {
		w("- Requests the permission policy denied:\n")
		for _, d := range ev.Denials {
			w("  - %s, because %s\n", code(d.Request), code(d.Reason))
		}
	}
	if len(in.Uncovered) == 0 {
		w("- Not confirmed: every changed file is covered by a claim\n")
	} else {
		w("- Not confirmed: changed files that no claim covers:\n")
		pathList(&b, "  ", in.Uncovered)
	}

	w("\n## Merge danger\n\n")
	if ev.Risk.Level == "" {
		w("Risk: not decided yet. It is set when the review ends.\n")
	} else {
		w("Risk: %s\n\n", code(ev.Risk.Level))
		for _, r := range ev.Risk.Reasons {
			w("- %s\n", code(r))
		}
	}
	if ev.Gate != "" {
		w("\nGate: %s\n", code(ev.Gate))
	}
	return b.String()
}

func checkWord(exit int) string {
	if exit == 0 {
		return "passed"
	}
	return "did not pass"
}

// pathList writes up to maxListItems paths as list items.
func pathList(b *strings.Builder, indent string, paths []string) {
	for i, p := range paths {
		if i == maxListItems {
			fmt.Fprintf(b, "%s- and %d more\n", indent, len(paths)-maxListItems)
			return
		}
		fmt.Fprintf(b, "%s- %s\n", indent, code(p))
	}
}

func more(b *strings.Builder, indent string, n int) {
	if n > maxListItems {
		fmt.Fprintf(b, "%s- and %d more\n", indent, n-maxListItems)
	}
}

// firstClaims returns at most maxListItems claims.
func firstClaims(c []api.Claim) []api.Claim {
	if len(c) > maxListItems {
		return c[:maxListItems]
	}
	return c
}

// inlineList writes a short list on one line, each item in its own span.
func inlineList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	var parts []string
	for i, it := range items {
		if i == 10 {
			parts = append(parts, fmt.Sprintf("and %d more", len(items)-10))
			break
		}
		parts = append(parts, code(it))
	}
	return strings.Join(parts, ", ")
}

// clean makes untrusted text safe to put in one line: valid UTF-8, no
// control, format or separator characters, runs of white space folded into
// one space, and at most max runes.
func clean(s string, max int) string {
	s = strings.ToValidUTF8(s, "?")
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Zs):
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || unicode.Is(unicode.Cs, r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > max {
		out = string([]rune(out)[:max]) + "..."
	}
	return out
}

// code cleans untrusted text and writes it as one inline code span.
func code(s string) string {
	c := clean(s, maxInlineRunes)
	if c == "" {
		return "(empty)"
	}
	return span(c)
}

// span writes cleaned text as a code span, with a fence longer than any run
// of backticks in the text. Text that starts or ends with a backtick gets a
// space inside each end, which markdown strips again.
func span(c string) string {
	longest, run := 0, 0
	for _, r := range c {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(c, "`") || strings.HasSuffix(c, "`") {
		c = " " + c + " "
	}
	return fence + c + fence
}
