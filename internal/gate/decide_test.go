package gate

import (
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// boundReview builds a bound review through BindReview, as production does.
func boundReview(t testing.TB, r *api.ReviewFile, harness string) *BoundReview {
	t.Helper()
	run := goodRun()
	run.Harness = harness
	b, err := BindReview(goodChange(), run, r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// at sets the attempt of the change and rebinds the review to it, as the
// orchestrator would. It leaves the review alone when the attempt is not a
// valid one.
func at(f *Facts, n int) {
	f.Change.Attempt = n
	if f.Review == nil || !f.Review.valid {
		return
	}
	run := f.Review.run
	run.Attempt = n
	if b, err := BindReview(f.Change, run, f.Review.file); err == nil {
		f.Review = b
	}
}

// okFacts is a change that passes: verify is fine, the other harness agrees,
// risk is low, first attempt of three.
func okFacts(t testing.TB) Facts {
	return Facts{
		Task:           api.TaskImplement,
		Change:         goodChange(),
		Verify:         VerifyFacts{ProtectedOK: true, ChecksPassed: true},
		Review:         boundReview(t, rev("agrees"), api.HarnessCodex),
		Risk:           api.Risk{Level: api.RiskLow, Reasons: []string{"served criteria working"}},
		BuilderHarness: api.HarnessClaudeCode,
		MaxAttempts:    3,
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name     string
		edit     func(t testing.TB, f *Facts)
		outcome  Outcome
		kind     ExceptionKind
		single   bool
		missing  bool
		inReason string
	}{
		// "A change from implement or fix-ci passes when all three hold."
		{"all three hold", func(testing.TB, *Facts) {}, OutcomePass, "", false, false, "agrees"},
		{"medium risk passes", func(_ testing.TB, f *Facts) { f.Risk.Level = api.RiskMedium }, OutcomePass, "", false, false, ""},
		{"fix-ci passes the same way", func(_ testing.TB, f *Facts) { f.Task = api.TaskFixCI }, OutcomePass, "", false, false, ""},

		// "Verify passes: no protected path, task checks pass, no regression."
		{"protected path touched", func(_ testing.TB, f *Facts) {
			f.Verify.ProtectedOK = false
			f.Verify.ProtectedTouched = []string{".factory/factory.yaml"}
		}, OutcomeRework, "", false, false, ".factory/factory.yaml"},
		{"a check failed", func(_ testing.TB, f *Facts) {
			f.Verify.ChecksPassed = false
			f.Verify.FailedChecks = []string{"unit"}
		}, OutcomeRework, "", false, false, "unit"},
		{"a check failed with no name", func(_ testing.TB, f *Facts) { f.Verify.ChecksPassed = false }, OutcomeRework, "", false, false, "check"},
		{"a working criterion stopped working", func(_ testing.TB, f *Facts) {
			f.Acceptance = []api.AcceptanceEntry{{ID: 3, State: api.StateNotWorking, Before: api.StateWorking, Check: "go test"}}
		}, OutcomeRework, "", false, false, "criterion 3"},
		{"a working criterion is now could not confirm", func(_ testing.TB, f *Facts) {
			f.Acceptance = []api.AcceptanceEntry{{ID: 3, State: api.StateCouldNotConfirm, Before: api.StateWorking}}
		}, OutcomeRework, "", false, false, "criterion 3"},
		{"a state nobody knows counts as not working", func(_ testing.TB, f *Facts) {
			f.Acceptance = []api.AcceptanceEntry{{ID: 3, State: "great", Before: api.StateWorking}}
		}, OutcomeRework, "", false, false, "criterion 3"},
		{"a criterion that was not working before is no regression", func(_ testing.TB, f *Facts) {
			f.Acceptance = []api.AcceptanceEntry{{ID: 3, State: api.StateNotWorking, Before: api.StateNotWorking, Check: "x"}}
		}, OutcomePass, "", false, false, ""},
		{"a criterion that became working", func(_ testing.TB, f *Facts) {
			f.Acceptance = []api.AcceptanceEntry{{ID: 3, State: api.StateWorking, Before: api.StateNotWorking, Check: "x"}}
		}, OutcomePass, "", false, false, ""},

		// "If verify fails or the review does not agree, back to the builder."
		{"no review at all", func(_ testing.TB, f *Facts) { f.Review = nil }, OutcomeReviewAgain, "", false, true, "usable review"},
		{"review concerns", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("concerns", pt("major")), api.HarnessCodex)
		}, OutcomeRework, "", false, false, "concerns"},
		{"review disagrees", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("disagrees", pt("blocking")), api.HarnessCodex)
		}, OutcomeRework, "", false, false, "disagrees"},
		{"agrees with a blocking point", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("agrees", pt("blocking")), api.HarnessCodex)
		}, OutcomeRework, "", false, false, "blocking"},
		{"empty review that does not agree counts as no review", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("concerns"), api.HarnessCodex)
		}, OutcomeReviewAgain, "", false, true, "no points"},
		{"a review that does not agree never becomes a question, even at high risk", func(t testing.TB, f *Facts) {
			f.Risk.Level = api.RiskHigh
			f.Review = boundReview(t, rev("disagrees", pt("major")), api.HarnessCodex)
		}, OutcomeRework, "", false, false, "disagrees"},
		{"high risk and no review is a rework", func(_ testing.TB, f *Facts) {
			f.Risk.Level = api.RiskHigh
			f.Review = nil
		}, OutcomeReviewAgain, "", false, true, ""},
		{"no review, review tries left", func(_ testing.TB, f *Facts) { f.Review = nil; f.ReviewTries = 1 }, OutcomeReviewAgain, "", false, true, ""},
		{"no review, review tries used up", func(_ testing.TB, f *Facts) { f.Review = nil; f.ReviewTries = 2 }, OutcomeException, ExceptionNoReview, false, true, ""},
		{"no review, own review limit of one", func(_ testing.TB, f *Facts) { f.Review = nil; f.MaxReviewTries = 1; f.ReviewTries = 1 }, OutcomeException, ExceptionNoReview, false, true, ""},
		{"no review does not spend an attempt on the last attempt", func(_ testing.TB, f *Facts) { at(f, 3); f.Review = nil }, OutcomeReviewAgain, "", false, true, ""},
		{"a review of another change is no review", func(t testing.TB, f *Facts) {
			run := goodRun()
			other := goodChange()
			other.Commit = strings.Repeat("e", 40)
			run.Commit = other.Commit
			b, err := BindReview(other, run, rev("agrees"))
			if err != nil {
				t.Fatal(err)
			}
			f.Review = b
		}, OutcomeReviewAgain, "", false, true, "different change"},
		{"a zero review is no review", func(_ testing.TB, f *Facts) { f.Review = &BoundReview{} }, OutcomeReviewAgain, "", false, true, ""},
		{"review try limit over the hard bound", func(_ testing.TB, f *Facts) { f.MaxReviewTries = 6 }, OutcomeException, ExceptionInvalidFacts, false, false, "at most"},
		{"review try limit at the hard bound", func(_ testing.TB, f *Facts) { f.MaxReviewTries = 5; f.Review = nil; f.ReviewTries = 4 }, OutcomeReviewAgain, "", false, true, ""},
		{"acceptance, no review, tries used up, attempts left", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskAcceptance
			f.Review = nil
			f.ReviewTries = 2
		}, OutcomeException, ExceptionNoReview, false, true, ""},
		{"acceptance, no review, tries used up, attempts used up", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskAcceptance
			f.Review = nil
			f.ReviewTries = 2
			at(f, 3)
		}, OutcomeException, ExceptionAcceptanceUnconfirmed, false, true, ""},
		{"onboard, no review, tries used up, attempts used up", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskOnboard
			f.Review = nil
			f.ReviewTries = 2
			at(f, 3)
		}, OutcomeException, ExceptionSetupNotFinished, false, true, ""},
		{"implement, no review, tries used up, attempts used up", func(_ testing.TB, f *Facts) {
			f.Review = nil
			f.ReviewTries = 2
			at(f, 3)
		}, OutcomeException, ExceptionNoReview, false, true, ""},
		{"negative review tries", func(_ testing.TB, f *Facts) { f.ReviewTries = -1 }, OutcomeException, ExceptionInvalidFacts, false, false, "negative"},

		// "When the attempts are used up, the goal is off course."
		{"verify fails on the last attempt", func(_ testing.TB, f *Facts) {
			at(f, 3)
			f.Verify.ChecksPassed = false
		}, OutcomeException, ExceptionOffCourse, false, false, ""},
		{"review disagrees on the last attempt", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("disagrees", pt("major")), api.HarnessCodex)
			at(f, 3)
		}, OutcomeException, ExceptionOffCourse, false, false, "disagrees"},
		{"past the last attempt is also used up", func(_ testing.TB, f *Facts) {
			at(f, 9)
			f.Review = nil
			f.Verify.ChecksPassed = false
		}, OutcomeException, ExceptionOffCourse, false, true, ""},
		{"one attempt allowed, it failed", func(_ testing.TB, f *Facts) {
			at(f, 1)
			f.MaxAttempts = 1
			f.Review = nil
			f.Verify.ChecksPassed = false
		}, OutcomeException, ExceptionOffCourse, false, true, ""},
		{"second of three fails: rework remains", func(_ testing.TB, f *Facts) {
			at(f, 2)
			f.Review = nil
			f.Verify.ChecksPassed = false
		}, OutcomeRework, "", false, true, ""},
		{"last attempt that passes still passes", func(_ testing.TB, f *Facts) { at(f, 3) }, OutcomePass, "", false, false, ""},

		// "High risk by content with an agreeing review is an exception."
		{"high risk and agreeing review", func(_ testing.TB, f *Facts) {
			f.Risk = api.Risk{Level: api.RiskHigh, Reasons: []string{"contract path changed: api/openapi.yaml"}}
		}, OutcomeException, ExceptionHighRisk, false, false, "contract path"},
		{"high risk on the last attempt is still the question", func(_ testing.TB, f *Facts) {
			at(f, 3)
			f.Risk.Level = api.RiskHigh
		}, OutcomeException, ExceptionHighRisk, false, false, ""},

		// "A review by the same harness may pass but marks single-opinion."
		{"same harness passes, marked", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("agrees"), api.HarnessClaudeCode)
			f.Risk.Level = api.RiskMedium
		}, OutcomePass, "", true, false, ""},
		{"same harness at high risk is still marked", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("agrees"), api.HarnessClaudeCode)
			f.Risk.Level = api.RiskHigh
		}, OutcomeException, ExceptionHighRisk, true, false, ""},
		{"same harness, different spelling", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("agrees"), "CLAUDE-CODE")
		}, OutcomePass, "", true, false, ""},
		{"unknown builder harness counts as single opinion", func(_ testing.TB, f *Facts) { f.BuilderHarness = "" }, OutcomePass, "", true, false, ""},
		{"same harness rework keeps the mark", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("concerns", pt("major")), api.HarnessClaudeCode)
		}, OutcomeRework, "", true, false, ""},

		// "Which task kinds the risk rules apply to": implement and fix-ci.
		{"acceptance task ignores risk", func(t testing.TB, f *Facts) {
			f.Task = api.TaskAcceptance
			f.Review = boundReview(t, &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectAcceptance, Verdict: "agrees"}, api.HarnessCodex)
			f.Risk = api.Risk{Level: api.RiskHigh}
		}, OutcomePass, "", false, false, ""},
		{"onboard task ignores risk", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskOnboard
			f.Risk = api.Risk{}
		}, OutcomePass, "", false, false, ""},
		{"onboard needs an agreeing review", func(t testing.TB, f *Facts) {
			f.Task = api.TaskOnboard
			f.Review = boundReview(t, rev("concerns", pt("major")), api.HarnessCodex)
		}, OutcomeRework, "", false, false, ""},
		{"acceptance attempts used up", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskAcceptance
			at(f, 3)
			f.Review = nil
			f.Verify.ChecksPassed = false
		}, OutcomeException, ExceptionAcceptanceUnconfirmed, false, true, ""},
		{"onboard attempts used up", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskOnboard
			at(f, 3)
			f.Verify.ChecksPassed = false
		}, OutcomeException, ExceptionSetupNotFinished, false, false, ""},
		{"verify contradicts itself (protected)", func(_ testing.TB, f *Facts) { f.Verify.ProtectedTouched = []string{"x"} }, OutcomeException, ExceptionInvalidFacts, false, false, "contradict"},
		{"verify contradicts itself (checks)", func(_ testing.TB, f *Facts) { f.Verify.FailedChecks = []string{"x"} }, OutcomeException, ExceptionInvalidFacts, false, false, "contradict"},
		{"no change reference", func(_ testing.TB, f *Facts) { f.Change = ChangeRef{} }, OutcomeException, ExceptionInvalidFacts, false, false, "ticket"},

		// Deny by default on facts the gate cannot read.
		{"review task is never gated", func(_ testing.TB, f *Facts) { f.Task = api.TaskReview }, OutcomeException, ExceptionInvalidFacts, false, false, "task"},
		{"unknown task", func(_ testing.TB, f *Facts) { f.Task = "deploy" }, OutcomeException, ExceptionInvalidFacts, false, false, ""},
		{"no risk level on an implement change", func(_ testing.TB, f *Facts) { f.Risk = api.Risk{} }, OutcomeException, ExceptionInvalidFacts, false, false, "risk"},
		{"made up risk level", func(_ testing.TB, f *Facts) { f.Risk.Level = "none" }, OutcomeException, ExceptionInvalidFacts, false, false, "risk"},
		{"attempt zero", func(_ testing.TB, f *Facts) { at(f, 0) }, OutcomeException, ExceptionInvalidFacts, false, false, "attempt"},
		{"no attempt limit", func(_ testing.TB, f *Facts) { f.MaxAttempts = 0 }, OutcomeException, ExceptionInvalidFacts, false, false, "attempt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := okFacts(t)
			tc.edit(t, &f)
			d := Decide(f)
			if d.Outcome != tc.outcome || d.Exception != tc.kind {
				t.Fatalf("got %s/%q (%v), want %s/%q", d.Outcome, d.Exception, d.Reasons, tc.outcome, tc.kind)
			}
			if d.SingleOpinion != tc.single {
				t.Fatalf("singleOpinion = %v, want %v", d.SingleOpinion, tc.single)
			}
			if d.ReviewMissing != tc.missing {
				t.Fatalf("reviewMissing = %v, want %v", d.ReviewMissing, tc.missing)
			}
			if len(d.Reasons) == 0 {
				t.Fatal("a decision must give reasons")
			}
			if tc.inReason != "" && !strings.Contains(strings.ToLower(strings.Join(d.Reasons, "|")), strings.ToLower(tc.inReason)) {
				t.Fatalf("reasons %v lack %q", d.Reasons, tc.inReason)
			}
			if d.Outcome == OutcomeException && d.Plain == "" {
				t.Fatal("an exception must tell the person in plain words")
			}
			if d.Outcome != OutcomeException && d.Exception != "" {
				t.Fatal("only an exception has a kind")
			}
		})
	}
}

