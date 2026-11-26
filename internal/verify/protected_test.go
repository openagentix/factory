package verify

import (
	"reflect"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := CheckProtected(testFactory(), tt.task, tt.cs)
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
	rep := CheckProtected(f, taskNamed("implement"), modified("qa/accept/1/x", "acceptance/y"))
	if !reflect.DeepEqual(rep.Touched, []string{"qa/accept/1/x"}) {
		t.Errorf("touched = %v", rep.Touched)
	}
}

func TestCheckProtected_ReportValidates(t *testing.T) {
	// Touched is never nil, so the evidence JSON has [] and not null.
	rep := CheckProtected(testFactory(), taskNamed("implement"), modified("a.go"))
	if rep.Touched == nil || !rep.OK {
		t.Errorf("report = %+v", rep)
	}
}
