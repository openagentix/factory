package api

import (
	"fmt"
	"strings"
)

// SchemaVersion is the version of result.json and the evidence bundle.
const SchemaVersion = 1

// Result statuses a worker may report.
const (
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusNeedsHuman = "needs_human"
	StatusNoChange   = "no_change"
)

// Usage is cost and token use, with the source of the numbers.
type Usage struct {
	// CostUSD is nil when the harness does not report cost ("unknown").
	CostUSD      *float64 `json:"costUSD,omitempty" yaml:"costUSD,omitempty"`
	InputTokens  int64    `json:"inputTokens,omitempty" yaml:"inputTokens,omitempty"`
	OutputTokens int64    `json:"outputTokens,omitempty" yaml:"outputTokens,omitempty"`
	// Source says who counted, for example "harness".
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Enforced is true only when a governance provider enforced the limit.
	Enforced bool `json:"enforced,omitempty" yaml:"enforced,omitempty"`
}

// HarnessInfo names the harness that ran and its version and model.
type HarnessInfo struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	Model   string `json:"model,omitempty" yaml:"model,omitempty"`
}

// Result is the content of /factory/out/result.json.
type Result struct {
	SchemaVersion int         `json:"schemaVersion"`
	Status        string      `json:"status"`
	Summary       string      `json:"summary"`
	Claims        []string    `json:"claims,omitempty"`
	Questions     []string    `json:"questions,omitempty"`
	Usage         Usage       `json:"usage"`
	Harness       HarnessInfo `json:"harness"`
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
	if strings.TrimSpace(r.Summary) == "" {
		p.add("summary", "must be set")
	}
	if err := p.err(); err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	return &r, nil
}
