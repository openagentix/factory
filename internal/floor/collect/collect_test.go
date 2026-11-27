package collect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/floor"
)

type badLedger struct{}

func (badLedger) Facts(context.Context) (LedgerFacts, error) { return LedgerFacts{}, errors.New("no") }

// A failed read never becomes a zero: with nothing to read from, every source
// the line cannot run without is read_failed, the fields stay unknown, and the
// lamp says "could not read".
func TestFailedReadsStayUnknown(t *testing.T) {
	now := time.Now()
	for name, s := range map[string]Sources{
		"nothing":      {Now: func() time.Time { return now }, Mode: floor.ModeDemo, Machine: "m1", Repository: "a/b"},
		"ledger fails": {Now: func() time.Time { return now }, Mode: floor.ModeDemo, Machine: "m1", Repository: "a/b", Ledger: badLedger{}},
	} {
		in := Collect(context.Background(), s)
		for _, k := range []floor.SourceKey{floor.SourceHost, floor.SourceStore, floor.SourceConfig} {
			if in.Sources[k] != floor.SourceReadFailed {
				t.Errorf("%s: source %s is %s", name, k, in.Sources[k])
			}
		}
		if in.Runs.Known || in.Goals.Known || in.Tickets.Known || in.Holding.Known || in.Gate.Known {
			t.Errorf("%s: an unread field is known", name)
		}
		f := floor.Project(in)
		if f.Signal.Lamp != floor.LampUnknown {
			t.Errorf("%s: lamp %s", name, f.Signal.Lamp)
		}
	}
}
