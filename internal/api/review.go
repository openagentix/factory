package api

import "fmt"

// Subjects a review may be about.
const (
	SubjectSpec       = "spec"
	SubjectPlan       = "plan"
	SubjectAcceptance = "acceptance"
	SubjectChange     = "change"
)

// Verdicts of a review.
const (
	VerdictAgrees    = "agrees"
	VerdictConcerns  = "concerns"
	VerdictDisagrees = "disagrees"
)

// Severities of a review point.
const (
	SeverityMinor    = "minor"
	SeverityMajor    = "major"
	SeverityBlocking = "blocking"
)

// ReviewPoint is one finding of a review.
type ReviewPoint struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
	File     string `json:"file,omitempty"`
}

// ReviewFile is the content of /factory/out/review.json, from review.
type ReviewFile struct {
	SchemaVersion int                `json:"schemaVersion"`
	Subject       string             `json:"subject"`
	Verdict       string             `json:"verdict"`
	Points        []ReviewPoint      `json:"points,omitempty"`
	Criteria      []CriterionOpinion `json:"criteria,omitempty"`
}

// ParseReview reads review.json. Unknown fields are an error.
func ParseReview(data []byte) (*ReviewFile, error) {
	var r ReviewFile
	if err := decodeJSONStrict(data, &r); err != nil {
		return nil, fmt.Errorf("review.json: %w", err)
	}
	var p problems
	if r.SchemaVersion != SchemaVersion {
		p.add("schemaVersion", "must be %d", SchemaVersion)
	}
	switch r.Subject {
	case SubjectSpec, SubjectPlan, SubjectAcceptance, SubjectChange:
	default:
		p.add("subject", "must be one of %s, %s, %s, %s", SubjectSpec, SubjectPlan, SubjectAcceptance, SubjectChange)
	}
	switch r.Verdict {
	case VerdictAgrees, VerdictConcerns, VerdictDisagrees:
	default:
		p.add("verdict", "must be one of %s, %s, %s", VerdictAgrees, VerdictConcerns, VerdictDisagrees)
	}
	for i, pt := range r.Points {
		base := fmt.Sprintf("points[%d]", i)
		switch pt.Severity {
		case SeverityMinor, SeverityMajor, SeverityBlocking:
		default:
			p.add(base+".severity", "must be one of %s, %s, %s", SeverityMinor, SeverityMajor, SeverityBlocking)
		}
		if blank(pt.Text) {
			p.add(base+".text", "must be set")
		}
		if pt.File != "" {
			checkFilePath(&p, base+".file", pt.File)
		}
	}
	checkOpinions(&p, "criteria", r.Criteria)
	if err := p.err(); err != nil {
		return nil, fmt.Errorf("review.json: %w", err)
	}
	return &r, nil
}
