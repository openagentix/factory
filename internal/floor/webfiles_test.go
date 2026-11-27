package floor

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// The web app reads two committed files that come from this package: the table
// of fixed phrases and a set of sample snapshots. Keeping them here keeps one
// source of truth. A test fails when a file differs from what the code makes.
// Run with -update-web to write them again.
var updateWeb = flag.Bool("update-web", false, "rewrite the files the web app reads")

const (
	webPhrases = "../../web/src/lib/phrases.json"
	webFixDir  = "../../web/src/fixtures/"
)

func marshalIndent(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checkWebFile(t *testing.T, path string, want []byte) {
	t.Helper()
	if *updateWeb {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run go test ./internal/floor -run WebFiles -update-web)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from what the code makes (run go test ./internal/floor -run WebFiles -update-web)", path)
	}
}

func TestWebFilesPhrases(t *testing.T) {
	checkWebFile(t, webPhrases, marshalIndent(t, Phrases()))
}

// demoInputs mirrors the story of the approved mockup: four goals, seven
// stations, a piece in every state.
func demoInputs() Inputs {
	in := healthy()
	in.Mode = ModeDemo
	in.DemoStory = "booking_tour"
	in.Repository = "demo/booking"
	in.Machine = "laptop-a"
	in.ScriptedStations = []StationNo{5}
	in.PartlyRealStations = []StationNo{2}
	in.Sources[SourceTrajectory] = SourceNotBuilt
	in.HostTime = Some(at(-20 * time.Second))
	in.HostReadAt = Some(at(-20 * time.Second))
	in.Attention = Opt[[]AttentionIn]{}
	line := func(id int, text string, st LineState, note ReasonCode, tk int) LineIn {
		return LineIn{ID: id, Text: text, State: st, NoteCode: note, NoteTicket: tk}
	}
	g := func(n int, title string, lane Lane, approved bool, manifest bool, lines []LineIn) GoalIn {
		return GoalIn{Number: n, Title: title, Open: true, Approved: Some(approved), Lane: lane,
			ManifestOnStaging: Some(manifest), Lines: Some(lines), LinesAsOf: Some(int64(7)),
			PlanLines: Some([]string{"Anyone can see where every piece of work is."}), BodyEditedAt: Some(at(-30 * time.Hour)),
			PlanSince: Some(at(-95 * time.Minute))}
	}
	g41 := g(41, "Combine passing changes in a holding area", LaneStandard, true, true, []LineIn{
		line(1, "A change that passes every check goes into the holding area.", LineWorking, "", 0),
		line(2, "A change that fails a check never goes in.", LineWorking, "", 0),
		line(3, "Two changes that touch the same place are combined in order.", LineWorking, "", 0),
		line(4, "The holding area can be rebuilt after a crash.", LineCouldNotConfirm, Unknown, 52),
	})
	g42 := g(42, "Two teammates' machines share one queue", LaneStandard, true, true, []LineIn{
		line(1, "Each machine takes a different ticket.", LineWorking, "", 0),
		line(2, "The first machine to claim a ticket keeps it.", LineNotWorking, CheckFailed, 57),
		line(3, "A machine that goes away frees its ticket.", LineNotWorking, QuestionOpen, 58),
		line(4, "It still works when two clocks differ.", LineCouldNotConfirm, "", 0),
	})
	g43 := g(43, "A person approves a goal by listing its lines", LaneQuick, true, false, []LineIn{
		line(1, "A goal with one to three plain lines can be approved.", LineNotWorking, Queued, 61),
		line(2, "The factory repeats the lines back, word for word.", LineNotWorking, Queued, 62),
	})
	g44 := g(44, "Watch the factory on one screen", LaneQuick, false, false, []LineIn{
		line(1, "The line shows where every piece of work is.", LineNotWorking, "", 0),
		line(2, "A stuck piece says why, and since when.", LineNotWorking, "", 0),
		line(3, "What needs a person is listed in plain words.", LineNotWorking, "", 0),
	})
	g44.Course = CourseNotStarted
	g40 := goal(40)
	g40.Open = false
	in.Goals = Some([]GoalIn{g41, g42, g43, g44})

	tk := func(n, goalNo int, title string, ready bool) TicketIn {
		t := ticket(n)
		t.Goal = goalNo
		t.Title = title
		t.Ready = ready
		return t
	}
	t58 := tk(58, 42, "Free a ticket when a machine goes away", true)
	t58.NeedsHuman = true
	t58.Rulings = []string{"Kept the last good state on disk, not in memory."}
	t52 := tk(52, 41, "Rebuild the holding area after a crash", true)
	t52.Rulings = []string{"Kept the last good state on disk, not in memory. Cost if wrong: one slower restart."}
	t52.Unconfirmed = []string{"No check covers a crash during the rebuild itself."}
	in.Tickets = Some([]TicketIn{
		tk(62, 43, "Repeat the lines back", true), tk(61, 43, "Read the lines strictly", true),
		t58, t52,
		tk(60, 42, "Two machines claim in the same second", true),
		tk(57, 42, "Lowest claim wins", true),
		tk(55, 42, "Only trusted people can start work", true),
		tk(56, 42, "Say which machine holds a ticket", true),
		tk(49, 41, "Take out a change that breaks a line", true),
		tk(47, 41, "Combine changes in order", true),
		tk(46, 41, "Refuse a change that fails a check", true),
	})
	in.Questions = Some([]QuestionIn{{Ticket: 58, Goal: 42, CommentID: 9001, Since: Some(at(-52 * time.Minute)),
		Text: "A machine holds a ticket and then goes away. After how long may another machine take it: 2 hours, or sooner?"}})

	r := func(id string, tn, goalNo, attempt int, ph api.Phase, since time.Duration) RunIn {
		x := run(tn, attempt, ph)
		x.ID = id
		x.Goal = goalNo
		x.Machine = "laptop-a"
		x.PhaseSince = Some(at(since))
		x.StartedAt = Some(at(since - 10*time.Minute))
		x.CreatedAt = Some(at(since - 12*time.Minute))
		x.Deadline = time.Hour
		return x
	}
	failed1 := r("r_52a", 57, 42, 1, api.PhaseFailed, -95*time.Minute)
	failed1.EndReason = CheckFailed
	failed2 := r("r_57b", 57, 42, 2, api.PhaseFailed, -40*time.Minute)
	failed2.EndReason = CheckFailed
	ok55 := r("r_55a", 55, 42, 1, api.PhaseSucceeded, -30*time.Minute)
	ok56 := r("r_56a", 56, 42, 1, api.PhaseSucceeded, -12*time.Minute)
	rev56 := r("r_56v", 56, 42, 1, api.PhaseWorking, -4*time.Minute)
	rev56.Task, rev56.SubjectRun, rev56.SubjectAttempt = "review", "r_56a", 1
	rev56.Machine = "laptop-a"
	runs := []RunIn{r("r_52w", 52, 41, 1, api.PhaseWorking, -6*time.Minute), failed1, failed2, ok55, ok56, rev56}
	for _, n := range []int{49, 47, 46} {
		runs = append(runs, r("r_"+itoa(n)+"a", n, 41, 1, api.PhaseSucceeded, -time.Duration(30+n)*time.Minute))
	}
	in.Runs = Some(runs)
	in.Claims = Some([]ClaimIn{{Ticket: 60, Machine: "laptop-b", Since: Some(at(-25 * time.Minute)), Expires: Some(at(95 * time.Minute))}})
	gates := []GateIn{
		{Ticket: 55, Status: GateConsequenceOpen, Risk: RiskSecurity, Run: "r_55a", Attempt: 1, Commit: "c0ffee55c0ffee55c0ffee55c0ffee55c0ffee55", Machine: "laptop-a", Since: Some(at(-18 * time.Minute))},
		{Ticket: 56, Status: GateWaitingReview, Run: "r_56a", Attempt: 1},
	}
	for _, n := range []int{49, 47, 46} {
		gates = append(gates, GateIn{Ticket: n, Status: GatePassed, Run: "r_" + itoa(n) + "a", Attempt: 1})
	}
	in.Gate = Some(gates)
	in.Holding = Some(HoldingIn{Exists: true, StagingSeq: 7, Commit: "ab12cd34", Published: true, Mine: true, PullRequest: 0,
		Applied: []EntryIn{{Ticket: 49, Goal: 41, At: Some(at(-31 * time.Minute))}, {Ticket: 47, Goal: 41, At: Some(at(-64 * time.Minute))},
			{Ticket: 46, Goal: 41, At: Some(at(-88 * time.Minute))}}})
	var acc []AcceptedIn
	for _, n := range []int{44, 42, 41, 39} {
		acc = append(acc, AcceptedIn{Ticket: n, Goal: 40, At: Some(at(-2 * time.Hour))})
	}
	in.Accepted = Some(acc)
	in.Journal = Some([]JournalIn{
		{Seq: 11, Time: Some(at(-31 * time.Minute)), Kind: "entered_holding_area", Ticket: 49},
		{Seq: 10, Time: Some(at(-40 * time.Minute)), Kind: "check_failed", Ticket: 57},
		{Seq: 9, Time: Some(at(-52 * time.Minute)), Kind: "question_asked", Ticket: 58},
		{Seq: 8, Time: Some(at(-70 * time.Minute)), Kind: "taken_out", Ticket: 51},
		{Seq: 7, Time: Some(at(-100 * time.Minute)), Kind: "could_not_confirm", Goal: 41},
		{Seq: 6, Time: Some(at(-3 * time.Hour)), Kind: "accepted", Goal: 40},
		{Seq: 5, Time: Some(at(-4 * time.Hour)), Kind: "plan_approved", Goal: 43},
	})
	in.FirstAttempts = Some([]FirstAttemptIn{})
	return in
}

// allUnknown is empty Inputs: nothing is known.
func allUnknown() Inputs { return Inputs{} }

// notBuilt is live mode where every source says it is not built.
func notBuilt() Inputs {
	src := map[SourceKey]SourceState{}
	for _, k := range AllSourceKeys() {
		src[k] = SourceNotBuilt
	}
	return Inputs{Now: t0, Mode: ModeLive, Repository: "acme/booking", Machine: me, Sources: src}
}

// pausedLine is a quiet live project, with one ticket ready and the line paused.
func pausedLine() Inputs {
	in := withTickets(healthy(), ticket(7))
	in.Paused = Some(true)
	return in
}

func TestWebFilesFixtures(t *testing.T) {
	cases := map[string]Inputs{
		"floor.demo.json": demoInputs(), "floor.unknown.json": allUnknown(),
		"floor.notbuilt.json": notBuilt(), "floor.paused.json": pausedLine(),
	}
	for name, in := range cases {
		checkWebFile(t, webFixDir+name, marshalIndent(t, Project(in)))
	}
}
