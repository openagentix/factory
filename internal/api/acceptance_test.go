package api

import (
	"strings"
	"testing"
)

const validManifestJSON = `{ "schemaVersion": 1, "goal": 40, "criteria": [
  { "id": 1, "text": "A customer can move a booking to another day", "run": "go test ./acceptance/40 -run TestMoveBooking" },
  { "id": 2, "text": "Works on a phone", "run": "", "reason": "No phone browser is available to the checks" } ] }`

func TestParseManifest_Valid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifestJSON))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Goal != 40 || len(m.Criteria) != 2 {
		t.Fatalf("unexpected: %+v", m)
	}
	if !m.Criteria[0].HasCheck() || m.Criteria[1].HasCheck() {
		t.Error("HasCheck wrong")
	}
	if m.Criteria[1].Reason == "" {
		t.Error("reason lost")
	}
}

func TestParseManifest_Invalid(t *testing.T) {
	const head = `{"schemaVersion":1,"goal":40,"criteria":[`
	tests := []struct{ name, input, wantErr string }{
		{"empty", "", "manifest"},
		{"unknown field", head + `],"x":1}`, "x"},
		{"wrong version", `{"schemaVersion":2,"goal":40,"criteria":[{"id":1,"text":"t","run":"x"}]}`, "schemaVersion"},
		{"no goal", `{"schemaVersion":1,"criteria":[{"id":1,"text":"t","run":"x"}]}`, "goal"},
		{"no criteria", head + `]}`, "criteria: must hold"},
		{"bad id", head + `{"id":0,"text":"t","run":"x"}]}`, "criteria[0].id"},
		{"duplicate id", head + `{"id":1,"text":"t","run":"x"},{"id":1,"text":"u","run":"y"}]}`, "criteria[1].id"},
		{"no text", head + `{"id":1,"run":"x"}]}`, "criteria[0].text"},
		{"no run and no reason", head + `{"id":1,"text":"t","run":""}]}`, "criteria[0].reason"},
		{"run and reason", head + `{"id":1,"text":"t","run":"x","reason":"r"}]}`, "criteria[0].reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestComputeState(t *testing.T) {
	withCheck := ManifestCriterion{ID: 1, Text: "t", Run: "go test ./..."}
	noCheck := ManifestCriterion{ID: 2, Text: "t", Reason: "no phone browser"}
	tests := []struct {
		name    string
		entry   ManifestCriterion
		outcome CheckOutcome
		want    AcceptanceState
	}{
		{"check passed", withCheck, CheckPassed, StateWorking},
		{"check failed", withCheck, CheckFailed, StateNotWorking},
		{"check not run", withCheck, CheckNotRun, StateCouldNotConfirm},
		{"unknown outcome", withCheck, CheckOutcome("maybe"), StateCouldNotConfirm},
		{"no check, a pass is reported", noCheck, CheckPassed, StateCouldNotConfirm},
		{"no check, a fail is reported", noCheck, CheckFailed, StateCouldNotConfirm},
		{"no check, not run", noCheck, CheckNotRun, StateCouldNotConfirm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeState(tt.entry, tt.outcome); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAcceptanceStateValues(t *testing.T) {
	tests := []struct {
		s     AcceptanceState
		value string
		name  string
		valid bool
	}{
		{StateWorking, "working", "working", true},
		{StateNotWorking, "not_working", "not working yet", true},
		{StateCouldNotConfirm, "could_not_confirm", "could not confirm", true},
		{AcceptanceState("done"), "done", "done", false},
	}
	for _, tt := range tests {
		if string(tt.s) != tt.value || tt.s.Name() != tt.name || tt.s.Valid() != tt.valid {
			t.Errorf("%q: name %q valid %v", tt.s, tt.s.Name(), tt.s.Valid())
		}
	}
}
