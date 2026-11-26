package verify

import (
	"reflect"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func testFactory() *api.Factory {
	f := &api.Factory{}
	f.Spec.AcceptancePath = "acceptance"
	f.Spec.ProtectedPaths = []string{".github/**"}
	return f
}

func taskNamed(name string, protected ...string) *api.Task {
	t := &api.Task{}
	t.Metadata.Name = name
	t.Spec.ProtectedPaths = protected
	return t
}

func modified(paths ...string) *ChangeSet {
	cs := &ChangeSet{}
	for _, p := range paths {
		cs.Changes = append(cs.Changes, Change{Path: p, Status: StatusModified})
	}
	return cs
}

func renamed(from, to string) Change {
	return Change{Path: to, OldPath: from, Status: StatusRenamed}
}

func TestCheckProtected(t *testing.T) {
	tests := []struct {
		name    string
		task    *api.Task
		cs      *ChangeSet
		touched []string
	}{
		{"clean change", taskNamed("implement"), modified("internal/a.go", "README.md"), nil},
		{"factory dir", taskNamed("implement"), modified(".factory/factory.yaml", "a.go"), []string{".factory/factory.yaml"}},
		{"deep factory dir", taskNamed("implement"), modified(".factory/tasks/x/y.yaml"), []string{".factory/tasks/x/y.yaml"}},
		{"acceptance path", taskNamed("implement"), modified("acceptance/40/check.go"), []string{"acceptance/40/check.go"}},
		{"configured path", taskNamed("implement"), modified(".github/workflows/ci.yml"), []string{".github/workflows/ci.yml"}},
		{"task path", taskNamed("implement", "migrations/**"), modified("migrations/1.sql", "b.go"), []string{"migrations/1.sql"}},
		{"case folded", taskNamed("implement"), modified(".FACTORY/x", "Acceptance/1/a"), []string{".FACTORY/x", "Acceptance/1/a"}},
		{"onboard may change .factory", taskNamed("onboard"), modified(".factory/factory.yaml", ".factory/tasks/a.yaml"), nil},
		{"onboard may change nothing else", taskNamed("onboard"), modified(".factory/factory.yaml", "main.go"), []string{"main.go"}},
		{"onboard still has configured paths", taskNamed("onboard"), modified(".github/ci.yml"), []string{".github/ci.yml"}},
		{"acceptance may change its path", taskNamed("acceptance"), modified("acceptance/40/a", "acceptance/manifest.yaml"), nil},
		{"acceptance may change nothing else", taskNamed("acceptance"), modified("acceptance/40/a", "src/x.go"), []string{"src/x.go"}},
		{"acceptance cannot touch factory dir", taskNamed("acceptance"), modified("acceptance/1/a", ".factory/x"), []string{".factory/x"}},
		{"rename out of protected", taskNamed("implement"),
			&ChangeSet{Changes: []Change{renamed(".github/ci.yml", "docs/ci.yml")}}, []string{".github/ci.yml"}},
		{"rename into protected", taskNamed("implement"),
			&ChangeSet{Changes: []Change{renamed("docs/ci.yml", ".github/ci.yml")}}, []string{".github/ci.yml"}},
		{"rename between protected", taskNamed("implement"),
			&ChangeSet{Changes: []Change{renamed(".github/a.yml", ".factory/a.yml")}}, []string{".factory/a.yml", ".github/a.yml"}},
		{"no change", taskNamed("implement"), &ChangeSet{}, nil},
		{"bare factory name", taskNamed("implement"), modified(".factory", "a.go"), []string{".factory"}},
		{"bare acceptance name", taskNamed("implement"), modified("acceptance"), []string{"acceptance"}},
		{"bare name of a configured path", taskNamed("implement"), modified(".github"), []string{".github"}},
		{"bare factory name, onboard", taskNamed("onboard"), modified(".factory"), []string{".factory"}},
		{"bare acceptance name stays touched even for acceptance (a link there redirects)", taskNamed("acceptance"), modified("acceptance"), []string{"acceptance"}},
		{"a link named like the factory dir", taskNamed("implement"),
			&ChangeSet{Changes: []Change{{Path: ".factory", Status: StatusAdded}}}, []string{".factory"}},
		{"suspicious: newline", taskNamed("implement"), modified("a\nb.go"), []string{"a<U+000A>b.go"}},
		{"suspicious: bidi override", taskNamed("implement"), modified("evil\u202egpj.exe"), []string{"evil<U+202E>gpj.exe"}},
		{"suspicious: zero width", taskNamed("implement"), modified("a\u200bb.go"), []string{"a<U+200B>b.go"}},
		{"suspicious: not UTF-8", taskNamed("implement"), modified("a\xffb.go"), []string{"a<0xFF>b.go"}},
		{"suspicious: nul", taskNamed("implement"), modified("a\x00b"), []string{"a<U+0000>b"}},
		{"suspicious and protected", taskNamed("implement"), modified(".factory/a\x1b"), []string{".factory/a<U+001B>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, err := CheckProtected(testFactory(), tt.task, tt.cs)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Touched) == 0 && len(tt.touched) == 0 {
				rep.Touched = nil
			}
			if !reflect.DeepEqual(rep.Touched, tt.touched) {
				t.Errorf("touched = %v, want %v", rep.Touched, tt.touched)
			}
			if rep.OK != (len(tt.touched) == 0) {
				t.Errorf("OK = %v with touched %v", rep.OK, tt.touched)
			}
		})
	}
}

func TestCheckProtected_CustomAcceptancePath(t *testing.T) {
	f := testFactory()
	f.Spec.AcceptancePath = "qa/accept"
	rep, err := CheckProtected(f, taskNamed("implement"), modified("qa/accept/1/x", "acceptance/y"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rep.Touched, []string{"qa/accept/1/x"}) {
		t.Errorf("touched = %v", rep.Touched)
	}
}

func TestCheckProtected_ReportValidates(t *testing.T) {
	// Touched is never nil, so the evidence JSON has [] and not null.
	rep, err := CheckProtected(testFactory(), taskNamed("implement"), modified("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Touched == nil || !rep.OK {
		t.Errorf("report = %+v", rep)
	}
}

func TestCheckProtected_BadPatternFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		f    func() *api.Factory
		task *api.Task
	}{
		{"factory file", func() *api.Factory { f := testFactory(); f.Spec.ProtectedPaths = []string{"ok/**", "[bad"}; return f }, taskNamed("implement")},
		{"task", testFactory, taskNamed("implement", "[bad")},
		{"acceptance path", func() *api.Factory { f := testFactory(); f.Spec.AcceptancePath = "[bad"; return f }, taskNamed("implement")},
		{"empty", func() *api.Factory { f := testFactory(); f.Spec.ProtectedPaths = []string{""}; return f }, taskNamed("implement")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CheckProtected(tt.f(), tt.task, modified("a.go"))
			if err == nil {
				t.Fatal("a pattern that cannot be parsed must fail verification")
			}
			if tt.name != "empty" && !strings.Contains(err.Error(), "[bad") {
				t.Errorf("the error must name the pattern: %v", err)
			}
		})
	}
}