// The person never reads the words of the engineering view.
func TestPlainWordsAvoidEngineeringWords(t *testing.T) {
	banned := []string{"pull request", "merge", "diff", "review", "branch", "commit"}
	cases := []func(*Facts){
		func(f *Facts) { f.Risk.Level = api.RiskHigh },
		func(f *Facts) { at(f, 3); f.Verify.ChecksPassed = false },
		func(f *Facts) { f.Task = api.TaskReview },
		func(f *Facts) { f.Review = nil; f.ReviewTries = 5 },
		func(f *Facts) { f.Task = api.TaskAcceptance; at(f, 3); f.Verify.ChecksPassed = false },
		func(f *Facts) { f.Task = api.TaskOnboard; at(f, 3); f.Verify.ChecksPassed = false },
	}
	for i, edit := range cases {
		f := okFacts(t)
		edit(&f)
		d := Decide(f)
		if d.Outcome != OutcomeException {
			t.Fatalf("case %d: not an exception", i)
		}
		for _, w := range banned {
			if strings.Contains(strings.ToLower(d.Plain), w) {
				t.Fatalf("case %d: plain text %q holds %q", i, d.Plain, w)
			}
		}
	}
}

// reviewVariants builds every kind of review value a careless caller could
// hand over for one change: nil, zero, bound for this change in several
// verdicts, and bound for another change.
type reviewVariant struct {
	name string
	r    *BoundReview
	// agrees is true when the review's content agrees. subject is its
	// subject. other is true when it was bound to another change.
	agrees  bool
	subject string
	other   bool
}

