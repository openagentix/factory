package acceptance

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
)

// maxWordingRunes is how much of a criterion's wording is shown.
const maxWordingRunes = 300

// mark is the sign before a line. The words are in the line as well, so the
// state never depends on the mark alone.
func mark(s api.AcceptanceState) string {
	switch s {
	case api.StateWorking:
		return "[x]"
	case api.StateNotWorking:
		return "[ ]"
	}
	return "[?]"
}

// stateLine is the state in plain words and a short neutral note. The notes
// are fixed phrases. Nothing from a check or a file is used.
func stateLine(r Result) string {
	s := r.State
	if !s.Valid() {
		s = api.StateCouldNotConfirm
	}
	line := s.Name()
	switch {
	case s == api.StateWorking:
	case r.TimedOut || r.Note == NoteTimedOut:
		line += ". Its check ran out of time."
	case r.Note == NoteNoCheck:
		line += ". No check exists for it."
	case r.Note == NoteTotalLimit:
		line += ". Time ran out before this was checked."
	case r.Note == NoteCouldNotRun:
		line += ". Its check could not run."
	case s == api.StateNotWorking:
		line += ". Its check did not pass."
	}
	return line
}

// wording makes the person's text safe to show: one line, control and
// invisible characters shown as <U+XXXX>, and a length limit.
func wording(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\u2800' || r == '\u3164' || unicode.Is(unicode.Cf, r) {
			return -1 // blank look-alikes and invisible characters
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "(no wording)"
	}
	return repo.Visible(s, maxWordingRunes)
}

// RenderText gives the list for a person as plain text, one line per
// criterion: a number, a mark, the person's wording in quotation marks (inner
// quotes and backslashes escaped), the state in words and,
// where there is one, a short note. The wording is untrusted and is shown as
// text only.
func RenderText(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		q := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(wording(r.Text))
		fmt.Fprintf(&b, "%s. %s \"%s\" - %s\n", strconv.Itoa(r.ID), mark(r.State), q, stateLine(r))
	}
	return b.String()
}

// RenderMarkdown gives the same list as markdown. The wording is untrusted,
// so it is always put in a code span, where no link, image, mention, heading
// or HTML can form. A backtick in the wording becomes an apostrophe, so it
// cannot end the span. A line starts with the number and the mark, so the
// wording can never start a heading, a quote or a list.
func RenderMarkdown(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		w := strings.ReplaceAll(wording(r.Text), "`", "'")
		fmt.Fprintf(&b, "%s. %s `%s` - %s\n", strconv.Itoa(r.ID), mark(r.State), w, stateLine(r))
	}
	return b.String()
}

// EngineeringNotes are for the engineering view, not for the person's list.
// They say what the states do not prove, and add facts about this run: checks
// shared by several criteria and criteria that were already working before
// any building. rep and alreadyWorking may be nil.
func EngineeringNotes(rep *Report, alreadyWorking []int) []string {
	notes := []string{
		"A criterion is working when its check passes. A check can pass for the wrong reason.",
		"The builder can change the code a check calls, so a check can pass without the work being done. This is not prevented.",
		"A check is a shell command. A command that reaches outside its copy of the files is not stopped: there is no operating system sandbox.",
	}
	if rep != nil {
		for _, ids := range rep.SharedChecks {
			notes = append(notes, fmt.Sprintf("Criteria %s name the same check.", joinIDs(ids)))
		}
		if rep.TotalLimitReached {
			notes = append(notes, "The time limit for the whole run ended early, so some criteria were not checked.")
		}
	}
	if len(alreadyWorking) > 0 {
		ids := append([]int(nil), alreadyWorking...)
		sort.Ints(ids)
		word := "criterion"
		if len(ids) > 1 {
			word = "criteria"
		}
		notes = append(notes, fmt.Sprintf("Already true before building: %s %s. This is expected for something that must keep working. Otherwise the check cannot fail and proves nothing.", word, joinIDs(ids)))
	}
	return notes
}

func joinIDs(ids []int) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, ", ")
}
