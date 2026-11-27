package githost

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// The factory marks its own text with one HTML comment on the last line of a
// comment or an issue body. A marker only counts when the author is the
// factory's own user. Text from anyone else is never trusted for markers.
//
//	<!-- oxf:run id=RUN -->
//	<!-- oxf:answer -->
//	<!-- oxf:claim machine=MACHINE run=RUN -->
//	<!-- oxf:consequence run=RUN attempt=N commit=COMMIT answer=yes -->
//	<!-- oxf:deps {"criteria":[1],"dependsOn":[12]} -->

var idRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type markerKind int

const (
	markerNone markerKind = iota
	markerRun
	markerAnswer
	markerClaim
	markerConsequence
)

// Deps is the machine-read dependency block of a sub-issue body.
type Deps struct {
	// Criteria are the numbered acceptance criteria the ticket serves.
	Criteria []int `json:"criteria"`
	// DependsOn are issue numbers of tickets this one waits for.
	DependsOn []int `json:"dependsOn"`
}

var errBadID = errors.New("githost: id must be 1 to 64 letters, digits, dot, dash or underscore")

// RunMarker returns the marker line for a run id.
func RunMarker(runID string) (string, error) {
	if !idRE.MatchString(runID) {
		return "", errBadID
	}
	return "<!-- oxf:run id=" + runID + " -->", nil
}

// AnswerMarker returns the marker line for an answer typed by a person.
func AnswerMarker() string { return "<!-- oxf:answer -->" }

// ClaimMarkerText returns the marker line for a claim.
func ClaimMarkerText(machineID, runID string) (string, error) {
	if !idRE.MatchString(machineID) || !idRE.MatchString(runID) {
		return "", errBadID
	}
	return "<!-- oxf:claim machine=" + machineID + " run=" + runID + " -->", nil
}

// neutralise breaks every comment opener in text that is not ours, so it
// cannot carry a marker.
func neutralise(s string) string { return strings.ReplaceAll(s, "<!--", "&lt;!--") }

// withMarker appends a marker line to comment text after making the text inert.
func withMarker(text, marker string) string {
	return safeComment(text) + "\n\n" + marker
}

// lastLine returns the last non-empty line of body, trimmed.
func lastLine(body string) string {
	body = strings.TrimRight(body, " \t\r\n")
	if i := strings.LastIndex(body, "\n"); i >= 0 {
		body = body[i+1:]
	}
	return strings.TrimSpace(body)
}

// markerInner returns the text inside a marker line, after "oxf:".
func markerInner(line string) (string, bool) {
	const open, closing = "<!-- oxf:", " -->"
	if !strings.HasPrefix(line, open) || !strings.HasSuffix(line, closing) || len(line) < len(open)+len(closing) {
		return "", false
	}
	return line[len(open) : len(line)-len(closing)], true
}

// parseMarker reads the marker on the last line of body. The caller must
// check the author before it believes the result.
func parseMarker(body string) (markerKind, string, *ClaimMarker) {
	inner, ok := markerInner(lastLine(body))
	if !ok {
		return markerNone, "", nil
	}
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return markerNone, "", nil
	}
	kv := func(f string, key string) (string, bool) {
		v, ok := strings.CutPrefix(f, key+"=")
		return v, ok && idRE.MatchString(v)
	}
	switch fields[0] {
	case "run":
		if len(fields) == 2 {
			if id, ok := kv(fields[1], "id"); ok {
				return markerRun, id, nil
			}
		}
	case "answer":
		if len(fields) == 1 {
			return markerAnswer, "", nil
		}
	case "consequence":
		if _, ok := ParseConsequence(body); ok {
			return markerConsequence, "", nil
		}
	case "claim":
		if len(fields) == 3 {
			m, ok1 := kv(fields[1], "machine")
			r, ok2 := kv(fields[2], "run")
			if ok1 && ok2 {
				return markerClaim, r, &ClaimMarker{Machine: m, Run: r}
			}
		}
	}
	return markerNone, "", nil
}

// withDeps appends the dependency block to an issue body.
func withDeps(body string, d Deps) (string, error) {
	if d.Criteria == nil {
		d.Criteria = []int{}
	}
	if d.DependsOn == nil {
		d.DependsOn = []int{}
	}
	b, err := json.Marshal(d) // json escapes < > & so the block cannot end early
	if err != nil {
		return "", err
	}
	block := "<!-- oxf:deps " + string(b) + " -->"
	if text := safeBody(body); text != "" {
		return text + "\n\n" + block, nil
	}
	return block, nil
}

// ParseDeps reads the dependency block on the last line of an issue body.
// The caller must only believe it for an issue whose Trusted field is true.
func ParseDeps(body string) (Deps, bool) {
	inner, ok := markerInner(lastLine(body))
	if !ok {
		return Deps{}, false
	}
	raw, ok := strings.CutPrefix(inner, "deps ")
	if !ok {
		return Deps{}, false
	}
	var d Deps
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return Deps{}, false
	}
	return d, true
}

