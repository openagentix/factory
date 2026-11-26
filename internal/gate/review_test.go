package gate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func rev(verdict string, points ...api.ReviewPoint) *api.ReviewFile {
	return &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectChange, Verdict: verdict, Points: points}
}

func pt(sev string) api.ReviewPoint { return api.ReviewPoint{Severity: sev, Text: "a point"} }

func TestAssess(t *testing.T) {
	tests := []struct {
		name       string
		r          *api.ReviewFile
		usable     bool
		agrees     bool
		wantVerdct string
	}{
		{"nil review is no review", nil, false, false, ""},
		{"plain agree", rev("agrees"), true, true, "agrees"},
		{"agree with minor and major points", rev("agrees", pt("minor"), pt("major")), true, true, "agrees"},
		{"agree with one blocking point is not agreeing", rev("agrees", pt("minor"), pt("blocking")), true, false, "concerns"},
		{"concerns with a point", rev("concerns", pt("major")), true, false, "concerns"},
		{"disagrees with a point", rev("disagrees", pt("blocking")), true, false, "disagrees"},
		{"concerns with no points is invalid", rev("concerns"), false, false, ""},
		{"disagrees with no points is invalid", rev("disagrees"), false, false, ""},
		{"unknown verdict is invalid", rev("approved", pt("minor")), false, false, ""},
		{"unknown severity is invalid", rev("agrees", pt("whatever")), false, false, ""},
		{"empty verdict is invalid", rev("", pt("minor")), false, false, ""},
		{"review of a spec is not a review of a change", &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectSpec, Verdict: "agrees"}, false, false, ""},
		{"wrong schema version is invalid", &api.ReviewFile{SchemaVersion: 2, Subject: api.SubjectChange, Verdict: "agrees"}, false, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Assess(tc.r)
			if a.Usable != tc.usable || a.Agrees != tc.agrees || a.Verdict != tc.wantVerdct {
				t.Fatalf("got %+v, want usable=%v agrees=%v verdict=%q", a, tc.usable, tc.agrees, tc.wantVerdct)
			}
			if !a.Agrees && len(a.Reasons) == 0 {
				t.Fatal("a review that does not agree must say why")
			}
		})
	}
}

func TestParseReviewLimits(t *testing.T) {
	good := `{"schemaVersion":1,"subject":"change","verdict":"agrees"}`
	if _, err := ParseReview([]byte(good)); err != nil {
		t.Fatalf("good review refused: %v", err)
	}
	big := `{"schemaVersion":1,"subject":"change","verdict":"agrees","points":[{"severity":"minor","text":"` +
		strings.Repeat("x", 100<<20) + `"}]}`
	if _, err := ParseReview([]byte(big)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("100 MiB review: err = %v", err)
	}
	if _, err := ParseReview([]byte(manyPoints(10000))); err == nil {
		t.Fatal("a review with 10000 points was accepted")
	}
}

func manyPoints(n int) string {
	var b strings.Builder
	b.WriteString(`{"schemaVersion":1,"subject":"change","verdict":"concerns","points":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"severity":"minor","text":"p"}`)
	}
	b.WriteString("]}")
	return b.String()
}

func TestParseReviewTooManyPoints(t *testing.T) {
	in := manyPoints(MaxReviewPoints + 1)
	if int64(len(in)) > MaxReviewBytes {
		t.Fatalf("test input is over the byte limit (%d), it would not test the point limit", len(in))
	}
	_, err := ParseReview([]byte(in))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(MaxReviewPoints)) {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseReview([]byte(manyPoints(MaxReviewPoints))); err != nil {
		t.Fatalf("exactly the limit refused: %v", err)
	}
}

func TestParseReviewKeepsStrictness(t *testing.T) {
	if _, err := ParseReview([]byte(`{"schemaVersion":1,"subject":"change","verdict":"agrees","commit":"abc"}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
}
