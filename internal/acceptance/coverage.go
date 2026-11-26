package acceptance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/openagentix/factory/internal/api"
)

// SpecCriterion is a criterion as the approved spec states it.
type SpecCriterion struct {
	ID   int
	Text string
}

// CoverageReport says how a manifest matches the spec's criteria. Each list
// is sorted. The manifest is valid for building only when OK is true.
type CoverageReport struct {
	// NoManifest is true when no manifest was given.
	NoManifest bool
	// NoSpec is true when the spec has no criteria.
	NoSpec bool
	// InvalidID lists ids below 1, in the spec or the manifest.
	InvalidID []int
	// Missing lists spec criteria with no manifest entry.
	Missing []int
	// Unknown lists manifest entries whose id is not in the spec.
	Unknown []int
	// Duplicate lists ids that appear more than once in the spec or in the
	// manifest.
	Duplicate []int
	// Differs lists ids whose manifest wording is not the spec's wording.
	Differs []int
}

// OK reports whether the manifest matches the spec exactly.
func (r CoverageReport) OK() bool {
	return !r.NoManifest && !r.NoSpec && len(r.InvalidID) == 0 && len(r.Missing) == 0 && len(r.Unknown) == 0 && len(r.Duplicate) == 0 && len(r.Differs) == 0
}

// Err returns nil when the report is OK, and otherwise one error that names
// every problem by criterion number. It holds no wording from any file.
func (r CoverageReport) Err() error {
	if r.OK() {
		return nil
	}
	if r.NoManifest {
		return errors.New("acceptance manifest: there is no manifest, so the manifest is not valid for building")
	}
	var parts []string
	add := func(ids []int, format string) {
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf(format, id))
		}
	}
	if r.NoSpec {
		parts = append(parts, "the spec has no criteria")
	}
	add(r.InvalidID, "criterion %d is not 1 or more")
	add(r.Missing, "criterion %d has no entry in the manifest")
	add(r.Unknown, "criterion %d is in the manifest and is not in the spec")
	add(r.Duplicate, "criterion %d appears more than once")
	add(r.Differs, "criterion %d has wording in the manifest that differs from the spec")
	return fmt.Errorf("acceptance manifest is not valid for building: %s", strings.Join(parts, "; "))
}

// CheckCoverage compares the manifest with the criteria of the spec. The
// wording must be equal, except that spaces at either end are ignored.
func CheckCoverage(spec []SpecCriterion, m *api.Manifest) CoverageReport {
	var rep CoverageReport
	if m == nil {
		rep.NoManifest = true
		return rep
	}
	rep.NoSpec = len(spec) == 0
	dup := map[int]bool{}
	invalid := map[int]bool{}
	specDup := map[int]bool{}
	specText := map[int]string{}
	for _, s := range spec {
		if s.ID < 1 {
			invalid[s.ID] = true
		}
		if _, ok := specText[s.ID]; ok {
			dup[s.ID] = true
			specDup[s.ID] = true
		}
		specText[s.ID] = s.Text
	}
	seen := map[int]bool{}
	for _, c := range m.Criteria {
		if c.ID < 1 {
			invalid[c.ID] = true
		}
		if seen[c.ID] {
			dup[c.ID] = true
			continue
		}
		seen[c.ID] = true
		want, ok := specText[c.ID]
		switch {
		case !ok:
			rep.Unknown = append(rep.Unknown, c.ID)
		case !specDup[c.ID] && strings.TrimSpace(c.Text) != strings.TrimSpace(want):
			rep.Differs = append(rep.Differs, c.ID)
		}
	}
	for id := range specText {
		if !seen[id] {
			rep.Missing = append(rep.Missing, id)
		}
	}
	for id := range invalid {
		rep.InvalidID = append(rep.InvalidID, id)
	}
	for id := range dup {
		rep.Duplicate = append(rep.Duplicate, id)
	}
	for _, l := range [][]int{rep.Missing, rep.Unknown, rep.Duplicate, rep.Differs, rep.InvalidID} {
		sort.Ints(l)
	}
	return rep
}
