package staging

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func hex40(c byte) string { return strings.Repeat(string(c), 40) }

func goodEntry(id string, ticket int, c byte) Entry {
	return Entry{ID: id, Ticket: ticket, Goal: 1, Attempt: 1, Task: "implement", Commit: hex40(c), Parent: hex40('f'), Title: "t"}
}

// Attack 4: an empty, short, upper case or ref shaped commit or parent is
// refused, and a bad id, task or ticket too.
func TestPlanRefusesMalformedEntries(t *testing.T) {
	bad := map[string]func(*Entry){
		"empty commit":       func(e *Entry) { e.Commit = "" },
		"abbreviated commit": func(e *Entry) { e.Commit = "abc123" },
		"ref name commit":    func(e *Entry) { e.Commit = "factory-staging" },
		"HEAD as commit":     func(e *Entry) { e.Commit = "HEAD" },
		"upper case commit":  func(e *Entry) { e.Commit = strings.ToUpper(hex40('a')) },
		"long commit":        func(e *Entry) { e.Commit = hex40('a') + "0" },
		"empty parent":       func(e *Entry) { e.Parent = "" },
		"abbreviated parent": func(e *Entry) { e.Parent = hex40('b')[:12] },
		"parent is revision": func(e *Entry) { e.Parent = "HEAD~1" },
		"empty id":           func(e *Entry) { e.ID = "" },
		"id with space":      func(e *Entry) { e.ID = "a b" },
		"id with newline":    func(e *Entry) { e.ID = "a\nb" },
		"id too long":        func(e *Entry) { e.ID = strings.Repeat("a", 65) },
		"zero ticket":        func(e *Entry) { e.Ticket = 0 },
		"negative goal":      func(e *Entry) { e.Goal = -1 },
		"zero attempt":       func(e *Entry) { e.Attempt = 0 },
		"unknown task":       func(e *Entry) { e.Task = "onboard" },
		"empty task":         func(e *Entry) { e.Task = "" },
		"bad dependency":     func(e *Entry) { e.DependsOn = []int{0} },
		"too many deps":      func(e *Entry) { e.DependsOn = make([]int, MaxDependsOn+1) },
		"commit is parent":   func(e *Entry) { e.Parent = e.Commit },
	}
	for name, edit := range bad {
		e := goodEntry("r1", 1, 'a')
		edit(&e)
		keep, removed := Plan([]Entry{e})
		if len(keep) != 0 || len(removed) != 1 || removed[0].Reason != RemovedBadEntry {
			t.Errorf("%s: keep=%v removed=%v", name, keep, removed)
		}
		if err := checkEntry(e); !errors.Is(err, ErrBadEntry) {
			t.Errorf("%s: checkEntry = %v", name, err)
		}
	}
	if keep, removed := Plan([]Entry{goodEntry("r1", 1, 'a')}); len(keep) != 1 || len(removed) != 0 {
		t.Errorf("a good entry: keep=%v removed=%v", keep, removed)
	}
}

// Attack 5: the same id, or the same commit under another id, counts once.
func TestPlanKeepsOnlyTheFirstOfADuplicate(t *testing.T) {
	a := goodEntry("r1", 1, 'a')
	sameID := goodEntry("r1", 2, 'b')
	sameCommit := goodEntry("r3", 3, 'a')
	c := goodEntry("r4", 4, 'c')
	keep, removed := Plan([]Entry{a, sameID, sameCommit, c})
	if len(keep) != 2 || keep[0].ID != "r1" || keep[1].ID != "r4" {
		t.Fatalf("keep = %v", keep)
	}
	if len(removed) != 2 || removed[0].Reason != RemovedBadEntry || removed[1].Reason != RemovedBadEntry {
		t.Fatalf("removed = %v", removed)
	}
	if removed[0].Ticket != 2 || removed[1].Ticket != 3 {
		t.Errorf("removals name the wrong entries: %v", removed)
	}
}

func chain() []Entry {
	e1, e2, e3, e4 := goodEntry("r1", 1, '1'), goodEntry("r2", 2, '2'), goodEntry("r3", 3, '3'), goodEntry("r4", 4, '4')
	e2.DependsOn = []int{1}
	e3.DependsOn = []int{2}
	e3.Goal = 2 // another goal
	return []Entry{e1, e2, e3, e4}
}

// Attack 27: dependents are found across goals and transitively. An unknown
// id is an error.
func TestDependentsAcrossGoals(t *testing.T) {
	got, err := Dependents(chain(), "r1")
	if err != nil || !reflect.DeepEqual(got, []string{"r2", "r3"}) {
		t.Fatalf("Dependents(r1) = %v, %v", got, err)
	}
	if got, err := Dependents(chain(), "r4"); err != nil || len(got) != 0 {
		t.Fatalf("Dependents(r4) = %v, %v", got, err)
	}
	if _, err := Dependents(chain(), "nope"); !errors.Is(err, ErrUnknownEntry) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := Dependents(chain(), ""); !errors.Is(err, ErrUnknownEntry) {
		t.Fatalf("blank id: %v", err)
	}
}

// Attack 27: a cycle ends, and the closure holds each entry once.
func TestDependentsCycleTerminates(t *testing.T) {
	e1, e2, e3 := goodEntry("r1", 1, '1'), goodEntry("r2", 2, '2'), goodEntry("r3", 3, '3')
	e1.DependsOn = []int{2}
	e2.DependsOn = []int{1}
	e3.DependsOn = []int{3, 1}
	got, err := Dependents([]Entry{e1, e2, e3}, "r1")
	if err != nil || !reflect.DeepEqual(got, []string{"r2", "r3"}) {
		t.Fatalf("Dependents = %v, %v", got, err)
	}
}

// Attack 28: the closure covers only the entries in the list. A dependent
// that was taken out earlier is not in the list and is not an error.
func TestDependentsOnlyLiveEntries(t *testing.T) {
	list := chain()
	live := []Entry{list[0], list[2], list[3]} // r2 is gone
	got, err := Dependents(live, "r1")
	if err != nil || len(got) != 0 {
		t.Fatalf("Dependents = %v, %v", got, err)
	}
}
