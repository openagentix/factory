package floor

import (
	"encoding/json"
	"testing"
)

// The zero value of every enum is invalid.
func TestZeroValueOfEveryEnumIsInvalid(t *testing.T) {
	zero := map[string]bool{
		"Lamp": Lamp("").Valid(), "LampWord": LampWord("").Valid(), "Mode": Mode("").Valid(),
		"SourceKey": SourceKey("").Valid(), "SourceState": SourceState("").Valid(), "StationNo": StationNo(0).Valid(),
		"StationKey": StationKey("").Valid(), "Who": Who("").Valid(), "StationSource": StationSource("").Valid(),
		"BayState": BayState("").Valid(), "GaugeKey": GaugeKey("").Valid(), "GaugeState": GaugeState("").Valid(),
		"GaugeWindow": GaugeWindow("").Valid(), "PieceKind": PieceKind("").Valid(), "PieceState": PieceState("").Valid(),
		"Lane": Lane("").Valid(), "Course": Course("").Valid(), "LineState": LineState("").Valid(),
		"CallKind": CallKind("").Valid(), "CallWho": CallWho("").Valid(), "ActionName": ActionName("").Valid(),
		"JournalKind": JournalKind("").Valid(), "PendingMode": PendingMode("").Valid(), "RiskReason": RiskReason("").Valid(),
		"GateStatus": GateStatus("").Valid(), "ReasonCode": ReasonCode("").Valid(),
	}
	for name, ok := range zero {
		if ok {
			t.Errorf("%s: the zero value is valid", name)
		}
	}
	if StationNo(8).Valid() || StationNo(-1).Valid() {
		t.Error("station 8 or -1 is valid")
	}
}

// Record 0004, "Piece states": each reason code maps to exactly one state.
// This table is copied from the record. A code added to AllReasonCodes without
// a row here fails the test, and so does a row without a code.
func TestEveryReasonCodeMapsToItsOneState(t *testing.T) {
	want := map[ReasonCode]PieceState{}
	for _, c := range []ReasonCode{"check_failed", "protected_path", "acceptance_regressed", "review_disagrees", "person_said_no",
		"no_change", "bad_result", "deadline", "pull_request_closed", "no_process", "no_review", "invalid_facts",
		"setup_not_finished", "acceptance_unconfirmed", "attempts_used_up", "taken_out", "untrusted_trigger",
		"too_many_cut_offs", "too_many_claim_lost", "read_failed", "unknown"} {
		want[c] = StateStuck
	}
	for _, c := range []ReasonCode{"needs_human", "question_open", "consequence_open", "plan_waits", "accept_pending",
		"project_checks_failing", "level_off", "level_guided", "level_unset", "goal_not_approved", "paused"} {
		want[c] = StateWaitingPerson
	}
	for _, c := range []ReasonCode{"queued", "cap", "throttle", "key_busy", "staging_frozen", "machine_busy",
		"waiting_dependency", "waiting_checks", "waiting_combine", "cut_off", "claim_lost", "claim_expired", "planned", "waiting_review", "not_built"} {
		want[c] = StateWaitingMachine
	}
	all := AllReasonCodes()
	if len(all) != len(want) {
		t.Fatalf("AllReasonCodes has %d codes, the record table has %d", len(all), len(want))
	}
	seen := map[ReasonCode]bool{}
	for _, c := range all {
		if seen[c] {
			t.Errorf("code %q listed twice", c)
		}
		seen[c] = true
		w, ok := want[c]
		if !ok {
			t.Errorf("code %q has no row in the record table", c)
			continue
		}
		if got := c.State(); got != w {
			t.Errorf("code %q: state %q, want %q", c, got, w)
		}
		if !c.Valid() {
			t.Errorf("code %q is not valid", c)
		}
	}
	for _, bad := range []ReasonCode{"", "ready", "mystery", "Queued"} {
		if bad.Valid() || bad.State() != "" {
			t.Errorf("code %q must be invalid with no state", bad)
		}
	}
}

func TestStationKeys(t *testing.T) {
	want := []StationKey{"plan", "write_checks", "build", "check", "second_opinion", "holding_area", "accepted"}
	for i, k := range want {
		if got := StationNo(i + 1).Key(); got != k {
			t.Errorf("station %d key %q, want %q", i+1, got, k)
		}
		if !k.Valid() {
			t.Errorf("key %q invalid", k)
		}
	}
	if StationNo(0).Key() != "" || StationNo(9).Key() != "" {
		t.Error("an invalid station has a key")
	}
}

func TestFactsAreValidatedAndEncodeAsNumberOrString(t *testing.T) {
	good := []struct {
		f    func() (Fact, bool)
		json string
	}{
		{func() (Fact, bool) { return NumFact(57) }, `57`},
		{func() (Fact, bool) { return NumFact(0) }, `0`},
		{func() (Fact, bool) { return CodeFact(CheckFailed) }, `"check_failed"`},
		{func() (Fact, bool) { return MachineFact("alice-laptop_1.x") }, `"alice-laptop_1.x"`},
		{func() (Fact, bool) { return StationFact(StationCheck) }, `"check"`},
		{func() (Fact, bool) { return RiskFact(RiskSecurity) }, `"security"`},
	}
	for _, g := range good {
		f, ok := g.f()
		if !ok || !f.Valid() {
			t.Fatalf("fact for %s refused", g.json)
		}
		b, err := json.Marshal(f)
		if err != nil || string(b) != g.json {
			t.Errorf("got %s err %v, want %s", b, err, g.json)
		}
	}
	bad := []func() (Fact, bool){
		func() (Fact, bool) { return NumFact(-1) },
		func() (Fact, bool) { return NumFact(1 << 40) },
		func() (Fact, bool) { return CodeFact("mystery") },
		func() (Fact, bool) { return CodeFact("") },
		func() (Fact, bool) { return MachineFact("") },
		func() (Fact, bool) { return MachineFact("a b") },
		func() (Fact, bool) { return MachineFact("<script>") },
		func() (Fact, bool) { return MachineFact("é") },
		func() (Fact, bool) { return MachineFact("a\nb") },
		func() (Fact, bool) { return StationFact("nope") },
		func() (Fact, bool) { return RiskFact("") },
	}
	for i, b := range bad {
		if f, ok := b(); ok || f.Valid() {
			t.Errorf("bad fact %d accepted", i)
		}
	}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	if ValidMachineID(string(long)) || !ValidMachineID(string(long[:64])) {
		t.Error("machine id length rule: 1 to 64")
	}
	// A zero Fact is invalid and refuses to encode.
	var zero Fact
	if zero.Valid() {
		t.Error("zero Fact is valid")
	}
	if _, err := json.Marshal(zero); err == nil {
		t.Error("zero Fact encoded")
	}
}

// Only waiting_dependency and waiting_checks may show another state than the
// one in the table, and only the state of the piece they wait on.
func TestOnlyTwoCodesTakeABlockersState(t *testing.T) {
	for _, c := range AllReasonCodes() {
		want := c == WaitingDependency || c == WaitingChecks
		if c.TakesBlockerState() != want {
			t.Errorf("%s: TakesBlockerState %v", c, c.TakesBlockerState())
		}
	}
	if WaitingDependency.State() != StateWaitingMachine || WaitingChecks.State() != StateWaitingMachine {
		t.Error("the default state of the two codes is waiting_machine")
	}
	if !ModeUnknown.Valid() || Mode("").Valid() {
		t.Error("mode unknown is valid, a blank mode is not")
	}
}
