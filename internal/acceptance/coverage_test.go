package acceptance

import (
	"reflect"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func manifestOf(cs ...api.ManifestCriterion) *api.Manifest {
	return &api.Manifest{SchemaVersion: api.SchemaVersion, Goal: 1, Criteria: cs}
}

func TestCheckCoverage_Clean(t *testing.T) {
	spec := []SpecCriterion{{1, "A customer can move a booking"}, {2, "Works on a phone"}}
	m := manifestOf(crit(1, "A customer can move a booking", "true"), crit(2, "Works on a phone", ""))
	rep := CheckCoverage(spec, m)
	if !rep.OK() || rep.Err() != nil {
		t.Errorf("report = %+v, err %v", rep, rep.Err())
	}
}

func TestCheckCoverage_Problems(t *testing.T) {
	spec := []SpecCriterion{{1, "one"}, {2, "two"}, {3, "three"}, {3, "three again"}, {4, "four"}}
	m := manifestOf(
		crit(1, "one", "true"),
		crit(3, "three", "true"),
		crit(4, "four but different", "true"),
		crit(4, "four", "true"),
		crit(9, "invented", "true"),
	)
	rep := CheckCoverage(spec, m)
	if rep.OK() {
		t.Fatal("must not be OK")
	}
	if want := []int{2}; !reflect.DeepEqual(rep.Missing, want) {
		t.Errorf("missing = %v", rep.Missing)
	}
	if want := []int{9}; !reflect.DeepEqual(rep.Unknown, want) {
		t.Errorf("unknown = %v", rep.Unknown)
	}
	if want := []int{3, 4}; !reflect.DeepEqual(rep.Duplicate, want) {
		t.Errorf("duplicate = %v", rep.Duplicate)
	}
	if want := []int{4}; !reflect.DeepEqual(rep.Differs, want) {
		t.Errorf("differs = %v", rep.Differs)
	}
	msg := rep.Err().Error()
	for _, want := range []string{"criterion 2", "criterion 9", "criterion 3", "criterion 4", "no entry", "not in the spec", "more than once", "wording"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
}

func TestCheckCoverage_WordingIsCompared(t *testing.T) {
	spec := []SpecCriterion{{1, "A customer can move a booking"}}
	cases := map[string]bool{
		"A customer can move a booking":     true,
		"  A customer can move a booking\n": true, // only the ends are trimmed
		"A customer can move a Booking":     false,
		"A customer can  move a booking":    false,
		"A customer can move a booking.":    false,
		"A customer can move a booking​":    false,
	}
	for text, ok := range cases {
		rep := CheckCoverage(spec, manifestOf(crit(1, text, "true")))
		if rep.OK() != ok {
			t.Errorf("%q: OK = %v, want %v", text, rep.OK(), ok)
		}
	}
}

func TestCheckCoverage_HostileTextInErrorIsInert(t *testing.T) {
	spec := []SpecCriterion{{1, "one"}}
	rep := CheckCoverage(spec, manifestOf(crit(1, "evil\x1b[31m\nred", "true")))
	msg := rep.Err().Error()
	if strings.ContainsAny(msg, "\x1b\r") || strings.Count(msg, "\n") > 1 {
		t.Errorf("error holds raw control characters: %q", msg)
	}
}

func TestCheckCoverage_NilManifest(t *testing.T) {
	if CheckCoverage([]SpecCriterion{{1, "a"}}, nil).OK() {
		t.Error("a nil manifest must not be OK")
	}
	if CheckCoverage(nil, nil).OK() {
		t.Error("no manifest must not be OK")
	}
}

func TestCheckCoverage_EmptySpec(t *testing.T) {
	if CheckCoverage(nil, manifestOf(crit(1, "a", "true"))).OK() {
		t.Error("entries with no spec are all unknown")
	}
}
