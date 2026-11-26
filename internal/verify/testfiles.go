package verify

import (
	"sort"

	"github.com/openagentix/factory/internal/api"
)

// DefaultTestPatterns are the patterns that mark a test file. They match at
// any depth. The caller may pass its own list in place of this one.
var DefaultTestPatterns = []string{
	"*_test.go",
	"test_*.py", "*_test.py",
	"*.test.*", "*.spec.*",
	"tests/**", "test/**", "__tests__/**",
}

// TestFiles keeps new test files apart from existing ones that were changed
// or deleted. Only the second kind raises the risk.
type TestFiles struct {
	Added   []string
	Changed []string
}

// ClassifyTestFiles sorts the test files of a change. A nil patterns list
// means DefaultTestPatterns. A renamed file is an existing test that changed
// when its old path is a test, and a new test when only its new path is.
func ClassifyTestFiles(cs *ChangeSet, patterns []string) TestFiles {
	if patterns == nil {
		patterns = DefaultTestPatterns
	}
	isTest := func(p string) bool { return p != "" && matchAny(patterns, p, MatchAnywhere) }
	tf := TestFiles{Added: []string{}, Changed: []string{}}
	for _, c := range cs.Changes {
		switch {
		case c.Status == StatusRenamed && isTest(c.OldPath):
			tf.Changed = append(tf.Changed, c.OldPath)
		case !isTest(c.Path):
		case c.Status == StatusAdded || c.Status == StatusRenamed:
			tf.Added = append(tf.Added, c.Path)
		default:
			tf.Changed = append(tf.Changed, c.Path)
		}
	}
	sort.Strings(tf.Added)
	sort.Strings(tf.Changed)
	return tf
}

// UncoveredFiles lists the changed files that no claim names. A rename is
// covered when a claim names its new path or its old path, and is listed by
// its new path. A claim must name the path exactly as git does.
func UncoveredFiles(cs *ChangeSet, claims []api.Claim) []string {
	claimed := map[string]bool{}
	for _, c := range claims {
		for _, f := range c.Files {
			claimed[f] = true
		}
	}
	out := []string{}
	for _, c := range cs.Changes {
		if !claimed[c.Path] && !(c.OldPath != "" && claimed[c.OldPath]) {
			out = append(out, c.Path)
		}
	}
	sort.Strings(out)
	return out
}
