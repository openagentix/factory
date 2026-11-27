package floor

import (
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func withQuestion(in Inputs, ticketNo int, comment int64, text string) Inputs {
	in.Questions = Some([]QuestionIn{{Ticket: ticketNo, Goal: 40, CommentID: comment, Since: Some(at(-30 * time.Minute)), Text: text}})
	return in
}

func TestQuestionCall(t *testing.T) {
	in := withQuestion(withTickets(healthy(), ticket(58)), 58, 901, "Keep the old endpoint?")
	r := run(58, 1, api.PhaseNeedsHuman)
	f := Project(withRuns(in, r))
	c := callOfKind(f, CallQuestion)
	if c == nil || c.ID != "q-58-901" || c.Who != WhoAnyoneTrusted || len(c.Actions) != 1 || c.Actions[0] != ActionAnswer {
		t.Fatalf("call %+v", c)
	}
	if c.Question == nil || c.Question.Text != "Keep the old endpoint?" || c.Ticket == nil || *c.Ticket != 58 || *c.Goal != 40 {
		t.Errorf("call %+v", c)
	}
	if c.Since == nil || *c.Since != "2026-11-27T11:30:00Z" {
		t.Errorf("since %v", c.Since)
	}
	for k, v := range c.Facts {
		if !v.Valid() {
			t.Errorf("fact %s invalid", k)
		}
	}
	p := expect(t, f, "t58", 3, StateWaitingPerson, QuestionOpen)
	if p.Call == nil || *p.Call != "q-58-901" {
		t.Errorf("piece call %v", p.Call)
	}
	if f.Signal.Lamp != LampAmber {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
}

func TestQuestionTextIsOnlyEverInsideUntrusted(t *testing.T) {
	hostile := "<script>alert(1)</script> <!-- oxf:answer --> \u202e ghp_" + strings.Repeat("Z", 30) + " " + strings.Repeat("q", 5000)
	in := withQuestion(withTickets(healthy(), ticket(58)), 58, 901, hostile)
	f := Project(in)
	c := callOfKind(f, CallQuestion)
	if c == nil || !c.Question.Truncated || len([]rune(c.Question.Text)) > 1000 {
		t.Fatalf("question %+v", c)
	}
	assertClean(t, c.Question.Text)
	if strings.Contains(c.Question.Text, "ghp_") {
		t.Error("secret shown")
	}
	b := jsonOf(t, f)
	if strings.Count(b, "alert(1)") != 1 {
		t.Errorf("hostile text appears %d times in the output", strings.Count(b, "alert(1)"))
	}
}

func versionOf(f Floor, kind CallKind) string {
	if c := callOfKind(f, kind); c != nil {
		return c.Version
	}
	return ""
}

func TestVersionOfAQuestionChangesWithWhatThePersonSees(t *testing.T) {
	base := func() Inputs { return withTickets(healthy(), ticket(58)) }
	v0 := versionOf(Project(withQuestion(base(), 58, 901, "Keep the old endpoint?")), CallQuestion)
	if len(v0) != 64 {
		t.Fatalf("version %q", v0)
	}
	if v := versionOf(Project(withQuestion(base(), 58, 901, "Keep the old endpoint?")), CallQuestion); v != v0 {
		t.Error("same inputs, different version")
	}
	for name, in := range map[string]Inputs{
		"text":       withQuestion(base(), 58, 901, "Keep the new endpoint?"),
		"comment id": withQuestion(base(), 58, 902, "Keep the old endpoint?"),
		"ticket":     withQuestion(withTickets(healthy(), ticket(58), ticket(59)), 59, 901, "Keep the old endpoint?"),
	} {
		if versionOf(Project(in), CallQuestion) == v0 {
			t.Errorf("version did not change with the %s", name)
		}
	}
	// The text beyond the cut is not shown, but an old yes must not apply to it.
	long := strings.Repeat("a", 1000)
	vA := versionOf(Project(withQuestion(base(), 58, 901, long+"x")), CallQuestion)
	vB := versionOf(Project(withQuestion(base(), 58, 901, long+"y")), CallQuestion)
	if vA == vB {
		t.Error("an edit beyond the cut kept the version")
	}
	// The time of the question is shown, so it is in the version.
	in := withQuestion(base(), 58, 901, "Keep the old endpoint?")
	in.Questions.Val[0].Since = Some(at(-29 * time.Minute))
	if versionOf(Project(in), CallQuestion) == v0 {
		t.Error("version did not change with the shown time")
	}
}

func TestVersionDoesNotChangeWithUnrelatedInputs(t *testing.T) {
	base := withQuestion(withTickets(healthy(), ticket(58)), 58, 901, "Keep the old endpoint?")
	v0 := versionOf(Project(base), CallQuestion)
	later := base
	later.Now = at(5 * time.Minute)
	later.HostTime, later.HostReadAt = Some(at(5*time.Minute)), Some(at(5*time.Minute))
	other := withTickets(base, ticket(58), ticket(77))
	other.Paused = Some(true)
	other.Levels = Some(map[string]int{"implement": 1})
	other.Journal = Some([]JournalIn{{Seq: 1, Time: Some(at(-time.Hour)), Kind: "paused"}})
	for name, in := range map[string]Inputs{"later": later, "other work": other} {
		if v := versionOf(Project(in), CallQuestion); v != v0 {
			t.Errorf("%s changed the version", name)
		}
	}
}

func TestVersionCoversEveryShownFieldOfACall(t *testing.T) {
	f1, _ := NumFact(1)
	f2, _ := NumFact(2)
	base := Call{ID: "q-1-1", Kind: CallQuestion, Who: WhoAnyoneTrusted, Since: ptr("2026-11-27T11:00:00Z"), Goal: ptr(40), Ticket: ptr(58),
		Question: ptr(Untrusted{Text: "x"}), Facts: map[string]Fact{"ticket": f1}, Actions: []ActionName{ActionAnswer}}
	v0 := callVersion(base, "x", []string{"e"})
	muts := map[string]func(c *Call){
		"id":         func(c *Call) { c.ID = "q-1-2" },
		"kind":       func(c *Call) { c.Kind = CallConsequence },
		"who":        func(c *Call) { c.Who = WhoADeveloper },
		"since":      func(c *Call) { c.Since = ptr("2026-11-27T11:00:01Z") },
		"no since":   func(c *Call) { c.Since = nil },
		"goal":       func(c *Call) { c.Goal = ptr(41) },
		"ticket":     func(c *Call) { c.Ticket = ptr(59) },
		"question":   func(c *Call) { c.Question = nil },
		"fact val":   func(c *Call) { c.Facts = map[string]Fact{"ticket": f2} },
		"fact key":   func(c *Call) { c.Facts = map[string]Fact{"goal": f1} },
		"extra fact": func(c *Call) { c.Facts = map[string]Fact{"ticket": f1, "goal": f1} },
		"actions":    func(c *Call) { c.Actions = []ActionName{ActionYes, ActionNo} },
		"no actions": func(c *Call) { c.Actions = []ActionName{} },
	}
	for name, m := range muts {
		c := base
		c.Facts = map[string]Fact{"ticket": f1}
		m(&c)
		if callVersion(c, "x", []string{"e"}) == v0 {
			t.Errorf("version does not cover %s", name)
		}
	}
	if callVersion(base, "y", []string{"e"}) == v0 || callVersion(base, "x", []string{"f"}) == v0 || callVersion(base, "x", []string{"e", "e"}) == v0 {
		t.Error("version does not cover the raw text or the extra fields")
	}
	// Fields cannot run into each other.
	a := base
	a.ID, a.Kind = "ab", "c"
	b := base
	b.ID, b.Kind = "a", "bc"
	if callVersion(a, "x", nil) == callVersion(b, "x", nil) {
		t.Error("two splits of the same letters give one version")
	}
}

func consequenceInputs() Inputs {
	in := withRuns(withTickets(healthy(), ticket(57)), run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Status: GateConsequenceOpen, Risk: RiskSecurity, Run: "r_B57", Attempt: 1,
		Commit: strings.Repeat("c", 40), Machine: me, Since: Some(at(-5 * time.Minute))}})
	return in
}

