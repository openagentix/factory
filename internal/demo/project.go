package demo

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/harness"
)

// Everything in this file is fixed text in the binary. No title, body or
// comment of any issue is ever put into a script, a file or a command line
// (record 0004, "Demo marking"; the check scripts run as the same user).

const (
	repoName    = "demo/booking"
	machineName = "demo-laptop"
	storyKey    = "booking_tour"
	maxAttempts = 3
	goalMove    = 1
	goalCancel  = 2
	// factoryLogin is the user the propose token acts as on the fake host.
	factoryLogin = "factory-bot"
)

// seedFiles is the made-up project. The files are plain text.
var seedFiles = map[string]string{
	"booking/README.txt": "The booking demo project. It is made up.\n",
	"booking/booking.sh": "echo booking\n",
	"booking/rules.txt":  "max_days=14\n",
}

// taskChecks are the project's own checks. They are plain sh and need no
// toolchain and no network.
func taskChecks() []api.Check {
	return []api.Check{
		{Name: "syntax", Run: `for f in booking/*.sh; do sh -n "$f" || exit 1; done`},
		{Name: "rules", Run: `test -f booking/rules.txt`},
	}
}

// goalBody is the plan of goal 1 as the person wrote it: three lines.
const goalMoveBody = "Customers can move a booking.\n\n- A booking can be moved up to 14 days ahead.\n- A moved booking gets a confirmation.\n- A moved booking never clashes with another booking.\n"

const goalCancelBody = "Customers can cancel a booking.\n\n- A booking can be cancelled.\n- A cancelled booking is refunded.\n"

