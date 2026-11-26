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
	return nil
}

// checkBranchPush is the push rule for PushBranch: only agent branches, and
// never the default branch.
func checkBranchPush(branch, defaultBranch string) error {
	if err := checkBranchName(branch); err != nil {
		return err
	}
	if branch == defaultBranch {
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
	switch {
	case strings.HasPrefix(head, BranchPrefix) && head != defaultBranch && base == StagingBranch:
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