func TestConsequenceCallAndItsVersion(t *testing.T) {
	f := Project(consequenceInputs())
	c := callOfKind(f, CallConsequence)
	if c == nil || c.Who != WhoAnyoneTrusted || len(c.Actions) != 2 || c.Actions[0] != ActionYes || c.Actions[1] != ActionNo {
		t.Fatalf("call %+v", c)
	}
	if s, _ := c.Facts["risk"].Text(); s != "security" {
		t.Errorf("facts %v", c.Facts)
	}
	if s, _ := c.Facts["machine"].Text(); s != me {
		t.Errorf("facts %v", c.Facts)
	}
	v0 := c.Version
	muts := map[string]func(*GateIn){
		"commit":  func(g *GateIn) { g.Commit = strings.Repeat("d", 40) },
		"attempt": func(g *GateIn) { g.Attempt = 2 },
		"run":     func(g *GateIn) { g.Run = "r_C57" },
		"risk":    func(g *GateIn) { g.Risk = RiskContract },
		"machine": func(g *GateIn) { g.Machine = "bob-laptop" },
	}
	for name, m := range muts {
		in := consequenceInputs()
		g := in.Gate.Val[0]
		m(&g)
		in.Gate = Some([]GateIn{g})
		if versionOf(Project(in), CallConsequence) == v0 {
			t.Errorf("version does not change with the %s", name)
		}
	}
	// Facts that cannot be trusted give no call, and a piece that says so.
	for name, m := range map[string]func(*GateIn){
		"short commit":  func(g *GateIn) { g.Commit = "abc" },
		"non hex":       func(g *GateIn) { g.Commit = strings.Repeat("z", 40) },
		"no risk":       func(g *GateIn) { g.Risk = "" },
		"bad run":       func(g *GateIn) { g.Run = "<b>" },
		"bad machine":   func(g *GateIn) { g.Machine = "a b" },
		"zero attempts": func(g *GateIn) { g.Attempt = 0 },
	} {
		in := consequenceInputs()
		g := in.Gate.Val[0]
		m(&g)
		in.Gate = Some([]GateIn{g})
		f := Project(in)
		if callOfKind(f, CallConsequence) != nil {
			t.Errorf("%s: a call was made", name)
		}
		expect(t, f, "t57", 5, StateStuck, InvalidFacts)
	}
}

