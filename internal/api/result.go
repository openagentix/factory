package api

import "fmt"

// SchemaVersion is the version of result.json and the evidence bundle.
const SchemaVersion = 1

// Result statuses a worker may report.
const (
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusNeedsHuman = "needs_human"
	StatusNoChange   = "no_change"
)

// Billing says how the usage was paid for.
const (
	// BillingMetered is an API key or gateway: dollars and tokens.
	BillingMetered = "metered"
	// BillingPlan is a subscription login: tokens and no dollar figure.
	BillingPlan = "plan"
	// BillingUnknown is for when the harness does not say.
	BillingUnknown = "unknown"
)

// Usage is cost and token use. A dollar figure is valid only for metered
// billing and is never estimated for plan usage.
type Usage struct {
	Billing string `json:"billing,omitempty"`
	// CostUSD is set only when Billing is metered.
	CostUSD      *float64 `json:"costUSD,omitempty"`
	InputTokens  int64    `json:"inputTokens,omitempty"`
	OutputTokens int64    `json:"outputTokens,omitempty"`
	// Source says who counted, for example "harness". The orchestrator
	// writes it. A value from the worker is ignored.
	Source string `json:"source,omitempty"`
	// Enforced is true only when a governance provider enforced the limit.
	// The orchestrator writes it. A value from the worker is ignored.
	Enforced bool `json:"enforced,omitempty"`
}

// HarnessInfo names the harness that ran and its version and model.
type HarnessInfo struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	Model   string `json:"model,omitempty" yaml:"model,omitempty"`
}

// Result is the content of /factory/out/result.json.
type Result struct {
	SchemaVersion int                `json:"schemaVersion"`
	Status        string             `json:"status"`
	Summary       string             `json:"summary"`
	Claims        []Claim            `json:"claims,omitempty"`
	Criteria      []CriterionOpinion `json:"criteria,omitempty"`
	Rulings       []Ruling           `json:"rulings,omitempty"`
	Questions     []string           `json:"questions,omitempty"`
	Usage         Usage              `json:"usage"`
	Harness       HarnessInfo        `json:"harness"`
}

// ParseResult reads result.json. Unknown fields are an error. The schema
// version must be 1, the status one of the known values and the summary set.
func ParseResult(data []byte) (*Result, error) {
	var r Result
	if err := decodeJSONStrict(data, &r); err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	var p problems
	if r.SchemaVersion != SchemaVersion {
		p.add("schemaVersion", "must be %d", SchemaVersion)
	}
	switch r.Status {
	case StatusSucceeded, StatusFailed, StatusNeedsHuman, StatusNoChange:
	default:
		p.add("status", "must be one of %s, %s, %s, %s", StatusSucceeded, StatusFailed, StatusNeedsHuman, StatusNoChange)
	}
	if blank(r.Summary) {
		p.add("summary", "must be set")
	}
	checkClaims(&p, "claims", r.Claims)
	checkOpinions(&p, "criteria", r.Criteria)
	checkRulings(&p, "rulings", r.Rulings)
	for i, q := range r.Questions {
		if blank(q) {
			p.add(fmt.Sprintf("questions[%d]", i), "must not be empty")
		}
	}
	// The orchestrator writes these two. Whatever the worker wrote is dropped.
	r.Usage.Source, r.Usage.Enforced = "", false
	checkUsage(&p, "usage", &r.Usage)
	if err := p.err(); err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	return &r, nil
}

func checkUsage(p *problems, field string, u *Usage) {
	switch u.Billing {
	case "":
		u.Billing = BillingUnknown
	case BillingMetered, BillingPlan, BillingUnknown:
	default:
		p.add(field+".billing", "must be one of %s, %s, %s", BillingMetered, BillingPlan, BillingUnknown)
	}
	if u.CostUSD != nil {
		switch {
		case u.Billing != BillingMetered:
			p.add(field+".costUSD", "is valid only with billing %s", BillingMetered)
		case *u.CostUSD < 0:
			p.add(field+".costUSD", "must be zero or more")
		}
	}
	if u.InputTokens < 0 {
		p.add(field+".inputTokens", "must be zero or more")
	}
	if u.OutputTokens < 0 {
		p.add(field+".outputTokens", "must be zero or more")
	}
}
