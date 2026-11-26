package verify

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		// plain and single star
		{"go.mod", "go.mod", true},
		{"go.mod", "sub/go.mod", false},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/x/a.md", false},
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
		{"[ab]x", "bx", true},
		// double star at the end needs at least one segment
		{"docs/**", "docs/a", true},
		{"docs/**", "docs/a/b/c.md", true},
		{"docs/**", "docs", false},
		{"docs/**", "docsx/a", false},
		{".factory/**", ".factory/tasks/x.yaml", true},
		{"**", "a", true},
		{"**", "a/b", true},
		// double star at the start and in the middle
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/main.go", true},
		{"**/*.go", "a/b/main.rs", false},
		{"a/**/z", "a/z", true},
		{"a/**/z", "a/b/z", true},
		{"a/**/z", "a/b/c/z", true},
		{"a/**/z", "a/b/c/y", false},
		{"a/**/b/**", "a/x/b/y", true},
		{"a/**/b/**", "a/x/c/y", false},
		// a star inside a segment never crosses a slash
		{"a*b", "a/b", false},
		{"a**b", "a/b", false},
		{"a**b", "axxb", true},
		// the whole path must match
		{"a/b", "a/b/c", false},
		{"a/b/c", "a/b", false},
		// empty and bad input never match
		{"", "a", false},
		{"a", "", false},
		{"[", "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" vs "+tt.path, func(t *testing.T) {
			if got := Match(tt.pattern, tt.path); got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchFold(t *testing.T) {
	if !MatchFold(".factory/**", ".FACTORY/x") {
		t.Error("MatchFold must ignore case")
	}
	if Match(".factory/**", ".FACTORY/x") {
		t.Error("Match must be case sensitive")
	}
}

func TestMatchAnywhere(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"*_test.go", "a/b/x_test.go", true},
		{"*_test.go", "x_test.go", true},
		{"*_test.go", "x_test.gox", false},
		{"tests/**", "tests/a.py", true},
		{"tests/**", "svc/tests/a.py", true},
		{"tests/**", "svc/mytests/a.py", false},
		{"*.test.*", "web/src/a.test.ts", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" vs "+tt.path, func(t *testing.T) {
			if got := MatchAnywhere(tt.pattern, tt.path); got != tt.want {
				t.Errorf("MatchAnywhere(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}
