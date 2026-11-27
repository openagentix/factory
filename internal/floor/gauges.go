package floor

import (
	"sort"
	"time"
)

const maxAttentionActions = 1_000_000 // a larger value is not believed, and cannot overflow the sum

const gaugeMinSamples = 5 // record 0004: every gauge with a threshold needs five cases

func (c *ctx) sourceGaugeState(keys ...SourceKey) (GaugeState, bool) {
	notBuilt := false
	for _, k := range keys {
		switch c.src[k] {
		case SourceReadFailed:
			return GaugeReadFailed, false
		case SourceNotBuilt:
			notBuilt = true
		}
	}
	if notBuilt {
		return GaugeNotBuilt, false
	}
	return GaugeOK, true
}

func (c *ctx) buildGauges(pieces []Piece) []Gauge {
	return []Gauge{c.gaugeInWork(pieces), c.gaugeAccepted(), c.gaugeRightFirstTime(), c.gaugeGoalToHolding(), c.gaugeAttention()}
}

func (c *ctx) gaugeInWork(pieces []Piece) Gauge {
	g := Gauge{Key: GaugeInWork, Window: WindowNow}
	st, ok := c.sourceGaugeState(SourceHost, SourceStore, SourceConfig, SourceTeam)
	if c.src[SourceTeam] == SourceNotBuilt && st == GaugeNotBuilt {
		st, ok = GaugeOK, true // a single machine has no team to read
		for _, k := range []SourceKey{SourceHost, SourceStore, SourceConfig} {
			if c.src[k] != SourceOK {
				st, ok = c.sourceGaugeState(k)
				break
			}
		}
	}
	g.State = st
	if !ok {
		return g
	}
	n := 0
	others := map[string]bool{}
	for _, p := range pieces {
		if p.Station >= 1 && p.Station <= 6 {
			n++
		}
		if p.State == StateWorking && p.Machine != nil && !p.Machine.Mine {
			others[p.Machine.ID] = true
		}
	}
	g.Value = ptr(int64(n))
	g.Machines = ptr(1 + len(others))
	return g
}

func (c *ctx) gaugeAccepted() Gauge {
	g := Gauge{Key: GaugeAcceptedToday, Window: WindowToday}
	st, ok := c.sourceGaugeState(SourceStaging)
	g.State = st
	if !ok {
		return g
	}
	if c.acceptedBad {
		g.State = GaugeReadFailed // an accepted change with no ticket cannot be counted
		return g
	}
	series := make([]int, 7)
	today := 0
	for _, list := range c.accepted {
		for _, a := range list {
			t, tok := known(a.At)
			if !tok || t.After(c.now) {
				// An accepted change must have a time. Counting it as zero would be a lie.
				g.State = GaugeReadFailed
				return g
			}
			if !t.Before(c.dayStart) {
				today++
			}
			for i := 0; i < 7; i++ {
				ds := c.dayStart.AddDate(0, 0, i-6)
				if !t.Before(ds) && t.Before(ds.AddDate(0, 0, 1)) {
					series[i]++
				}
			}
		}
	}
	g.Value, g.Series = ptr(int64(today)), series
	return g
}

func (c *ctx) gaugeRightFirstTime() Gauge {
	g := Gauge{Key: GaugeRightFirstTime, Window: Window30d}
	st, ok := c.sourceGaugeState(SourceStore)
	g.State = st
	if !ok {
		return g
	}
	from := c.now.AddDate(0, 0, -30)
	seen := map[int]bool{}
	n, good := 0, 0
	for _, f := range c.in.FirstAttempts.Val {
		if seen[f.Ticket] || f.Ticket <= 0 {
			g.State = GaugeReadFailed // two answers for one ticket, or no ticket
			return g
		}
		seen[f.Ticket] = true
		t, tok := known(f.EndedAt)
		if !tok {
			continue // the first attempt has not ended
		}
		if t.After(c.now) {
			g.State = GaugeReadFailed
			return g
		}
		if t.After(from) {
			n++
			if f.ReachedHolding {
				good++
			}
		}
	}
	g.Samples = ptr(n)
	if n < gaugeMinSamples {
		g.State = GaugeNotEnoughYet
		return g
	}
	g.State = GaugeOK
	g.Value = ptr(int64((good*100 + n/2) / n)) // whole percent, rounded half up
	return g
}

func (c *ctx) gaugeGoalToHolding() Gauge {
	g := Gauge{Key: GaugeGoalToHolding, Window: Window30d}
	st, ok := c.sourceGaugeState(SourceStaging)
	g.State = st
	if !ok {
		return g
	}
	from := c.now.AddDate(0, 0, -30)
	seen := map[int]bool{}
	var secs []int64
	for _, e := range c.in.GoalTimings.Val {
		if seen[e.Goal] || e.Goal <= 0 {
			g.State = GaugeReadFailed
			return g
		}
		seen[e.Goal] = true
		h, hok := known(e.FirstHoldingAt)
		if !hok {
			continue // the goal has not reached the holding area
		}
		a, aok := known(e.ApprovedAt)
		if !aok || h.Before(a) || h.After(c.now) {
			g.State = GaugeReadFailed
			return g
		}
		if h.After(from) {
			secs = append(secs, int64(h.Sub(a)/time.Second))
		}
	}
	g.Samples = ptr(len(secs))
	if len(secs) < gaugeMinSamples {
		g.State = GaugeNotEnoughYet
		return g
	}
	sort.Slice(secs, func(i, j int) bool { return secs[i] < secs[j] })
	m := len(secs) / 2
	v := secs[m]
	if len(secs)%2 == 0 {
		v = (secs[m-1] + secs[m]) / 2
	}
	g.State, g.Value = GaugeOK, ptr(v)
	return g
}

// gaugeAttention is not built in the first release. When the journal is stored
// and actions on the git host are counted, the value is the actions per goal
// with an accepted change in 7 days, in hundredths.
func (c *ctx) gaugeAttention() Gauge {
	g := Gauge{Key: GaugeAttention, Window: Window7d}
	if c.src[SourceStore] == SourceReadFailed {
		g.State = GaugeReadFailed
		return g
	}
	if c.src[SourceStore] == SourceNotBuilt || !c.in.Attention.Known {
		g.State = GaugeNotBuilt
		return g
	}
	from := c.now.AddDate(0, 0, -7)
	actions := map[int]int{}
	for _, a := range c.in.Attention.Val {
		t, tok := known(a.AcceptedAt)
		if !tok || t.After(c.now) || a.Actions < 0 || a.Actions > maxAttentionActions || a.Goal <= 0 {
			g.State = GaugeReadFailed
			return g
		}
		if t.After(from) {
			actions[a.Goal] += a.Actions
		}
	}
	g.Samples = ptr(len(actions))
	if len(actions) < gaugeMinSamples {
		g.State = GaugeNotEnoughYet
		return g
	}
	total := 0
	for _, n := range actions {
		total += n
	}
	g.State, g.Value = GaugeOK, ptr(int64((total*100+len(actions)/2)/len(actions)))
	return g
}