// manifestGoal1 is written by the acceptance step. The third line has no
// command, so it can not be confirmed.
func manifestGoal1() []byte {
	m := api.Manifest{SchemaVersion: api.SchemaVersion, Goal: goalMove, Criteria: []api.ManifestCriterion{
		{ID: 1, Text: "A booking can be moved up to 14 days ahead.", Run: `test -f booking/move.sh && grep -q '^max_days=14$' booking/rules.txt`},
		{ID: 2, Text: "A moved booking gets a confirmation.", Run: `test -f booking/confirm.sh && sh booking/confirm.sh | grep -q confirmed`},
		{ID: 3, Text: "A moved booking never clashes with another booking.", Reason: "no check could be written"},
	}}
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

// manifestGoal2 is written by the acceptance step of goal 2. Both lines have
// a command, so both can be confirmed.
func manifestGoal2() []byte {
	m := api.Manifest{SchemaVersion: api.SchemaVersion, Goal: goalCancel, Criteria: []api.ManifestCriterion{
		{ID: 1, Text: "A booking can be cancelled.", Run: `test -f booking/cancel.sh && sh booking/cancel.sh | grep -q cancelled`},
		{ID: 2, Text: "A cancelled booking is refunded.", Run: `test -f booking/refund.sh && sh booking/refund.sh | grep -q refunded`},
	}}
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

func okResult(summary string, claims ...api.Claim) *api.Result {
	return &api.Result{SchemaVersion: 1, Status: api.StatusSucceeded, Summary: summary, Claims: claims, Harness: api.HarnessInfo{Name: "stub"}}
}

// ticketSpec is one ticket of goal 1, with the script the stub follows for
// each try.
type ticketSpec struct {
	key     string
	title   string
	body    string
	served  []int
	depends []int // indexes in the ticket list of its goal
	goal    int
	scripts []harness.StubScript
}

func claimOf(text string, files ...string) api.Claim { return api.Claim{Text: text, Files: files} }

func tickets() []ticketSpec {
	l := goal1Tickets()
	for i := range l {
		l[i].goal = goalMove
	}
	return l
}

// ticketsOf lists the tickets of a goal, or nil for a goal with none.
func ticketsOf(goal int) []ticketSpec {
	switch goal {
	case goalMove:
		return tickets()
	case goalCancel:
		return tickets2()
	}
	return nil
}

// tickets2 are the two tickets of goal 2. Both pass at the first try.
func tickets2() []ticketSpec {
	return []ticketSpec{
		{key: "cancel", goal: goalCancel, title: "Cancel a booking", body: "Add the cancel step.", served: []int{1},
			scripts: []harness.StubScript{{
				Files:  []harness.StubFile{{Path: "booking/cancel.sh", Content: "echo cancelled\n"}},
				Result: okResult("Added the cancel step.", claimOf("Added the cancel step.", "booking/cancel.sh")),
			}}},
		{key: "refund", goal: goalCancel, title: "Refund a cancelled booking", body: "Add the refund step.", served: []int{2},
			scripts: []harness.StubScript{{
				Files:  []harness.StubFile{{Path: "booking/refund.sh", Content: "echo refunded\n"}},
				Result: okResult("Added the refund step.", claimOf("Added the refund step.", "booking/refund.sh")),
			}}},
	}
}

func goal1Tickets() []ticketSpec {
	return []ticketSpec{
		{key: "move", title: "Move a booking to a new date", body: "Add the move step.", served: []int{1},
			scripts: []harness.StubScript{{
				Files:  []harness.StubFile{{Path: "booking/move.sh", Content: "echo moved\n"}},
				Result: okResult("Added the move step.", claimOf("Added the move step.", "booking/move.sh")),
			}}},
		{key: "confirm", title: "Send a confirmation after a move", body: "Add the confirmation.", served: []int{2}, depends: []int{0},
			scripts: []harness.StubScript{
				{ // try 1: a syntax error, so the project's own checks fail
					Files:  []harness.StubFile{{Path: "booking/confirm.sh", Content: "if then fi (\n"}},
					Result: okResult("Added the confirmation.", claimOf("Added the confirmation.", "booking/confirm.sh")),
				},
				{ // try 2: fixed
					Files:  []harness.StubFile{{Path: "booking/confirm.sh", Content: "echo confirmed\n"}},
					Result: okResult("Fixed the confirmation.", claimOf("Fixed the confirmation.", "booking/confirm.sh")),
				},
			}},
		{key: "ask", title: "Decide what happens when the day is full", body: "Handle a full day.", served: []int{3},
			scripts: []harness.StubScript{{
				Result: &api.Result{SchemaVersion: 1, Status: api.StatusNeedsHuman, Summary: "I need an answer.",
					Questions: []string{"Should a booking be movable onto a day that is already full?"}, Harness: api.HarnessInfo{Name: "stub"}},
			}, { // try 2, after the answer: the agent goes on and passes
				Files:  []harness.StubFile{{Path: "booking/full-day.sh", Content: "echo full day\n"}},
				Result: okResult("Handled a full day.", claimOf("Handled a full day.", "booking/full-day.sh")),
			}}},
		{key: "staff", title: "Let staff approve moves", body: "Add a staff role.", served: []int{1},
			scripts: []harness.StubScript{{
				Files:  []harness.StubFile{{Path: "auth/roles.txt", Content: "staff=approve_moves\n"}},
				Result: okResult("Added the staff role.", claimOf("Added the staff role.", "auth/roles.txt")),
			}, { // try 2, after a no: a change that is not risky
				Files:  []harness.StubFile{{Path: "booking/staff.sh", Content: "echo staff\n"}},
				Result: okResult("Added the staff step.", claimOf("Added the staff step.", "booking/staff.sh")),
			}}},
		{key: "ahead", title: "Let a move go further ahead", body: "Raise the limit.", served: []int{1},
			scripts: []harness.StubScript{{
				Files:  []harness.StubFile{{Path: "booking/rules.txt", Content: "max_days=365\n"}},
				Result: okResult("Raised the limit.", claimOf("Raised the limit.", "booking/rules.txt")),
			}}},
	}
}

func acceptanceScript() harness.StubScript { return acceptanceScriptFor(goalMove) }

func acceptanceScriptFor(goal int) harness.StubScript {
	m, path := manifestGoal1(), "acceptance/1/manifest.json"
	if goal == goalCancel {
		m, path = manifestGoal2(), "acceptance/2/manifest.json"
	}
	return harness.StubScript{
		Files:  []harness.StubFile{{Path: path, Content: string(m)}},
		Result: okResult("Wrote the checks.", claimOf("Wrote the checks.", path)),
	}
}

func factoryFile() *api.Factory {
	f := &api.Factory{Spec: api.FactorySpec{
		Repository: repoName,
		RiskPaths:  api.RiskPaths{Security: []string{"auth/**"}},
	}}
	f.ApplyDefaults()
	return f
}

func taskFor(name string) *api.Task {
	return &api.Task{APIVersion: api.APIVersion, Kind: api.KindTask, Metadata: api.ObjectMeta{Name: name}}
}

// writeSeed writes the project files into dir and returns the number written.
func writeSeed(dir string) error {
	for p, c := range seedFiles {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(c), 0o600); err != nil {
			return err
		}
	}
	return nil
}
