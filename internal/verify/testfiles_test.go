package verify

import (
	"reflect"
	"strings"
	"testing"
)

func TestClassifyTestFiles(t *testing.T) {
	cs := &ChangeSet{Changes: []Change{
		{Path: "a/new_test.go", Status: StatusAdded},
		{Path: "a/old_test.go", Status: StatusModified},
		{Path: "a/gone_test.go", Status: StatusDeleted},
		{Path: "a/code.go", Status: StatusModified},
		{Path: "py/test_x.py", Status: StatusAdded},
		{Path: "py/y_test.py", Status: StatusModified},
		{Path: "web/a.test.ts", Status: StatusAdded},
		{Path: "web/b.spec.js", Status: StatusModified},
		{Path: "tests/fixtures/data.json", Status: StatusModified},
		{Path: "svc/test/util.rb", Status: StatusAdded},
		{Path: "svc/__tests__/c.js", Status: StatusDeleted},
		{Path: "docs/latest.md", Status: StatusModified},
		// moving an existing test is a change to an existing test
		{Path: "b/moved_test.go", OldPath: "a/was_test.go", Status: StatusRenamed},
		// a non-test renamed into a test name is a new test file
		{Path: "c/fresh_test.go", OldPath: "c/fresh.go", Status: StatusRenamed},
		// a test renamed to a non-test name is still a change to an existing test
		{Path: "c/helper.go", OldPath: "c/helper_test.go", Status: StatusRenamed},
	}}
	got, err := ClassifyTestFiles(cs, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantAdded := []string{"a/new_test.go", "c/fresh_test.go", "py/test_x.py", "svc/test/util.rb", "web/a.test.ts"}
	wantChanged := []string{"a/gone_test.go", "a/old_test.go", "a/was_test.go", "c/helper_test.go", "py/y_test.py", "svc/__tests__/c.js", "tests/fixtures/data.json", "web/b.spec.js"}
	if !reflect.DeepEqual(got.Added, wantAdded) {
		t.Errorf("added = %v\nwant %v", got.Added, wantAdded)
	}
	if !reflect.DeepEqual(got.Changed, wantChanged) {
		t.Errorf("changed = %v\nwant %v", got.Changed, wantChanged)
	}
}

func TestClassifyTestFiles_CallerPatternsReplaceDefaults(t *testing.T) {
	cs := &ChangeSet{Changes: []Change{
		{Path: "x_test.go", Status: StatusModified},
		{Path: "check/one.sh", Status: StatusModified},
	}}
	got, _ := ClassifyTestFiles(cs, []string{"check/**"})
	if !reflect.DeepEqual(got.Changed, []string{"check/one.sh"}) || len(got.Added) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestClassifyTestFiles_EmptyListsAreNotNil(t *testing.T) {
	got, _ := ClassifyTestFiles(&ChangeSet{}, nil)
	if got.Added == nil || got.Changed == nil {
		t.Errorf("lists must be non-nil: %+v", got)
	}
}

func TestUncoveredFiles(t *testing.T) {
	cs := &ChangeSet{Changes: []Change{
		{Path: "a.go", Status: StatusModified},
		{Path: "b.go", Status: StatusAdded},
		{Path: "c.go", Status: StatusDeleted},
		{Path: "d.go", OldPath: "e.go", Status: StatusRenamed},
	}}
	tests := []struct {
		name   string
		claims []claimLike
		want   []string
	}{
		{"no claims", nil, []string{"a.go", "b.go", "c.go", "d.go"}},
		{"some", []claimLike{{"x", []string{"a.go", "c.go"}}}, []string{"b.go", "d.go"}},
		{"two claims", []claimLike{{"x", []string{"a.go", "b.go"}}, {"y", []string{"c.go", "e.go", "zzz.go"}}}, []string{}},
		{"claiming the old path of a rename covers it", []claimLike{{"x", []string{"e.go"}}}, []string{"a.go", "b.go", "c.go"}},
		{"claimed name must match exactly", []claimLike{{"x", []string{"./a.go", "A.go", "dir/a.go"}}}, []string{"a.go", "b.go", "c.go", "d.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UncoveredFiles(cs, toClaims(tt.claims))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyTestFiles_BadPatternLists(t *testing.T) {
	cs := &ChangeSet{Changes: []Change{{Path: "a_test.go", Status: StatusModified}}}
	tests := []struct {
		name     string
		patterns []string
		wantErr  string
	}{
		{"empty list", []string{}, "empty"},
		{"bad pattern", []string{"*_test.go", "[bad"}, "[bad"},
		{"empty pattern", []string{""}, "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ClassifyTestFiles(cs, tt.patterns)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
	if _, err := ClassifyTestFiles(cs, nil); err != nil {
		t.Errorf("nil means the defaults: %v", err)
	}
}
