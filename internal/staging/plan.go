package staging

import (
	"fmt"
	"regexp"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
)

// MaxDependsOn is the most tickets an entry may wait for.
const MaxDependsOn = 64

var (
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	idRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// allowedTaskNames are the only tasks whose output can reach staging at all.
// Config.AllowedTasks narrows this set.
var allowedTaskNames = map[string]bool{api.TaskImplement: true, api.TaskFixCI: true, api.TaskAcceptance: true}

// checkEntry is the shape rule for one entry. It looks at the entry only and
// reads no repository. A blank, zero or unknown value is refused.
func checkEntry(e Entry) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrBadEntry}, a...)...)
	}
	switch {
	case !idRE.MatchString(e.ID):
		return bad("the id is not a plain id")
	case !commitRE.MatchString(e.Commit):
		return bad("the commit is not a full lower case hex id")
	case !commitRE.MatchString(e.Parent):
		return bad("the parent is not a full lower case hex id")
	case e.Commit == e.Parent:
		return bad("the commit is its own parent")
	case e.Ticket < 1 || e.Goal < 1 || e.Attempt < 1:
		return bad("ticket, goal and attempt must be 1 or more")
	case !allowedTaskNames[e.Task]:
		return bad("the task is not one that reaches staging")
	case len(e.DependsOn) > MaxDependsOn:
		return bad("too many dependencies")
	}
	for _, d := range e.DependsOn {
		if d < 1 {
			return bad("a dependency is not a ticket number")
		}
	}
	return nil
}

// Plan checks a list of entries, in pass order, and returns the ones to try,
// in the same order, with a removal for each one that is refused. It reads
// nothing and writes nothing.
//
// An entry is refused when its shape is wrong (see checkEntry) or when its id
// or its commit is the one of an entry already kept: only the first counts.
func Plan(list []Entry) (keep []Entry, removed []Removal) {
	ids, commits := map[string]bool{}, map[string]bool{}
	for _, e := range list {
		err := checkEntry(e)
		if err == nil && (ids[e.ID] || commits[e.Commit]) {
			err = fmt.Errorf("%w: the id or the commit is used by an earlier entry", ErrBadEntry)
		}
		if err != nil {
			removed = append(removed, Removal{EntryID: repo.Visible(e.ID, 64), Ticket: e.Ticket, Goal: e.Goal,
				Reason: RemovedBadEntry, Detail: repo.Visible(err.Error(), 200)})
			continue
		}
		ids[e.ID], commits[e.Commit] = true, true
		keep = append(keep, e)
	}
	return keep, removed
}

// Dependents returns the ids of the entries that go with the entry id when it
// is undone: every entry whose ticket waits, directly or through other
// tickets, for the ticket of id, in any goal. The list is in pass order and
// does not hold id. The search ends on a cycle. It covers only the entries in
// list. An id that is not in list is ErrUnknownEntry.
func Dependents(list []Entry, id string) ([]string, error) {
	root := -1
	for i, e := range list {
		if e.ID == id && id != "" {
			root = i
			break
		}
	}
	if root < 0 {
		return nil, fmt.Errorf("%w: %q", ErrUnknownEntry, repo.Visible(id, 64))
	}
	waiting := map[int][]int{} // ticket -> tickets that wait for it
	for _, e := range list {
		for _, d := range e.DependsOn {
			waiting[d] = append(waiting[d], e.Ticket)
		}
	}
	reached := map[int]bool{}
	queue := []int{list[root].Ticket}
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		for _, w := range waiting[t] {
			if !reached[w] {
				reached[w] = true
				queue = append(queue, w)
			}
		}
	}
	var out []string
	for i, e := range list {
		if i != root && reached[e.Ticket] {
			out = append(out, e.ID)
		}
	}
	return out, nil
}