func reviewVariants(t testing.TB, c ChangeRef) []reviewVariant {
	bind := func(ch ChangeRef, file *api.ReviewFile, harness string) *BoundReview {
		run := ReviewRun{RunID: "r_review", Task: api.TaskReview, Phase: api.PhaseSucceeded,
			Ticket: ch.Ticket, Attempt: ch.Attempt, Commit: ch.Commit, Harness: harness}
		b, err := BindReview(ch, run, file)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	other := c
	other.Commit = strings.Repeat("e", 40)
	acc := &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectAcceptance, Verdict: "agrees"}
	spec := &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectSpec, Verdict: "agrees"}
	return []reviewVariant{
		{name: "nil"},
		{name: "zero", r: &BoundReview{}},
		{name: "zero but marked valid", r: &BoundReview{valid: true}},
		{"agrees other harness", bind(c, rev("agrees"), api.HarnessCodex), true, api.SubjectChange, false},
		{"agrees same harness", bind(c, rev("agrees", pt("minor")), api.HarnessClaudeCode), true, api.SubjectChange, false},
		{"agrees about acceptance", bind(c, acc, api.HarnessCodex), true, api.SubjectAcceptance, false},
		{"agrees about a spec", bind(c, spec, api.HarnessCodex), true, api.SubjectSpec, false},
		{name: "agrees with blocking", r: bind(c, rev("agrees", pt("blocking")), api.HarnessCodex)},
		{name: "concerns", r: bind(c, rev("concerns", pt("major")), api.HarnessCodex)},
		{name: "disagrees", r: bind(c, rev("disagrees", pt("blocking")), api.HarnessCodex)},
		{name: "concerns with no points", r: bind(c, rev("concerns"), api.HarnessCodex)},
		{"agrees for another change", bind(other, rev("agrees"), api.HarnessCodex), true, api.SubjectChange, true},
	}
}

