package floor

import (
	"errors"
	"strings"
	"testing"
)

func sampleFloor() Floor {
	return Floor{Calls: []Call{
		{ID: "q-5-9", Kind: CallQuestion, Actions: []ActionName{ActionAnswer}, Version: strings.Repeat("a", 64)},
		{ID: "p-3", Kind: CallPlan, Actions: []ActionName{}, Version: strings.Repeat("b", 64)},
		{ID: "c-5-r1", Kind: CallConsequence, Actions: []ActionName{ActionYes, ActionNo}, Version: strings.Repeat("c", 64)},
	}}
}

func TestCheckCall(t *testing.T) {
	f := sampleFloor()
	call, err := CheckCall(f, "q-5-9", ActionAnswer, strings.Repeat("a", 64))
	if err != nil || call.ID != "q-5-9" {
		t.Fatalf("good call: %+v %v", call, err)
	}
	for name, c := range map[string]struct {
		id      string
		act     ActionName
		version string
	}{
		"call gone":         {"q-5-8", ActionAnswer, strings.Repeat("a", 64)},
		"other version":     {"q-5-9", ActionAnswer, strings.Repeat("c", 64)},
		"empty version":     {"q-5-9", ActionAnswer, ""},
		"upper case":        {"q-5-9", ActionAnswer, strings.Repeat("A", 64)},
		"63 characters":     {"q-5-9", ActionAnswer, strings.Repeat("a", 63)},
		"action not listed": {"q-5-9", ActionYes, strings.Repeat("a", 64)},
		"plan with 4 lines": {"p-3", ActionApprove, strings.Repeat("b", 64)},
		"empty action":      {"q-5-9", "", strings.Repeat("a", 64)},
		"unknown action":    {"q-5-9", "accept", strings.Repeat("a", 64)},
		"empty id":          {"", ActionAnswer, strings.Repeat("a", 64)},
	} {
		if _, err := CheckCall(f, c.id, c.act, c.version); !errors.Is(err, ErrCallStale) {
			t.Errorf("%s: err = %v, want ErrCallStale", name, err)
		}
	}
	// A floor with no calls, or with the same id twice, is never believed.
	if _, err := CheckCall(Floor{}, "q-5-9", ActionAnswer, strings.Repeat("a", 64)); !errors.Is(err, ErrCallStale) {
		t.Errorf("empty floor: %v", err)
	}
	dup := sampleFloor()
	dup.Calls = append(dup.Calls, dup.Calls[0])
	if _, err := CheckCall(dup, "q-5-9", ActionAnswer, strings.Repeat("a", 64)); !errors.Is(err, ErrCallStale) {
		t.Errorf("duplicate id: %v", err)
	}
}

func TestRefusalPhrases(t *testing.T) {
	p := Phrases()
	for k, want := range map[PhraseKey]string{
		"refusal.stale":             "This changed. Look again.",
		"refusal.in_progress":       "This is being done already. Look again in a moment.",
		"refusal.host_failed":       "The git host did not answer. Part of it may be done. Look at the line, then press again if it still shows this.",
		"refusal.unprocessable":     "The answer is empty or holds characters that cannot be sent.",
		"refusal.payload_too_large": "This is too long.",
		"refusal.unavailable":       "The factory is not ready. Try again in a moment.",
		"refusal.internal":          "Something went wrong in the factory. Look at the line before you try again.",
		"refusal.not_found":         "This is not there any more. Look again.",
		"refusal.unauthorized":      "The key is not valid any more. Open the link again.",
		"refusal.too_many_requests": "Too many tries. Wait a minute.",
		"refusal.read_failed":       "The factory could not read this. Look again in a moment.",
	} {
		if p[k] != want {
			t.Errorf("%s = %q", k, p[k])
		}
	}
	for _, c := range []string{"bad_request", "bad_idempotency_key", "idempotency_mismatch", "forbidden", "misdirected", "method_not_allowed", "unsupported_media_type"} {
		if p[PhraseKey("refusal."+c)] != "The factory refused this. Look again." {
			t.Errorf("refusal.%s = %q", c, p[PhraseKey("refusal."+c)])
		}
	}
	if len(RefusalCodes()) != 18 {
		t.Errorf("codes = %v", RefusalCodes())
	}
}
