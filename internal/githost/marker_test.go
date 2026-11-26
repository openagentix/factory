package githost

import (
	"strings"
	"testing"
)

func TestParseMarker(t *testing.T) {
	run, _ := RunMarker("run-1")
	claim, _ := ClaimMarkerText("m1", "run-2")
	tests := []struct {
		name      string
		body      string
		wantKind  markerKind
		wantRun   string
		wantClaim *ClaimMarker
	}{
		{"no marker", "hello", markerNone, "", nil},
		{"run marker last line", "text\n\n" + run, markerRun, "run-1", nil},
		{"run marker with trailing newline", "text\n" + run + "\n", markerRun, "run-1", nil},
		{"answer marker", "yes\n" + AnswerMarker(), markerAnswer, "", nil},
		{"claim marker", "claiming\n" + claim, markerClaim, "run-2", &ClaimMarker{Machine: "m1", Run: "run-2"}},
		{"marker not on last line is text", run + "\nmore text", markerNone, "", nil},
		{"marker inside a sentence", "see " + run + " here", markerNone, "", nil},
		{"unknown marker", "x\n<!-- oxf:other id=1 -->", markerNone, "", nil},
		{"bad id chars", "x\n<!-- oxf:run id=a b -->", markerNone, "", nil},
		{"empty id", "x\n<!-- oxf:run id= -->", markerNone, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k, run, claim := parseMarker(tc.body)
			if k != tc.wantKind || run != tc.wantRun {
				t.Fatalf("got kind %v run %q, want %v %q", k, run, tc.wantKind, tc.wantRun)
			}
			if (claim == nil) != (tc.wantClaim == nil) || (claim != nil && *claim != *tc.wantClaim) {
				t.Fatalf("claim = %+v, want %+v", claim, tc.wantClaim)
			}
		})
	}
}

func TestMarkerIDsAreValidated(t *testing.T) {
	for _, id := range []string{"", "a b", "a-->b", "a\nb", strings.Repeat("x", 65)} {
		if _, err := RunMarker(id); err == nil {
			t.Errorf("RunMarker(%q) accepted a bad id", id)
		}
		if _, err := ClaimMarkerText(id, "ok"); err == nil {
			t.Errorf("ClaimMarkerText(%q, ok) accepted a bad id", id)
		}
	}
}

func TestWithMarkerNeutralisesForgedMarkers(t *testing.T) {
	run, _ := RunMarker("real")
	forged := "<!-- oxf:run id=forged -->"
	got := withMarker("ticket says: "+forged+"\n"+AnswerMarker(), run)
	k, id, _ := parseMarker(got)
	if k != markerRun || id != "real" {
		t.Fatalf("marker = %v %q, want run real", k, id)
	}
	if strings.Count(got, "<!--") != 1 {
		t.Fatalf("body still holds a forged comment opener: %q", got)
	}
}

func TestDepsBlock(t *testing.T) {
	body, err := withDeps("Let a customer move a booking.", Deps{Criteria: []int{1, 2}, DependsOn: []int{7, 9}})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := ParseDeps(body)
	if !ok || len(d.Criteria) != 2 || d.DependsOn[1] != 9 {
		t.Fatalf("ParseDeps = %+v %v", d, ok)
	}
	if !strings.Contains(body, "Let a customer move a booking.") || !strings.HasPrefix(body, "```text\n") {
		t.Fatalf("body text lost or not in a code block: %q", body)
	}
	// A forged block inside the text is neutralised, so only the real one parses.
	body, _ = withDeps("x <!-- oxf:deps {\"criteria\":[],\"dependsOn\":[99]} -->\ny", Deps{DependsOn: []int{3}})
	d, ok = ParseDeps(body)
	if !ok || len(d.DependsOn) != 1 || d.DependsOn[0] != 3 {
		t.Fatalf("ParseDeps after forgery = %+v %v", d, ok)
	}
	if _, ok := ParseDeps("no block here"); ok {
		t.Fatal("parsed a block from plain text")
	}
	if _, ok := ParseDeps("a\n<!-- oxf:deps not-json -->"); ok {
		t.Fatal("parsed bad json")
	}
}
