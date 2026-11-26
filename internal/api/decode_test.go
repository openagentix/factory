package api

import (
	"strings"
	"testing"
)

// encoding/json matches keys without regard to case. These files must not.
func TestJSONKeysAreCaseSensitive(t *testing.T) {
	tests := []struct {
		name  string
		parse func([]byte) error
		input string
		want  string
	}{
		{"result top level", func(b []byte) error { _, err := ParseResult(b); return err },
			`{"schemaVersion":1,"Status":"failed","summary":"x"}`, `unknown field "Status"`},
		{"result all caps", func(b []byte) error { _, err := ParseResult(b); return err },
			`{"SCHEMAVERSION":1,"status":"failed","summary":"x"}`, `unknown field "SCHEMAVERSION"`},
		{"result nested in a slice", func(b []byte) error { _, err := ParseResult(b); return err },
			`{"schemaVersion":1,"status":"failed","summary":"x","claims":[{"Text":"a"}]}`, `unknown field "Text"`},
		{"result nested in a struct", func(b []byte) error { _, err := ParseResult(b); return err },
			`{"schemaVersion":1,"status":"failed","summary":"x","usage":{"Billing":"plan"}}`, `unknown field "Billing"`},
		{"issues", func(b []byte) error { _, err := ParseIssues(b); return err },
			`{"schemaVersion":1,"Issues":[{"title":"t"}]}`, `unknown field "Issues"`},
		{"issues nested", func(b []byte) error { _, err := ParseIssues(b); return err },
			`{"schemaVersion":1,"issues":[{"Title":"t"}]}`, `unknown field "Title"`},
		{"review", func(b []byte) error { _, err := ParseReview(b); return err },
			`{"schemaVersion":1,"subject":"spec","Verdict":"agrees"}`, `unknown field "Verdict"`},
		{"review nested", func(b []byte) error { _, err := ParseReview(b); return err },
			`{"schemaVersion":1,"subject":"spec","verdict":"concerns","points":[{"Severity":"minor","text":"t"}]}`, `unknown field "Severity"`},
		{"manifest", func(b []byte) error { _, err := ParseManifest(b); return err },
			`{"schemaVersion":1,"Goal":40,"criteria":[{"id":1,"text":"t","run":"x"}]}`, `unknown field "Goal"`},
		{"manifest nested", func(b []byte) error { _, err := ParseManifest(b); return err },
			`{"schemaVersion":1,"goal":40,"criteria":[{"ID":1,"text":"t","run":"x"}]}`, `unknown field "ID"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.parse([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestJSONExactKeysStillParse(t *testing.T) {
	// A null object and an unknown-typed value are left to the normal decoder.
	if _, err := ParseResult([]byte(`{"schemaVersion":1,"status":"failed","summary":"x","claims":null,"usage":null}`)); err != nil {
		t.Errorf("null values: %v", err)
	}
	if _, err := ParseResult([]byte(`{"schemaVersion":1,"status":"failed","summary":"x","usage":5}`)); err == nil {
		t.Error("a number for an object must fail")
	}
}
