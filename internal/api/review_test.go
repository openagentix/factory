package api

import (
	"strings"
	"testing"
)

const validReviewJSON = `{ "schemaVersion": 1, "subject": "change", "verdict": "agrees",
  "points": [ { "severity": "minor", "text": "The error message is vague", "file": "internal/booking/move.go" } ],
  "criteria": [ { "id": 1, "opinion": "met", "note": "" } ] }`

func TestParseReview_Valid(t *testing.T) {
	r, err := ParseReview([]byte(validReviewJSON))
	if err != nil {
		t.Fatalf("ParseReview: %v", err)
	}
	if r.Subject != SubjectChange || r.Verdict != VerdictAgrees || r.Points[0].Severity != SeverityMinor ||
		r.Points[0].File != "internal/booking/move.go" || r.Criteria[0].Opinion != OpinionMet {
		t.Errorf("unexpected: %+v", r)
	}
}

func TestParseReview_AllValues(t *testing.T) {
	for _, s := range []string{"spec", "plan", "acceptance", "change"} {
		for _, v := range []string{"agrees", "concerns", "disagrees"} {
			in := `{"schemaVersion":1,"subject":"` + s + `","verdict":"` + v + `"}`
			if _, err := ParseReview([]byte(in)); err != nil {
				t.Errorf("%s/%s: %v", s, v, err)
			}
		}
	}
	for _, sev := range []string{"minor", "major", "blocking"} {
		in := `{"schemaVersion":1,"subject":"spec","verdict":"concerns","points":[{"severity":"` + sev + `","text":"t"}]}`
		if _, err := ParseReview([]byte(in)); err != nil {
			t.Errorf("severity %s: %v", sev, err)
		}
	}
}

func TestParseReview_Invalid(t *testing.T) {
	const head = `{"schemaVersion":1,"subject":"spec","verdict":"concerns"`
	tests := []struct{ name, input, wantErr string }{
		{"empty", "", "review"},
		{"unknown field", head + `,"x":1}`, "x"},
		{"wrong version", `{"schemaVersion":2,"subject":"spec","verdict":"agrees"}`, "schemaVersion"},
		{"bad subject", `{"schemaVersion":1,"subject":"code","verdict":"agrees"}`, "subject"},
		{"bad verdict", `{"schemaVersion":1,"subject":"spec","verdict":"yes"}`, "verdict"},
		{"bad severity", head + `,"points":[{"severity":"huge","text":"t"}]}`, "points[0].severity"},
		{"point no text", head + `,"points":[{"severity":"minor"}]}`, "points[0].text"},
		{"bad opinion", head + `,"criteria":[{"id":1,"opinion":"fine"}]}`, "criteria[0].opinion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseReview([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