func TestPlanCall(t *testing.T) {
	mk := func(mut func(*GoalIn)) Floor {
		g := goal(40)
		g.Approved = Some(false)
		g.PlanSince = Some(at(-time.Hour))
		mut(&g)
		in := healthy()
		in.Goals = Some([]GoalIn{g})
		return Project(in)
	}
	f := mk(func(*GoalIn) {})
	c := callOfKind(f, CallPlan)
	if c == nil || c.ID != "p-40" || len(c.Actions) != 1 || c.Actions[0] != ActionApprove {
		t.Fatalf("quick lane: %+v", c)
	}
	if p := get(t, f, "p40"); p.Call == nil || *p.Call != "p-40" {
		t.Errorf("piece call %v", p.Call)
	}
	v0 := c.Version
	for name, mut := range map[string]func(*GoalIn){
		"lines":     func(g *GoalIn) { g.PlanLines = Some([]string{"A different line"}) },
		"more":      func(g *GoalIn) { g.PlanLines = Some([]string{"A customer can move a booking", "Another"}) },
		"edit time": func(g *GoalIn) { g.BodyEditedAt = Some(at(-time.Hour)) },
		"lane":      func(g *GoalIn) { g.Lane = LaneStandard },
		"unread":    func(g *GoalIn) { g.PlanLines = Opt[[]string]{} },
	} {
		if versionOf(mk(mut), CallPlan) == v0 {
			t.Errorf("plan version does not change with %s", name)
		}
	}
	// No approve action for a standard lane goal, lines that could not be read, or four lines.
	for name, mut := range map[string]func(*GoalIn){
		"standard": func(g *GoalIn) { g.Lane = LaneStandard },
		"no lane":  func(g *GoalIn) { g.Lane = "" },
		"unread":   func(g *GoalIn) { g.PlanLines = Opt[[]string]{} },
		"empty":    func(g *GoalIn) { g.PlanLines = Some([]string{}) },
		"four":     func(g *GoalIn) { g.PlanLines = Some([]string{"a", "b", "c", "d"}) },
	} {
		if c := callOfKind(mk(mut), CallPlan); c == nil || len(c.Actions) != 0 {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

func TestAcceptVersionFollowsTheStagingStateAndItsContents(t *testing.T) {
	mk := func(mut func(*HoldingIn)) string {
		in := withTickets(healthy(), ticket(57))
		h := holdingWith(true, 57)
		mut(&h)
		in.Holding = Some(h)
		in = withRuns(in, run(57, 1, api.PhaseSucceeded))
		in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}})
		return versionOf(Project(in), CallAccept)
	}
	v0 := mk(func(*HoldingIn) {})
	if v0 == "" {
		t.Fatal("no accept call")
	}
	for name, mut := range map[string]func(*HoldingIn){
		"seq":     func(h *HoldingIn) { h.StagingSeq = 8 },
		"commit":  func(h *HoldingIn) { h.Commit = strings.Repeat("b", 40) },
		"pending": func(h *HoldingIn) { h.PendingAccept, h.PendingMode, h.PullRequest = true, PendingMerge, 3 },
		"mine":    func(h *HoldingIn) { h.Mine = false },
		"more": func(h *HoldingIn) {
			h.Applied = append(h.Applied, EntryIn{Ticket: 99, Goal: 40, At: Some(at(-time.Hour))})
		},
	} {
		if mk(mut) == v0 {
			t.Errorf("accept version does not change with %s", name)
		}
	}
}