// RunMarkerText reads a factory comment body that ends with a run marker. It
// gives the text before that final marker line, and the run id. ok is false
// for any other body: no marker, a claim marker, an answer marker, a marker
// that is not on the last line. The caller must check the author. A marker
// inside the text stays part of the text.
func RunMarkerText(body string) (text, runID string, ok bool) {
	kind, id, _ := parseMarker(body)
	if kind != markerRun {
		return "", "", false
	}
	trimmed := strings.TrimRight(body, " \t\r\n")
	if i := strings.LastIndex(trimmed, "\n"); i >= 0 {
		return strings.TrimRight(trimmed[:i], " \t\r\n"), id, true
	}
	return "", id, true
}

// slotRE is what the part after the dot of a run marker id may be: a publish
// slot (record 0003, "Publish steps"). It has no dot, so a slot can never
// stand for another run id that holds a dot.
var slotRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// MarkerIDBelongsToRun says whether the id of a run marker, "<run id>" or
// "<run id>.<slot>", is the marker of run runID. The id counts when it is
// exactly the run id, or the run id, a dot and a slot of up to 32 letters,
// digits, underscore or dash. A prefix alone never counts: run1 does not own
// run10, run1x, "run1." or "run1..". A caller that knows other runs whose
// ids hold a dot must also check that the id is not exactly one of them.
func MarkerIDBelongsToRun(id, runID string) bool {
	if runID == "" || !idRE.MatchString(runID) {
		return false
	}
	if id == runID {
		return true
	}
	slot, ok := strings.CutPrefix(id, runID+".")
	return ok && slotRE.MatchString(slot)
}

// ConsequenceMarker is the content of a consequence answer marker: the open
// exception it answers (run, attempt, commit) and the answer.
type ConsequenceMarker struct {
	Run     string
	Attempt int
	Commit  string
	Yes     bool
}

var commit40RE = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (m ConsequenceMarker) check() error {
	if !idRE.MatchString(m.Run) {
		return errBadID
	}
	if m.Attempt < 1 {
		return errors.New("githost: attempt must be a positive number")
	}
	if !commit40RE.MatchString(m.Commit) {
		return errors.New("githost: commit must be 40 lowercase hex characters")
	}
	return nil
}

// ConsequenceAnswerBody returns the exact comment of a yes or a no on a
// consequence (record 0004, "Consequences").
func ConsequenceAnswerBody(run string, attempt int, commit string, yes bool) (string, error) {
	m := ConsequenceMarker{Run: run, Attempt: attempt, Commit: commit, Yes: yes}
	if err := m.check(); err != nil {
		return "", err
	}
	text, word := "No, send it back.", "no"
	if yes {
		text, word = "Yes, that is what I want.", "yes"
	}
	return text + "\n\n<!-- oxf:consequence run=" + run + " attempt=" + strconv.Itoa(attempt) +
		" commit=" + commit + " answer=" + word + " -->", nil
}

// ParseConsequence reads a consequence marker on the last line of body. The
// caller must check the author before it believes the result. Exactly four
// fields, in this order, in single-space form.
func ParseConsequence(body string) (ConsequenceMarker, bool) {
	inner, ok := markerInner(lastLine(body))
	if !ok {
		return ConsequenceMarker{}, false
	}
	parts := strings.Split(inner, " ")
	if len(parts) != 5 || parts[0] != "consequence" {
		return ConsequenceMarker{}, false
	}
	val := func(f, key string) (string, bool) { return strings.CutPrefix(f, key+"=") }
	run, ok1 := val(parts[1], "run")
	att, ok2 := val(parts[2], "attempt")
	com, ok3 := val(parts[3], "commit")
	ans, ok4 := val(parts[4], "answer")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return ConsequenceMarker{}, false
	}
	if att == "" || att[0] == '0' || len(att) > 9 {
		return ConsequenceMarker{}, false
	}
	n := 0
	for _, r := range att {
		if r < '0' || r > '9' {
			return ConsequenceMarker{}, false
		}
		n = n*10 + int(r-'0')
	}
	m := ConsequenceMarker{Run: run, Attempt: n, Commit: com}
	switch ans {
	case "yes":
		m.Yes = true
	case "no":
	default:
		return ConsequenceMarker{}, false
	}
	if m.check() != nil {
		return ConsequenceMarker{}, false
	}
	return m, true
}

// ConsequenceQuestionBody returns the comment text of a consequence question:
// a code block of exactly five lines (record 0004). The phrase is one fixed
// line. The caller posts it with PostComment, which adds the run marker.
func ConsequenceQuestionBody(phrase, run string, attempt int, commit, machine string) (string, error) {
	m := ConsequenceMarker{Run: run, Attempt: attempt, Commit: commit}
	if err := m.check(); err != nil {
		return "", err
	}
	if !idRE.MatchString(machine) {
		return "", errBadID
	}
	if strings.TrimSpace(phrase) == "" || strings.ContainsAny(phrase, "\n\r`<>&@") {
		return "", errors.New("githost: the phrase must be one plain line")
	}
	return "```text\n" + phrase + "\nrun=" + run + "\nattempt=" + strconv.Itoa(attempt) +
		"\ncommit=" + commit + "\nmachine=" + machine + "\n```", nil
}

// EndsWithMarker reports whether the last line of body is any factory marker
// line ("<!-- oxf:" ... "-->"). A comment that does is never a person's
// answer to a question; only the exact answer marker makes it one.
func EndsWithMarker(body string) bool {
	line := lastLine(body)
	return strings.HasPrefix(line, "<!-- oxf:") && strings.HasSuffix(line, "-->")
}
