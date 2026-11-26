package acceptance

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/openagentix/factory/internal/api"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func sampleResults() []Result {
	return []Result{
		{ID: 1, Text: "A customer can move a booking to another day", State: api.StateWorking, Check: "x", Ran: true},
		{ID: 2, Text: "Works on a phone", State: api.StateCouldNotConfirm, Note: NoteNoCheck},
		{ID: 3, Text: "An email goes out when a booking moves", State: api.StateNotWorking, Check: "x", Ran: true, ExitCode: 1},
		{ID: 4, Text: "The page loads in under two seconds", State: api.StateNotWorking, Check: "x", Ran: true, ExitCode: -1, TimedOut: true, Note: NoteTimedOut},
		{ID: 5, Text: "Old clients still work", State: api.StateCouldNotConfirm, Check: "x", Note: NoteTotalLimit},
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs.\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestRender_Golden(t *testing.T) {
	golden(t, "list.txt.golden", RenderText(sampleResults()))
	golden(t, "list.md.golden", RenderMarkdown(sampleResults()))
}

func TestRender_NoDeveloperWords(t *testing.T) {
	banned := regexp.MustCompile(`(?i)pull request|merge|\bdiff\b|review|branch|commit`)
	for _, out := range []string{RenderText(sampleResults()), RenderMarkdown(sampleResults())} {
		if m := banned.FindString(out); m != "" {
			t.Errorf("the list holds %q:\n%s", m, out)
		}
	}
}

func TestRender_OneLinePerCriterion(t *testing.T) {
	for _, out := range []string{RenderText(sampleResults()), RenderMarkdown(sampleResults())} {
		lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		if len(lines) != 5 {
			t.Errorf("lines = %d:\n%s", len(lines), out)
		}
	}
	if RenderText(nil) != "" || RenderMarkdown(nil) != "" {
		t.Error("an empty list must render as nothing")
	}
}

var hostile = []string{
	"[click me](http://evil.example/steal)",
	"![tracker](http://evil.example/p.png)",
	"@everyone please look @someone",
	"# A heading\n## and another\n---\n> quote\n- item\n1. item",
	"<img src=x onerror=alert(1)><script>alert(1)</script><a href=\"http://evil.example\">x</a>",
	"plain link http://evil.example/auto and www.evil.example and user@example.invalid",
	"back`tick``s and ```fence\ncode\n``` end",
	"line one\r\nline two\n\n\nline three\ttab",
	"ansi \x1b[31mred\x1b[0m bell \a nul \x00 end",
	"bidi \u202eevil\u202c and zero width \u200b\u200d and line sep \u2028 para \u2029",
	"bad utf8 \xff\xfe bytes",
	"<!-- hidden comment --> &lt;b&gt; &#x3c;i&#x3e; | table | cell |",
	"[ref]: http://evil.example \"title\"\n[ref]",
	strings.Repeat("long ", 500),
	"",
	"   \n\t  ",
}

func TestRender_HostileTextIsInert(t *testing.T) {
	var results []Result
	for i, h := range hostile {
		results = append(results, Result{ID: i + 1, Text: h, State: api.StateNotWorking, Check: "x", Ran: true})
	}
	n := len(results)

	md := RenderMarkdown(results)
	lines := strings.Split(strings.TrimSuffix(md, "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("lines = %d, want %d:\n%s", len(lines), n, md)
	}
	line := regexp.MustCompile("^\\d+\\. \\[ \\] `([^`]*)` - not working yet\\. [A-Za-z ,.]+$")
	for i, l := range lines {
		m := line.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("line %d does not have the fixed shape: %q", i+1, l)
			continue
		}
		if len(m[1]) > 400 {
			t.Errorf("line %d: wording not cut: %d bytes", i+1, len(m[1]))
		}
	}

	txt := RenderText(results)
	tl := strings.Split(strings.TrimSuffix(txt, "\n"), "\n")
	if len(tl) != n {
		t.Fatalf("text lines = %d, want %d", len(tl), n)
	}
	for _, out := range []string{md, txt} {
		for _, r := range strings.TrimSuffix(out, "\n") {
			if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029') {
				t.Errorf("output holds the character U+%04X", r)
			}
		}
	}
}

func TestRender_StateMarksAndNotes(t *testing.T) {
	txt := RenderText(sampleResults())
	for _, want := range []string{
		"1. [x] \"A customer can move a booking to another day\" - working\n",
		"2. [?] \"Works on a phone\" - could not confirm. No check exists for it.\n",
		"3. [ ] \"An email goes out when a booking moves\" - not working yet. Its check did not pass.\n",
		"4. [ ] \"The page loads in under two seconds\" - not working yet. Its check ran out of time.\n",
		"5. [?] \"Old clients still work\" - could not confirm. Time ran out before this was checked.\n",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
}

func TestRender_AnUnknownStateIsCouldNotConfirm(t *testing.T) {
	out := RenderText([]Result{{ID: 1, Text: "x", State: "done"}})
	if !strings.Contains(out, "[?]") || strings.Contains(out, "working -") {
		t.Errorf("out = %q", out)
	}
}

func TestEngineeringNotes(t *testing.T) {
	notes := strings.Join(EngineeringNotes(&Report{SharedChecks: [][]int{{1, 2}}}, []int{3}), "\n")
	for _, want := range []string{"wrong reason", "code a check calls", "Criteria 1, 2", "criterion 3", "Already true before building"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if static := EngineeringNotes(nil, nil); len(static) < 3 {
		t.Errorf("static notes = %v", static)
	}
}