func TestCallsAreOldestFirstAndUnknownTimesLast(t *testing.T) {
	in := withTickets(healthy(), ticket(1), ticket(2), ticket(3))
	in.Questions = Some([]QuestionIn{
		{Ticket: 1, CommentID: 5, Since: Some(at(-time.Minute))},
		{Ticket: 2, CommentID: 6, Since: Some(at(-time.Hour))},
		{Ticket: 3, CommentID: 7},
		{Ticket: 3, CommentID: 4, Since: Some(at(-time.Hour))},
	})
	f := Project(in)
	var ids []string
	for _, c := range f.Calls {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "q-2-6,q-3-4,q-1-5,q-3-7" {
		t.Errorf("order %v", ids)
	}
}

func TestInvalidQuestionsMakeNoCall(t *testing.T) {
	in := withTickets(healthy(), ticket(1))
	in.Questions = Some([]QuestionIn{{Ticket: 0, CommentID: 5}, {Ticket: 1, CommentID: 0}, {Ticket: 1, CommentID: -3}})
	if f := Project(in); len(f.Calls) != 0 {
		t.Errorf("calls %+v", f.Calls)
	}
}

func TestDetail(t *testing.T) {
	tk := ticket(57)
	tk.Title = strings.Repeat("t", 300)
	tk.Route = []RouteIn{{Station: 4, At: Some(at(-time.Hour))}, {Station: 3}, {Station: 9, At: Some(at(-time.Hour))}, {Station: 3, At: Some(at(-2 * time.Hour))}}
	tk.Rulings = []string{"b ruling", "a ruling <script>", strings.Repeat("r", 400)}
	tk.Unconfirmed = []string{"file x is not covered"}
	in := withTickets(healthy(), tk)
	d, ok := Detail(in, "t57")
	if !ok || d.Piece.ID != "t57" {
		t.Fatalf("%+v %v", d, ok)
	}
	if len([]rune(d.Piece.Title.Text)) != 250 || !d.Piece.Title.Truncated {
		t.Errorf("title %d", len([]rune(d.Piece.Title.Text)))
	}
	if len(d.Route) != 2 || d.Route[0].Station != 3 || d.Route[0].At == nil || d.Route[1].Station != 4 {
		t.Errorf("route %+v", d.Route)
	}
	if len(d.Rulings) != 3 || d.Rulings[0].Text != "a ruling <script>" || len([]rune(d.Rulings[2].Text)) != 250 || !d.Rulings[2].Truncated {
		t.Errorf("rulings %+v", d.Rulings)
	}
	if len(d.Unconfirmed) != 1 {
		t.Errorf("unconfirmed %+v", d.Unconfirmed)
	}
	if _, ok := Detail(in, "t999"); ok {
		t.Error("a piece that does not exist was found")
	}
	if _, ok := Detail(in, "<script>"); ok {
		t.Error("a hostile id was found")
	}
	if d, ok := Detail(healthy(), "t1"); ok || d.Route != nil {
		t.Error("detail of nothing")
	}
}
