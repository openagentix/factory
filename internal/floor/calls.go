package floor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// A call's version is the SHA-256 of a canonical encoding, in hex. The
// encoding is a list of fields. Each field is written as its length in bytes,
// a colon, its bytes, and a bar, so no field can run into the next. The fields
// are, in order:
//
//	"oxf.floor.call.v1", id, kind, who,
//	since ("-" when unknown), goal ("-" when none), ticket ("-" when none),
//	the question's full raw text ("-" when none),
//	the facts as key=value in key order, joined by commas,
//	the actions in their order, joined by commas,
//	then the extra fields of the kind.
//
// So the version covers everything the person is shown (the shown fields),
// the text beyond the cut of a question, and what the action depends on.
// The extra fields are: a question, its comment id; a consequence, run,
// attempt, commit, risk and the machine that holds the decision; a plan, the
// lane, the lines as read from the goal body and the body's last edit time; an
// Accept, stagingSeq, commit, pending flag, mode, pull request number and the
// sorted tickets in the holding area. A statement has none.
// Time of day, other pieces and other calls are not in it.
func callVersion(c Call, rawQuestion string, extra []string) string {
	h := sha256.New()
	w := func(s string) { fmt.Fprintf(h, "%d:%s|", len(s), s) }
	opt := func(p *string) string {
		if p == nil {
			return "-"
		}
		return *p
	}
	optN := func(p *int) string {
		if p == nil {
			return "-"
		}
		return itoa(*p)
	}
	w("oxf.floor.call.v1")
	w(c.ID)
	w(string(c.Kind))
	w(string(c.Who))
	w(opt(c.Since))
	w(optN(c.Goal))
	w(optN(c.Ticket))
	if c.Question == nil {
		w("-")
	} else {
		w(rawQuestion)
	}
	keys := make([]string, 0, len(c.Facts))
	for k := range c.Facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var fs []string
	for _, k := range keys {
		b, _ := c.Facts[k].MarshalJSON()
		fs = append(fs, k+"="+string(b))
	}
	w(strings.Join(fs, ","))
	var as []string
	for _, a := range c.Actions {
		as = append(as, string(a))
	}
	w(strings.Join(as, ","))
	for _, e := range extra {
		w(e)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *ctx) facts(kv ...any) map[string]Fact {
	m := map[string]Fact{}
	for i := 0; i+1 < len(kv); i += 2 {
		k := kv[i].(string)
		var f Fact
		var ok bool
		switch v := kv[i+1].(type) {
		case int:
			f, ok = NumFact(int64(v))
		case int64:
			f, ok = NumFact(v)
		case string:
			f, ok = MachineFact(v)
		case RiskReason:
			f, ok = RiskFact(v)
		}
		if ok {
			m[k] = f
		}
	}
	return m
}

func (c *ctx) store(call Call, raw string, extra []string) string {
	if call.Actions == nil {
		call.Actions = []ActionName{}
	}
	call.Version = callVersion(call, raw, extra)
	c.calls[call.ID] = call
	return call.ID
}

// addConsequenceCall makes the call for a change that waits for a yes or no.
// It returns "" when the gate's facts are not good enough to ask a person.
func (c *ctx) addConsequenceCall(tk TicketIn, g GateIn) string {
	if !g.Risk.Valid() || !runIDPattern.MatchString(g.Run) || g.Attempt < 1 || len(g.Commit) < 40 ||
		!commitPattern.MatchString(g.Commit) || !ValidMachineID(g.Machine) {
		return ""
	}
	call := Call{ID: "c-" + itoa(tk.Number) + "-" + g.Run, Kind: CallConsequence, Who: WhoAnyoneTrusted,
		Since: c.fmtPast(g.Since), Ticket: ptr(tk.Number), Actions: []ActionName{ActionYes, ActionNo},
		Facts: c.facts("ticket", tk.Number, "attempt", g.Attempt, "risk", g.Risk, "machine", g.Machine)}
	if tk.Goal > 0 {
		call.Goal = ptr(tk.Goal)
		call.Facts["goal"], _ = NumFact(int64(tk.Goal))
	}
	return c.store(call, "", []string{g.Run, itoa(g.Attempt), g.Commit, string(g.Risk), g.Machine})
}

// buildCalls adds the calls that do not come from one piece's decision: the
// questions, the plans, the Accept and the statement about the project's own
// checks. The consequence calls were added while the pieces were decided.
func (c *ctx) buildCalls(pieces []Piece) {
	if c.failAll {
		return // nothing can be asked of a person when the facts cannot be read
	}
	for _, qs := range c.questions {
		for _, q := range qs {
			if q.Ticket <= 0 || q.CommentID <= 0 {
				continue
			}
			call := Call{ID: questionID(q), Kind: CallQuestion, Who: WhoAnyoneTrusted, Since: c.fmtPast(q.Since),
				Ticket: ptr(q.Ticket), Question: ptr(NewUntrusted(q.Text, 1000)), Actions: []ActionName{ActionAnswer},
				Facts: c.facts("ticket", q.Ticket)}
			if q.Goal > 0 {
				call.Goal = ptr(q.Goal)
				call.Facts["goal"], _ = NumFact(int64(q.Goal))
			}
			c.store(call, q.Text, []string{itoa(int(q.CommentID))})
		}
	}
	for _, g := range c.goalList {
		if !g.Open || !g.Approved.Known || g.Approved.Val {
			continue
		}
		call := Call{ID: "p-" + itoa(g.Number), Kind: CallPlan, Who: WhoAnyoneTrusted, Since: c.fmtPast(g.PlanSince),
			Goal: ptr(g.Number), Actions: []ActionName{}, Facts: c.facts("goal", g.Number)}
		extra := []string{string(g.Lane)}
		if g.PlanLines.Known {
			extra = append(extra, itoa(len(g.PlanLines.Val)))
			extra = append(extra, g.PlanLines.Val...)
			// Approving needs a quick lane goal whose lines were read.
			if g.Lane == LaneQuick && len(g.PlanLines.Val) >= 1 && len(g.PlanLines.Val) <= 3 {
				call.Actions = []ActionName{ActionApprove}
			}
		} else {
			extra = append(extra, "unread")
		}
		if t, ok := known(g.BodyEditedAt); ok {
			extra = append(extra, itoa(int(t.Unix())), itoa(t.Nanosecond()))
		} else {
			extra = append(extra, "-")
		}
		c.store(call, "", extra)
	}
	if c.ok(SourceStaging) && c.checks {
		c.store(Call{ID: "s-project-checks", Kind: CallStatement, Who: WhoADeveloper, Actions: []ActionName{}, Facts: map[string]Fact{}}, "", nil)
	}
	if c.ok(SourceStaging) && c.holding.Exists && c.holding.Published {
		h := c.holding
		// The call counts the pieces that are at 06, and nothing else. An entry
		// already accepted was taken out of the holding area before this point.
		tickets := map[int]bool{}
		for _, p := range pieces {
			if p.Station == 6 && p.Ticket != nil {
				tickets[*p.Ticket] = true
			}
		}
		if len(tickets) == 0 {
			return
		}
		allAt, first := true, ""
		for _, e := range h.Applied {
			if !tickets[e.Ticket] {
				continue
			}
			if s := c.fmtPast(e.At); s == nil {
				allAt = false
			} else if first == "" || *s < first {
				first = *s
			}
		}
		var tl []int
		for t := range tickets {
			tl = append(tl, t)
		}
		sort.Ints(tl)
		call := Call{ID: "a-" + itoa(int(h.StagingSeq)), Kind: CallAccept, Actions: []ActionName{}, Facts: c.facts("count", len(tl))}
		// The version covers what the action depends on: the state, frozen,
		// published and pending, and who holds staging. (Fix round 1.)
		extra := []string{itoa(int(h.StagingSeq)), h.Commit, fmt.Sprint(h.PendingAccept), string(h.PendingMode), itoa(h.PullRequest),
			fmt.Sprint(h.Frozen), fmt.Sprint(h.Published), fmt.Sprint(h.Mine)}
		for _, t := range tl {
			extra = append(extra, itoa(t))
		}
		switch {
		case h.PendingAccept && h.PendingMode == PendingDeveloper:
			call.Who = WhoADeveloper
		case h.PendingAccept:
			call.Who = WhoThisMachine
			if h.Mine {
				call.Actions = []ActionName{ActionCancel}
			}
		default:
			call.Who = WhoThisMachine
			if h.Mine && !h.Frozen && h.Published {
				call.Actions = []ActionName{ActionAccept} // never on a frozen state
			}
		}
		if h.PendingAccept {
			call.Since = c.fmtPast(h.PendingSince)
		} else if allAt && first != "" {
			call.Since = ptr(first)
		}
		c.store(call, "", extra)
	}
}

// sortedCalls returns the calls, oldest first. A call with no known time goes
// last. Equal times are ordered by id.
func (c *ctx) sortedCalls() []Call {
	out := make([]Call, 0, len(c.calls))
	for _, call := range c.calls {
		out = append(out, call)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Since == nil) != (b.Since == nil) {
			return b.Since == nil
		}
		if a.Since != nil && *a.Since != *b.Since {
			return *a.Since < *b.Since
		}
		return a.ID < b.ID
	})
	return out
}
