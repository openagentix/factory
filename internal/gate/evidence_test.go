package gate

import (
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func TestFillEvidence(t *testing.T) {
	tests := []struct {
		name       string
		edit       func(t testing.TB, f *Facts)
		decide     func(Decision) Decision
		gate       string
		hasReview  bool
		verdict    string
		single     bool
		riskLevel  string
		reviewHarn string
	}{
		{"pass", func(testing.TB, *Facts) {}, nil, api.GatePassed, true, "agrees", false, api.RiskLow, api.HarnessCodex},
		{"rework", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("disagrees", pt("major")), api.HarnessCodex)
		}, nil, api.GateFailed, true, "disagrees", false, api.RiskLow, api.HarnessCodex},
		{"agrees with a blocking point is recorded as concerns", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("agrees", pt("blocking")), api.HarnessCodex)
		}, nil, api.GateFailed, true, "concerns", false, api.RiskLow, api.HarnessCodex},
		{"single opinion", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("agrees"), api.HarnessClaudeCode)
		}, nil, api.GatePassed, true, "agrees", true, api.RiskLow, api.HarnessClaudeCode},
		{"high risk question", func(_ testing.TB, f *Facts) { f.Risk.Level = api.RiskHigh }, nil, api.GateException, true, "agrees", false, api.RiskHigh, api.HarnessCodex},
		{"high risk, person said yes", func(_ testing.TB, f *Facts) { f.Risk.Level = api.RiskHigh },
			func(d Decision) Decision {
				n, err := ApplyPersonDecision(d, AnswerYes)
				if err != nil {
					t.Fatal(err)
				}
				return n
			}, api.GatePassed, true, "agrees", false, api.RiskHigh, api.HarnessCodex},
		{"no review", func(_ testing.TB, f *Facts) { f.Review = nil }, nil, api.GateFailed, false, "", false, api.RiskLow, ""},
		{"unusable review is recorded as no review", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("concerns"), api.HarnessCodex)
		}, nil, api.GateFailed, false, "", false, api.RiskLow, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := okFacts(t)
			tc.edit(t, &f)
			d := Decide(f)
			if tc.decide != nil {
				d = tc.decide(d)
			}
			ev := &api.Evidence{Review: &api.ReviewSummary{Verdict: "agrees", Harness: "stale"}, Risk: api.Risk{Level: "low"}, Gate: api.GatePending}
			if err := FillEvidence(ev, d, f.Review); err != nil {
				t.Fatal(err)
			}
			if ev.Gate != tc.gate {
				t.Fatalf("gate = %q, want %q", ev.Gate, tc.gate)
			}
			if tc.hasReview != (ev.Review != nil) {
				t.Fatalf("review = %+v", ev.Review)
			}
			if ev.Review != nil {
				if ev.Review.Verdict != tc.verdict || ev.Review.SingleOpinion != tc.single || ev.Review.Harness != tc.reviewHarn || ev.Review.Run != "r_review" {
					t.Fatalf("review = %+v", ev.Review)
				}
			}
			if ev.Risk.Level != tc.riskLevel || ev.Risk.Reasons == nil {
				t.Fatalf("risk = %+v", ev.Risk)
			}
		})
	}
}

func TestFillEvidenceRefusesNonsense(t *testing.T) {
	if err := FillEvidence(nil, Decision{Outcome: OutcomePass}, nil); err == nil {
		t.Fatal("nil evidence accepted")
	}
	ev := &api.Evidence{Gate: api.GatePending}
	if err := FillEvidence(ev, Decision{}, nil); err == nil || ev.Gate != api.GatePending {
		t.Fatalf("empty decision accepted, gate = %q", ev.Gate)
	}
}
