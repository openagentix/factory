package githost

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// The factory marks its own text with one HTML comment on the last line of a
// comment or an issue body. A marker only counts when the author is the
// factory's own user. Text from anyone else is never trusted for markers.
//
//	<!-- oxf:run id=RUN -->
//	<!-- oxf:answer -->
//	<!-- oxf:claim machine=MACHINE run=RUN -->
//	<!-- oxf:deps {"criteria":[1],"dependsOn":[12]} -->

var idRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type markerKind int

const (
	markerNone markerKind = iota
	markerRun
	markerAnswer
	markerClaim
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
