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
const extraPhrases = 2

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
