package acceptance

import (
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func TestEvidenceEntries(t *testing.T) {
	results := []Result{
		{ID: 1, State: api.StateWorking, Check: "go test ./a"},
		{ID: 2, State: api.StateNotWorking, Check: "go test ./b", TimedOut: true},
		{ID: 3, State: api.StateCouldNotConfirm, Note: NoteNoCheck},
		{ID: 4, State: api.StateCouldNotConfirm, Check: "go test ./d", Note: NoteTotalLimit},
		{ID: 5, State: api.StateWorking, Check: "go test ./e"},
	}
	before := map[int]api.AcceptanceState{1: api.StateNotWorking, 2: api.StateWorking, 3: api.StateCouldNotConfirm}
	got, err := EvidenceEntries(&Report{kind: KindJudged, Results: results}, before)
	if err != nil {
		t.Fatal(err)
	}
	want := []api.AcceptanceEntry{
		{ID: 1, State: api.StateWorking, Before: api.StateNotWorking, Check: "go test ./a"},
		{ID: 2, State: api.StateNotWorking, Before: api.StateWorking, Check: "go test ./b"},
		{ID: 3, State: api.StateCouldNotConfirm, Before: api.StateCouldNotConfirm},
		{ID: 4, State: api.StateCouldNotConfirm},
		{ID: 5, State: api.StateWorking, Check: "go test ./e"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestEvidenceEntries_PassTheRecordsOwnValidation(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "a", "true"), crit(2, "b", "false"), crit(3, "c", ""))
	c := tr.commit("x")
	tr.write("more.txt", "x")
	judged := tr.commit("change")
	rep, err := Run(t.Context(), tr.open(), Request{Judged: judged, Source: c, Goal: 40}, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	// Build on a record that is valid apart from the acceptance entries.
	ev := validBundle(t)
	ev.Acceptance, err = EvidenceEntries(rep, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("the bundle is not valid: %v", err)
	}
	if got := ev.Acceptance[2].Check; got != "" {
		t.Errorf("could not confirm must have no check: %q", got)
	}
}

func validBundle(t *testing.T) *api.Evidence {
	t.Helper()
	ev := &api.Evidence{
		SchemaVersion: api.SchemaVersion, Run: "r_1", Task: api.TaskImplement, Goal: 40, Ticket: 42, Attempt: 1,
		Mode: api.ModeUnattended, Method: "builtin", MethodUsed: "builtin",
		Trigger: api.TriggerRef{Kind: "issueLabeled", Ref: "#42"},
		Worker:  api.HarnessInfo{Name: "claude-code", Version: "1"},
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("base bundle: %v", err)
	}
	return ev
}
