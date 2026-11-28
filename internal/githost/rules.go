package githost

import (
	"regexp"
	"strings"
)

var (
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	branchRE = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// checkBranchName rejects names git would refuse or that could be read as an option.
func checkBranchName(b string) error {
	switch {
	case b == "", !branchRE.MatchString(b), strings.HasPrefix(b, "-"),
		strings.Contains(b, ".."), strings.Contains(b, "//"),
		strings.HasSuffix(b, "/"), strings.HasSuffix(b, ".lock"), strings.HasSuffix(b, "."):
		return refuse("bad branch name")
	}
	// Git refuses a name part that starts with a dot or ends in ".lock".
	for _, part := range strings.Split(b, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return refuse("bad branch name")
		}
	}
	return nil
}

// asciiEqualFold compares two names without regard to ASCII letter case. A
// host that folds case treats names that differ only so as one branch. Other
// characters must match exactly.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// isStagingName reports whether a branch name, as a host or a person might
// write it, names the staging branch or could be taken for it: the exact name,
// with refs/heads/, in any letter case, with white space or a trailing slash.
// The default branch must never be one of these, because staging is force
// pushed.
func isStagingName(name string) bool {
	n := strings.TrimSpace(name)
	n = strings.TrimPrefix(n, "refs/heads/")
	n = strings.TrimRight(n, "/")
	return strings.EqualFold(strings.TrimSpace(n), StagingBranch)
}

// checkStagingPush is the rule for PushStaging: the default branch must be
// known, and must not be the staging branch. A blank name is a failure.
func checkStagingPush(defaultBranch string) error {
	if strings.TrimSpace(defaultBranch) == "" {
		return refuse("the default branch is not known, so the staging branch is not pushed")
	}
	if isStagingName(defaultBranch) {
		return refuse("the default branch is the staging branch %q: staging is force pushed and would rewrite it", StagingBranch)
	}
	return nil
}

// checkBranchPush is the push rule for PushBranch: only agent branches, and
// never the default branch.
func checkBranchPush(branch, defaultBranch string) error {
	if err := checkBranchName(branch); err != nil {
		return err
	}
	if asciiEqualFold(branch, defaultBranch) {
		return refuse("the default branch cannot be pushed")
	}
	if !strings.HasPrefix(branch, BranchPrefix) || len(branch) == len(BranchPrefix) {
		return refuse("only branches under %q may be pushed", BranchPrefix)
	}
	return nil
}

// checkCommit accepts only a full commit id, so a value can never be read as
// an option or a ref name.
func checkCommit(c string) error {
	if !commitRE.MatchString(c) {
		return refuse("commit must be a full lowercase hex id")
	}
	return nil
}

// checkPRBranches allows an agent branch into the staging branch, and the
// staging branch into the default branch. Nothing else.
func checkPRBranches(head, base, defaultBranch string) error {
	if checkStagingPush(defaultBranch) != nil {
		// A pull request into the staging branch would be a pull request
		// into the default branch, from a branch the gate never judged.
		return refuse("the default branch is not known or is the staging branch")
	}
	switch {
	case strings.HasPrefix(head, BranchPrefix) && !asciiEqualFold(head, defaultBranch) && base == StagingBranch:
		return checkBranchName(head)
	case head == StagingBranch && base == defaultBranch && base != StagingBranch:
		return nil
	}
	return refuse("a pull request must go from %q* into %q, or from %q into the default branch", BranchPrefix, StagingBranch, StagingBranch)
}

func checkLabel(l string) error {
	for _, f := range FixedLabels() {
		if l == f {
			return nil
		}
	}
	return refuse("label %q is not in the fixed list", l)
}

// isFactoryLogin reports whether the author is the factory's own user.
func isFactoryLogin(author, factoryLogin string) bool {
	return author != "" && factoryLogin != "" && strings.EqualFold(author, factoryLogin)
}

var loginRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,38}|[A-Za-z0-9-]*\[bot\])$`)

const (
	// maxListTitle and maxListBody bound the text of a pull request in a list.
	maxListTitle = 250
	maxListBody  = 65536
)

// checkListState allows the three states a list of pull requests may ask for.
func checkListState(st PRState) error {
	switch st {
	case PROpen, PRClosed, PRMerged:
		return nil
	}
	return refuse("the state of a pull request list must be open, closed or merged")
}

// isAgentBranch reports whether name is a valid agent branch: under
// BranchPrefix, with something after it, and a name git accepts.
func isAgentBranch(name string) bool {
	return strings.HasPrefix(name, BranchPrefix) && len(name) > len(BranchPrefix) && checkBranchName(name) == nil
}

// hasLabelFold reports whether the labels hold the label. The host treats
// label names that differ only by letter case as one label.
func hasLabelFold(labels []string, label string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, label) {
			return true
		}
	}
	return false
}

// redactLine makes untrusted host text safe to put in an error: one line, no
// control characters.
func redactLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}
