package api

import "fmt"

// MaxIssuesPerRun is the most tickets one run may propose. The publish step
// creates at most this many issues per run.
const MaxIssuesPerRun = 12

// IssueProposal is one ticket a worker proposes.
type IssueProposal struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Criteria lists the numbered acceptance criteria the ticket serves.
	Criteria []int `json:"criteria"`
	// DependsOn lists indexes of other issues in the same file.
	DependsOn []int `json:"dependsOn"`
}

// IssuesFile is the content of /factory/out/issues.json, from decompose.
type IssuesFile struct {
	SchemaVersion int             `json:"schemaVersion"`
	Issues        []IssueProposal `json:"issues"`
}

// ParseIssues reads issues.json. Unknown fields are an error.
func ParseIssues(data []byte) (*IssuesFile, error) {
	var f IssuesFile
	if err := decodeJSONStrict(data, &f); err != nil {
		return nil, fmt.Errorf("issues.json: %w", err)
	}
	var p problems
	if f.SchemaVersion != SchemaVersion {
		p.add("schemaVersion", "must be %d", SchemaVersion)
	}
	n := len(f.Issues)
	switch {
	case n == 0:
		p.add("issues", "must hold at least one issue")
	case n > MaxIssuesPerRun:
		p.add("issues", "holds %d issues, the limit is %d", n, MaxIssuesPerRun)
	}
	for i, is := range f.Issues {
		base := fmt.Sprintf("issues[%d]", i)
		if blank(is.Title) {
			p.add(base+".title", "must be set")
		}
		seen := map[int]bool{}
		for j, c := range is.Criteria {
			switch {
			case c < 1:
				p.add(fmt.Sprintf("%s.criteria[%d]", base, j), "must be 1 or more")
			case seen[c]:
				p.add(fmt.Sprintf("%s.criteria[%d]", base, j), "duplicate criterion %d", c)
			}
			seen[c] = true
		}
		for j, d := range is.DependsOn {
			switch {
			case d < 0 || d >= n:
				p.add(fmt.Sprintf("%s.dependsOn[%d]", base, j), "must be the index of an issue in this file (0 to %d)", n-1)
			case d == i:
				p.add(fmt.Sprintf("%s.dependsOn[%d]", base, j), "an issue cannot depend on itself")
			}
		}
	}
	if len(p.list) == 0 && hasCycle(f.Issues) {
		p.add("issues", "dependsOn has a cycle")
	}
	if err := p.err(); err != nil {
		return nil, fmt.Errorf("issues.json: %w", err)
	}
	return &f, nil
}

// hasCycle reports whether the dependsOn links form a cycle. Indexes must be
// in range.
func hasCycle(issues []IssueProposal) bool {
	const (
		unseen = iota
		active
		done
	)
	state := make([]int, len(issues))
	var visit func(i int) bool
	visit = func(i int) bool {
		switch state[i] {
		case active:
			return true
		case done:
			return false
		}
		state[i] = active
		for _, d := range issues[i].DependsOn {
			if visit(d) {
				return true
			}
		}
		state[i] = done
		return false
	}
	for i := range issues {
		if visit(i) {
			return true
		}
	}
	return false
}
