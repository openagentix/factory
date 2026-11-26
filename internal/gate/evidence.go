package gate

import (
	"errors"
	"fmt"

	"github.com/openagentix/factory/internal/api"
)

// FillEvidence writes a decision into the evidence bundle: the review, the
// risk, the gate result and the single-opinion mark.
//
// The bundle records the verdict the gate acted on, so a review that said
// agrees and held a blocking point is recorded as concerns. A review that
// counted as no review is left out. The gate result is passed for a pass,
// failed for a rework, and exception for an exception. After a person's yes
// it is passed.
func FillEvidence(ev *api.Evidence, d Decision, r *BoundReview) error {
	if ev == nil {
		return errors.New("gate: no evidence bundle to fill")
	}
	var gate string
	switch d.Outcome {
	case OutcomePass:
		gate = api.GatePassed
	case OutcomeRework:
		gate = api.GateFailed
	case OutcomeException:
		gate = api.GateException
	default:
		return fmt.Errorf("gate: the decision has no valid outcome (%q)", string(d.Outcome))
	}
	ev.Review = nil
	if r.Usable() {
		ev.Review = &api.ReviewSummary{
			Run: r.RunID(), Harness: r.Harness(), Verdict: r.Assessment().Verdict, SingleOpinion: d.SingleOpinion,
		}
	}
	ev.Risk = api.Risk{Level: d.Risk.Level, Reasons: append([]string{}, d.Risk.Reasons...)}
	ev.Gate = gate
	return nil
}