func subjectOf(task string) string {
	if task == api.TaskAcceptance {
		return api.SubjectAcceptance
	}
	return api.SubjectChange
}

// oracle says, from first principles and not from the gate's code, whether a
// change may pass without a person.
func oracle(f Facts, reviewAgrees bool) bool {
	switch f.Task {
	case api.TaskImplement, api.TaskFixCI, api.TaskAcceptance, api.TaskOnboard:
	default:
		return false
	}
	c := f.Change
	if c.Ticket < 1 || c.Attempt < 1 || len(c.Commit) != 40 || c.BuilderRun == "" || f.MaxAttempts < 1 || f.ReviewTries < 0 || f.MaxReviewTries < 0 || f.MaxReviewTries > 5 {
		return false
	}
	if f.Task == api.TaskImplement || f.Task == api.TaskFixCI {
		if f.Risk.Level != api.RiskLow && f.Risk.Level != api.RiskMedium {
			return false
		}
	}
	v := f.Verify
	if !v.ProtectedOK || !v.ChecksPassed || len(v.ProtectedTouched) > 0 || len(v.FailedChecks) > 0 {
		return false
	}
	for _, e := range f.Acceptance {
		if !e.Before.Valid() || (e.Before == api.StateWorking && e.State != api.StateWorking) {
			return false
		}
	}
	return reviewAgrees
}

