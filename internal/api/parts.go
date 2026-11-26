package api

import (
	"fmt"
	"strings"
)

// Opinions of a worker or a reviewer on one acceptance criterion. An opinion
// is shown in the engineering view only. It never sets a criterion's state.
const (
	OpinionMet    = "met"
	OpinionNotMet = "not_met"
	OpinionUnsure = "unsure"
)

// Claim is one thing a worker says it did, with the files it names.
type Claim struct {
	Text  string   `json:"text"`
	Files []string `json:"files"`
}

// CriterionOpinion is an opinion on one numbered acceptance criterion.
type CriterionOpinion struct {
	ID      int    `json:"id"`
	Opinion string `json:"opinion"`
	Note    string `json:"note,omitempty"`
}

// Ruling is a choice the worker made where its method would have asked a
// person. It can be undone and it is inside the ticket.
type Ruling struct {
	Decision    string `json:"decision"`
	Why         string `json:"why"`
	CostIfWrong string `json:"costIfWrong"`
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func checkClaims(p *problems, field string, claims []Claim) {
	for i, c := range claims {
		base := fmt.Sprintf("%s[%d]", field, i)
		if blank(c.Text) {
			p.add(base+".text", "must be set")
		}
		for j, f := range c.Files {
			checkFilePath(p, fmt.Sprintf("%s.files[%d]", base, j), f)
		}
	}
}

func checkOpinions(p *problems, field string, list []CriterionOpinion) {
	seen := map[int]bool{}
	for i, c := range list {
		base := fmt.Sprintf("%s[%d]", field, i)
		switch {
		case c.ID < 1:
			p.add(base+".id", "must be 1 or more")
		case seen[c.ID]:
			p.add(base+".id", "duplicate criterion %d", c.ID)
		}
		seen[c.ID] = true
		switch c.Opinion {
		case OpinionMet, OpinionNotMet, OpinionUnsure:
		default:
			p.add(base+".opinion", "must be one of %s, %s, %s", OpinionMet, OpinionNotMet, OpinionUnsure)
		}
	}
}

func checkRulings(p *problems, field string, list []Ruling) {
	for i, r := range list {
		base := fmt.Sprintf("%s[%d]", field, i)
		if blank(r.Decision) {
			p.add(base+".decision", "must be set")
		}
		if blank(r.Why) {
			p.add(base+".why", "must be set")
		}
		if blank(r.CostIfWrong) {
			p.add(base+".costIfWrong", "must be set")
		}
	}
}

// checkFilePath checks a path that is only compared with changed files.
func checkFilePath(p *problems, field, f string) {
	switch {
	case f == "":
		p.add(field, "must not be empty")
	case !cleanRelativePath(f):
		p.add(field, "must be a clean relative path: no leading /, no backslash, no empty, . or .. part")
	}
}
