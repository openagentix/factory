package floor

import (
	"fmt"
	"sort"
	"time"
)

// sortCanon returns a copy of s in one fixed order that does not depend on the
// order the caller used. The order is the text of each element. Equal elements
// print equal, so they may sit in either place. Inputs hold no pointers, so
// the text is the value.
func sortCanon[T any](s []T) []T {
	type kv struct {
		k string
		v T
	}
	tmp := make([]kv, len(s))
	for i, v := range s {
		tmp[i] = kv{fmt.Sprintf("%+v", v), v}
	}
	sort.SliceStable(tmp, func(i, j int) bool { return tmp[i].k < tmp[j].k })
	out := make([]T, len(s))
	for i := range tmp {
		out[i] = tmp[i].v
	}
	return out
}

// known returns the time of an Opt and whether it is usable: known and not the
// zero time.
func known(o Opt[time.Time]) (time.Time, bool) {
	return o.Val, o.Known && !o.Val.IsZero()
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func ptr[T any](v T) *T { return &v }

func itoa(n int) string { return fmt.Sprintf("%d", n) }