// The whole walk: every combination of the facts, with zero, nil and
// hand-built values for each input type. PASS only when verify passed with no
// contradiction, no regression with every earlier state known, a valid bound
// agreeing review of this change, and risk not high.
func TestNoCombinationPassesWithoutAllConditions(t *testing.T) {
	tasks := []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance, api.TaskOnboard, api.TaskReview, "", "x"}
	verifies := []VerifyFacts{
		{ProtectedOK: true, ChecksPassed: true},
		{ProtectedOK: false, ChecksPassed: true},
		{ProtectedOK: true, ChecksPassed: false},
		{ProtectedOK: true, ChecksPassed: true, ProtectedTouched: []string{"a"}},
		{ProtectedOK: true, ChecksPassed: true, FailedChecks: []string{"a"}},
		{},
	}
	accs := [][]api.AcceptanceEntry{
		nil,
		{{ID: 1, State: api.StateWorking, Before: api.StateWorking, Check: "c"}},
		{{ID: 1, State: api.StateNotWorking, Before: api.StateWorking}},
		{{ID: 1, State: api.StateWorking}},
		{{ID: 1, State: api.StateWorking, Before: "weird"}},
		{{ID: 1, State: "weird", Before: api.StateWorking}},
		{{}},
	}
	risks := []string{api.RiskLow, api.RiskMedium, api.RiskHigh, "", "weird"}
	builders := []string{api.HarnessClaudeCode}
	changes := []ChangeRef{
		{Ticket: 42, Attempt: 1, Commit: testCommit, BuilderRun: "r_build"},
		{Ticket: 42, Attempt: 3, Commit: testCommit, BuilderRun: "r_build"},
		{Ticket: 42, Attempt: 4, Commit: testCommit, BuilderRun: "r_build"},
		{},
		{Ticket: 42, Attempt: 0, Commit: testCommit, BuilderRun: "r_build"},
		{Ticket: 42, Attempt: 1, Commit: testCommit[:7], BuilderRun: "r_build"},
		{Ticket: 0, Attempt: 1, Commit: testCommit, BuilderRun: "r_build"},
		{Ticket: 42, Attempt: 1, Commit: testCommit, BuilderRun: ""},
	}
	tries := [][2]int{{0, 0}, {2, 0}, {-1, 0}, {0, -1}, {0, 6}}
	n, passed, asked := 0, 0, 0
	for _, c := range changes {
		// A review can only be bound to a change that is well formed. For the
		// others, bind to a good change that differs, so the review does not
		// match.
		bindTo := c
		if c.valid() != nil {
			bindTo = goodChange()
		}
		for _, rv := range reviewVariants(t, bindTo) {
			for _, task := range tasks {
				// Independent of the gate: the content agrees, the subject is
				// the one the record names for the task, and it is about this
				// change.
				agrees := rv.agrees && !rv.other && rv.subject == subjectOf(task) && rv.r.change == c
				for _, v := range verifies {
					for _, acc := range accs {
						for _, risk := range risks {
							for _, b := range builders {
								for _, tr := range tries {
									for _, maxA := range []int{0, 3} {
										f := Facts{Task: task, Change: c, Verify: v, Acceptance: acc, Review: rv.r,
											ReviewTries: tr[0], MaxReviewTries: tr[1], Risk: api.Risk{Level: risk},
											BuilderHarness: b, MaxAttempts: maxA}
										d := Decide(f)
										n++
										want := oracle(f, agrees)
										if want && riskApplies(task) && risk == api.RiskHigh {
											t.Fatal("oracle bug")
										}
										if (d.Outcome == OutcomePass) != want {
											t.Fatalf("pass = %v, oracle %v, review %s, facts %+v, decision %+v", d.Outcome == OutcomePass, want, rv.name, f, d)
										}
										if d.Outcome == OutcomePass {
											passed++
										}
										if d.Outcome == OutcomeException && d.Exception == "" {
											t.Fatal("exception with no kind")
										}
										if d.Outcome != OutcomeException && d.Exception != "" {
											t.Fatal("kind without exception")
										}
										if len(d.Reasons) == 0 {
											t.Fatalf("no reasons: %+v", d)
										}
										if d.Outcome == OutcomeException && (d.Exception == ExceptionHighRisk || n%11 == 0) {
											got, err := ApplyPersonDecision(d, c, AnswerYes)
											if d.Exception != ExceptionHighRisk && (err == nil || got.Outcome == OutcomePass) {
												t.Fatalf("a yes passed %+v", d)
											}
											if d.Exception == ExceptionHighRisk {
												asked++
												if !agrees || !v.ProtectedOK || !v.ChecksPassed || len(v.ProtectedTouched)+len(v.FailedChecks) > 0 {
													t.Fatalf("asked a person without earning it: %+v", f)
												}
												if err != nil || got.Outcome != OutcomePass {
													t.Fatalf("a good yes failed: %v", err)
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if passed == 0 || asked == 0 {
		t.Fatalf("passed %d, asked %d: the walk proves nothing", passed, asked)
	}
	t.Logf("walked %d combinations, %d passed, %d asked a person", n, passed, asked)
}

func TestDecideDoesNotAliasInput(t *testing.T) {
	f := okFacts(t)
	f.Risk = api.Risk{Level: api.RiskHigh, Reasons: []string{"a"}}
	d := Decide(f)
	f.Risk.Reasons[0] = "changed"
	if d.Risk.Reasons[0] != "a" {
		t.Fatal("decision shares memory with its input")
	}
}

// The record names the subject of each task's review. Any other subject is
// refused for that task.
func TestReviewSubjectPerTask(t *testing.T) {
	subjects := []string{api.SubjectChange, api.SubjectAcceptance, api.SubjectSpec, api.SubjectPlan}
	want := map[string]string{
		api.TaskImplement: api.SubjectChange, api.TaskFixCI: api.SubjectChange,
		api.TaskOnboard: api.SubjectChange, api.TaskAcceptance: api.SubjectAcceptance,
	}
	for task, subj := range want {
		for _, got := range subjects {
			f := okFacts(t)
			f.Task = task
			f.Risk.Level = api.RiskLow
			file := &api.ReviewFile{SchemaVersion: 1, Subject: got, Verdict: "agrees"}
			f.Review = boundReview(t, file, api.HarnessCodex)
			d := Decide(f)
			if (got == subj) != (d.Outcome == OutcomePass) {
				t.Fatalf("task %s, subject %s: outcome %s (%v)", task, got, d.Outcome, d.Reasons)
			}
			if got != subj && d.Outcome != OutcomeReviewAgain {
				t.Fatalf("task %s, subject %s: a wrong subject must count as no review, got %s", task, got, d.Outcome)
			}
		}
	}
}
