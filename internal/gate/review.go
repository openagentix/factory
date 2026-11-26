package gate

import (
	"fmt"

	"github.com/openagentix/factory/internal/api"
)

// Limits on a review file. The parser in internal/api has none, so the gate
// sets them. A review file is written by an agent that read untrusted code.
const (
	// MaxReviewBytes is the largest review.json the gate parses.
	MaxReviewBytes int64 = 256 << 10
	// MaxReviewPoints is the most points a review may hold.
	MaxReviewPoints = 100
)

// ParseReview reads review.json bytes with limits, then with the strict
// parser of internal/api. The caller reads the file. This function does not.
func ParseReview(data []byte) (*api.ReviewFile, error) {
	if int64(len(data)) > MaxReviewBytes {
		return nil, fmt.Errorf("review.json is too large: %d bytes, the limit is %d", len(data), MaxReviewBytes)
	}
	r, err := api.ParseReview(data)
	if err != nil {
		return nil, err
	}
	if len(r.Points) > MaxReviewPoints {
		return nil, fmt.Errorf("review.json holds %d points, the limit is %d", len(r.Points), MaxReviewPoints)
	}
	return r, nil
}

// Assessment is what the gate makes of a review, after its sanity rules.
type Assessment struct {
	// Usable is false for a review that is invalid. It then counts as no
	// review.
	Usable bool
	// Agrees is true only for a usable review with the verdict agrees and no
	// point of the highest severity.
	Agrees bool
	// Verdict is the verdict the gate acts on. It is "concerns" when the
	// review said agrees and also held a blocking point. Empty when the
	// review is not usable.
	Verdict string
	// Reasons say why the review is unusable or does not agree.
	Reasons []string
}

// overLimits reports whether a review is over the limits of ParseReview, so
// that a review built another way cannot skip them.
func overLimits(r *api.ReviewFile) bool {
	if len(r.Points) > MaxReviewPoints {
		return true
	}
	var n int64
	for _, p := range r.Points {
		n += int64(len(p.Text)) + int64(len(p.File))
		if n > MaxReviewBytes {
			return true
		}
	}
	return false
}

// Assess applies the sanity rules to a review. They run before Decide uses
// it. A review for any subject but a change, an unknown verdict or severity,
// and a review that does not agree but gives no point are all unusable: a
// rework needs reasons.
func Assess(r *api.ReviewFile) Assessment { return AssessFor(api.TaskImplement, r) }

// SubjectFor is the subject the record names for the review of a task's
// output: acceptance for the acceptance task, change for every other task the
// gate judges. A review about any other subject is refused for that task.
func SubjectFor(task string) string {
	if task == api.TaskAcceptance {
		return api.SubjectAcceptance
	}
	return api.SubjectChange
}

// AssessFor is Assess for the review of one task's output. It accepts only
// the subject of SubjectFor(task).
func AssessFor(task string, r *api.ReviewFile) Assessment {
	return assess(r, SubjectFor(task))
}

// assess applies the sanity rules. An empty subject accepts any of the four
// valid subjects.
func assess(r *api.ReviewFile, subject string) Assessment {
	if r == nil {
		return Assessment{Reasons: []string{"there is no review"}}
	}
	bad := func(why string) Assessment { return Assessment{Reasons: []string{why}} }
	if r.SchemaVersion != api.SchemaVersion {
		return bad("the review file has the wrong schema version")
	}
	switch {
	case subject == "" && (r.Subject == api.SubjectSpec || r.Subject == api.SubjectPlan || r.Subject == api.SubjectAcceptance || r.Subject == api.SubjectChange):
	case r.Subject != subject:
		return bad(fmt.Sprintf("the review is about %q, and a review about %q is needed here", clean(r.Subject, 20), subject))
	}
	switch r.Verdict {
	case api.VerdictAgrees, api.VerdictConcerns, api.VerdictDisagrees:
	default:
		return bad("the review has no valid verdict")
	}
	if overLimits(r) {
		return bad("the review is over the limits of size and number of points")
	}
	blocking := false
	for _, p := range r.Points {
		switch p.Severity {
		case api.SeverityMinor, api.SeverityMajor:
		case api.SeverityBlocking:
			blocking = true
		default:
			return bad("the review has a point with no valid severity")
		}
	}
	if r.Verdict != api.VerdictAgrees && len(r.Points) == 0 {
		return bad("the review does not agree and gives no points, so it gives no reasons for a rework")
	}
	if r.Verdict == api.VerdictAgrees && blocking {
		return Assessment{
			Usable:  true,
			Verdict: api.VerdictConcerns,
			Reasons: []string{"the review says agrees but holds a blocking point, so it is treated as not agreeing"},
		}
	}
	if r.Verdict != api.VerdictAgrees {
		return Assessment{Usable: true, Verdict: r.Verdict, Reasons: []string{"the review verdict is " + r.Verdict}}
	}
	return Assessment{Usable: true, Agrees: true, Verdict: api.VerdictAgrees}
}
