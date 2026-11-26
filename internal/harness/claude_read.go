package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// claudeLine is the part of one stream-json line the adapter uses. Fields it
// does not name are ignored, so a new field in a later version is harmless.
type claudeLine struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Message json.RawMessage `json:"message"`

	Timestamp string `json:"timestamp"`

	// init
	Model        string `json:"model"`
	APIKeySource string `json:"apiKeySource"`
	Version      string `json:"claude_code_version"`

	// permission_denied
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`

	// result
	IsError       *bool          `json:"is_error"`
	NumTurns      int            `json:"num_turns"`
	DurationMS    int64          `json:"duration_ms"`
	TotalCostUSD  *float64       `json:"total_cost_usd"`
	Usage         *claudeUsage   `json:"usage"`
	Denials       []claudeDenial `json:"permission_denials"`
	TerminalError string         `json:"terminal_reason"`
}

type claudeUsage struct {
	Input         int64 `json:"input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	Output        int64 `json:"output_tokens"`
}

type claudeDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type claudeMessage struct {
	Content []json.RawMessage `json:"content"`
}

type claudeBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
}

// denyReason is the reason recorded for a denial. In this mode the program
// denies any request that is not on the allowlist, without asking.
const denyReason = "not allowed by the permission policy"

// Read implements Adapter.
func (c *Claude) Read(r io.Reader, emit func(api.Event)) (Summary, error) {
	now := clock(c.Now)
	var (
		sum      Summary
		last     time.Time
		inits    int
		requests = map[string]string{} // tool_use id to a short request text
		live     []api.Denial
	)
	sum.Usage.Billing = api.BillingUnknown
	at := func() time.Time {
		if !last.IsZero() {
			return last
		}
		return now()
	}
	send := func(kind, text string) { emit(api.Event{Time: at(), Kind: kind, Text: clip(text)}) }

	err := eachLine(r, func(line []byte) {
		var l claudeLine
		if json.Unmarshal(line, &l) != nil || l.Type == "" {
			sum.Skipped++
			return
		}
		if l.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
				last = t
			}
		}
		switch l.Type {
		case "system":
			switch l.Subtype {
			case "init":
				inits++
				b := claudeBilling(l.APIKeySource)
				if inits == 1 {
					sum.Harness = api.HarnessInfo{Name: c.Name(), Version: l.Version, Model: l.Model}
					sum.Usage.Billing = b
				} else if b != sum.Usage.Billing {
					// Billing is set by the first init event. A later event that
					// disagrees must not hide a metered run, so the run is metered.
					sum.Usage.Billing = api.BillingMetered
				}
				send(api.EventStatus, fmt.Sprintf("started %s %s %s", c.Name(), l.Version, l.Model))
			case "permission_denied":
				req := requests[l.ToolUseID]
				if req == "" {
					req = l.ToolName
				}
				live = append(live, api.Denial{Request: req, Reason: denyReason})
				send(api.EventDenied, "denied: "+req)
			default:
				// Known system notes such as thinking counts and hooks.
			}
		case "assistant":
			var m claudeMessage
			if json.Unmarshal(l.Message, &m) != nil {
				sum.Skipped++
				return
			}
			for _, raw := range m.Content {
				var b claudeBlock
				if json.Unmarshal(raw, &b) != nil {
					sum.Skipped++
					continue
				}
				switch b.Type {
				case "text":
					if b.Text != "" {
						send(api.EventMessage, b.Text)
					}
				case "tool_use":
					req := requestText(b.Name, b.Input)
					requests[b.ID] = req
					send(api.EventTool, req)
				case "thinking", "redacted_thinking":
				default:
					sum.Skipped++
				}
			}
		case "user":
			// Tool results. They can be large and may hold anything, so they
			// do not go to the live view.
		case "result":
			sum.Complete = true
			sum.Turns = l.NumTurns
			sum.Duration = time.Duration(l.DurationMS) * time.Millisecond
			sum.Failed = (l.IsError != nil && *l.IsError) || (l.Subtype != "" && l.Subtype != "success")
			if u := l.Usage; u != nil {
				// Claude counts cached input apart from fresh input. Add them.
				var s1, s2 bool
				sum.Usage.InputTokens, s1 = addTokens(0, u.Input, u.CacheCreation, u.CacheRead)
				sum.Usage.OutputTokens, s2 = addTokens(0, u.Output)
				sum.TokensSaturated = s1 || s2
			}
			// A dollar figure is kept only for metered billing. The program
			// prints one for a subscription too, but it is an estimate.
			if sum.Usage.Billing == api.BillingMetered && l.TotalCostUSD != nil {
				v := *l.TotalCostUSD
				sum.Usage.CostUSD = &v
			}
			sum.Denials = sum.Denials[:0]
			for _, d := range l.Denials {
				req := requestText(d.ToolName, d.ToolInput)
				sum.Denials = append(sum.Denials, api.Denial{Request: req, Reason: denyReason})
			}
			send(api.EventStatus, fmt.Sprintf("finished: %d turns, %d denied", sum.Turns, len(sum.Denials)))
		default:
			sum.Skipped++
		}
	}, func() { sum.Skipped++ })

	if !sum.Complete {
		// No final event: use the denials seen on the way.
		sum.Denials = live
	}
	return sum, err
}

// claudeBilling reads the first event's apiKeySource. The value "none" means
// a subscription login. Any other value names a key source, which is metered.
func claudeBilling(source string) string {
	switch source {
	case "":
		return api.BillingUnknown
	case "none":
		return api.BillingPlan
	default:
		return api.BillingMetered
	}
}

// requestText is a short text for a tool call: the tool and its main input.
func requestText(tool string, input json.RawMessage) string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
		Skill    string `json:"skill"`
	}
	_ = json.Unmarshal(input, &in)
	for _, v := range []string{in.Command, in.FilePath, in.Skill, in.Pattern, in.Path} {
		if v != "" {
			return clip(tool + ": " + v)
		}
	}
	return clip(tool)
}
