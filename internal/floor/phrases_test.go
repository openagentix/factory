package floor

import (
	"regexp"
	"strings"
	"testing"
)

// The plain-screen words that record 0004 forbids.
var forbiddenWords = regexp.MustCompile(`(?i)\b(pull requests?|merg\w*|diffs?|review\w*|branch\w*|commit\w*|staging|gates?|harness\w*|run ids?|tokens?|evidence)\b`)

func TestEveryCodeKindAndWordHasAPhrase(t *testing.T) {
	p := Phrases()
	need := []PhraseKey{}
	for _, c := range AllReasonCodes() {
		need = append(need, PhraseKey("reason."+string(c)))
	}
	for _, k := range AllJournalKinds() {
		need = append(need, PhraseKey("journal."+string(k)))
	}
	for _, w := range AllLampWords() {
		need = append(need, PhraseKey("lamp."+string(w)))
	}
	for _, k := range AllCallKinds() {
		if k == CallConsequence {
			for _, r := range AllRiskReasons() {
				need = append(need, PhraseKey("call.consequence."+string(r)))
			}
			continue
		}
		need = append(need, PhraseKey("call."+string(k)))
	}
	need = append(need, "honest.not_built", "honest.read_failed", "honest.not_enough_yet")
	for _, c := range RefusalCodes() {
		need = append(need, PhraseKey("refusal."+c))
	}
	for _, k := range need {
		if s, ok := p[k]; !ok || strings.TrimSpace(s) == "" {
			t.Errorf("no phrase for %s", k)
		}
	}
	// No phrase outside the families above.
	if len(p) != len(need)+extraPhrases {
		t.Errorf("table has %d phrases, the families need %d plus %d extras", len(p), len(need), extraPhrases)
	}
}

// extraPhrases are the signal sentence parts: moving and elsewhere.
const extraPhrases = 2 + 4 // the signal parts and the four one forms

func TestPhrasesCarryOnlyNumericAndCodeSlots(t *testing.T) {
	slot := regexp.MustCompile(`\{([^}]*)\}`)
	allowed := map[string]bool{}
	for _, s := range PhraseSlots() {
		allowed[s] = true
	}
	if len(allowed) == 0 {
		t.Fatal("no slots declared")
	}
	for _, free := range []string{"title", "text", "question", "note", "ruling", "body", "name", "comment"} {
		if allowed[free] {
			t.Errorf("slot %q could carry free text", free)
		}
	}
	for k, s := range Phrases() {
		for _, m := range slot.FindAllStringSubmatch(s, -1) {
			if !allowed[m[1]] {
				t.Errorf("%s: slot {%s} is not allowed", k, m[1])
			}
		}
		// Braces must close; nothing else odd may sit in a phrase.
		if strings.Count(s, "{") != strings.Count(s, "}") {
			t.Errorf("%s: unbalanced braces: %q", k, s)
		}
		if strings.ContainsAny(s, "<>&\n\r\t\x00") || strings.Contains(s, "\xe2\x80\x94") {
			t.Errorf("%s: markup, control or dash character: %q", k, s)
		}
	}
}

func TestNoPhraseUsesAForbiddenPlainScreenWord(t *testing.T) {
	for k, s := range Phrases() {
		if m := forbiddenWords.FindString(s); m != "" {
			t.Errorf("%s: forbidden word %q in %q", k, m, s)
		}
	}
	// The pattern itself works.
	for _, bad := range []string{"merge it", "a pull request", "the gate", "your token", "run id", "staging area", "reviewer"} {
		if !forbiddenWords.MatchString(bad) {
			t.Errorf("pattern misses %q", bad)
		}
	}
	for _, ok := range []string{"different", "second opinion", "holding area", "tries"} {
		if forbiddenWords.MatchString(ok) {
			t.Errorf("pattern flags %q", ok)
		}
	}
}

func TestPhrasesTableIsACopy(t *testing.T) {
	a := Phrases()
	a["reason.queued"] = "changed"
	if Phrases()["reason.queued"] == "changed" {
		t.Fatal("a caller changed the shared table")
	}
}

func TestTheRecordSentencesAreWordForWord(t *testing.T) {
	p := Phrases()
	for k, want := range map[PhraseKey]string{
		"call.statement":                "The project's own checks are failing. A developer is needed.",
		"reason.paused":                 "The line is paused on this machine.",
		"reason.project_checks_failing": "The project's own checks are failing. A developer is needed.",
		"lamp.could_not_read":           "Could not read",
		"lamp.stuck":                    "Stuck",
		"lamp.needs_a_person":           "Needs a person",
		"lamp.running":                  "Running",
		"lamp.paused":                   "Paused",
		"lamp.idle":                     "Idle",
		"honest.not_built":              "Not built yet",
		"honest.read_failed":            "Could not read",
		"honest.not_enough_yet":         "Not enough yet",
		"call.consequence.contract":     "This changes how other programs talk to yours. Is that what you want?",
		"call.consequence.security":     "This changes who may do what in your product. Is that what you want?",
		"call.consequence.dependency":   "This changes a part your product is built from. Is that what you want?",
		"call.consequence.test_changed": "This changes or removes a check that already existed. Is that what you want?",
	} {
		if p[k] != want {
			t.Errorf("%s: got %q, want %q", k, p[k], want)
		}
	}
	if !strings.Contains(p["reason.unknown"], "could not say why") {
		t.Errorf("unknown reason phrase: %q", p["reason.unknown"])
	}
}

func TestOneFormsAreChosenByNumber(t *testing.T) {
	for _, k := range []PhraseKey{"reason.attempts_used_up", "reason.too_many_cut_offs", "reason.too_many_claim_lost", "call.accept"} {
		plain, _ := PhraseFor(k, 2)
		zero, _ := PhraseFor(k, 0)
		one, ok := PhraseFor(k, 1)
		if !ok || one == plain || zero != plain || strings.Contains(one, "{") && k != "reason.attempts_used_up" && k != "call.accept" && !strings.Contains(one, "{piece}") {
			t.Errorf("%s: zero %q one %q two %q", k, zero, one, plain)
		}
		if m := forbiddenWords.FindString(one); m != "" {
			t.Errorf("%s: forbidden word in the one form", k)
		}
	}
	if s, _ := PhraseFor("call.accept", 1); s != "1 change is in the holding area. Accept it, or ask a developer to finish this step." {
		t.Errorf("%q", s)
	}
	if s, _ := PhraseFor("reason.attempts_used_up", 1); s != "{piece}: the 1 try is used." {
		t.Errorf("%q", s)
	}
	// A key with no one form uses the plain phrase for any number.
	a, _ := PhraseFor("reason.queued", 1)
	b, _ := PhraseFor("reason.queued", 2)
	if a != b || a == "" {
		t.Error("queued")
	}
}
