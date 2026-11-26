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

// okFacts is a change that passes: verify is fine, the other harness agrees,
// risk is low, first attempt of three.
func okFacts(t testing.TB) Facts {
	return Facts{
		Task:           api.TaskImplement,
		Verify:         VerifyFacts{ProtectedOK: true, ChecksPassed: true},
		Review:         boundReview(t, rev("agrees"), api.HarnessCodex),
		Risk:           api.Risk{Level: api.RiskLow, Reasons: []string{"served criteria working"}},
		BuilderHarness: api.HarnessClaudeCode,
		Attempt:        1,
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
		{"no review at all", func(_ testing.TB, f *Facts) { f.Review = nil }, OutcomeRework, "", false, true, "no review"},
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
		}, OutcomeRework, "", false, true, "no points"},
		{"a review that does not agree never becomes a question, even at high risk", func(t testing.TB, f *Facts) {
			f.Risk.Level = api.RiskHigh
			f.Review = boundReview(t, rev("disagrees", pt("major")), api.HarnessCodex)
		}, OutcomeRework, "", false, false, "disagrees"},
		{"high risk and no review is a rework", func(_ testing.TB, f *Facts) {
			f.Risk.Level = api.RiskHigh
			f.Review = nil
		}, OutcomeRework, "", false, true, ""},

		// "When the attempts are used up, the goal is off course."
		{"verify fails on the last attempt", func(_ testing.TB, f *Facts) {
			f.Attempt = 3
			f.Verify.ChecksPassed = false
		}, OutcomeException, ExceptionOffCourse, false, false, ""},
		{"review disagrees on the last attempt", func(t testing.TB, f *Facts) {
			f.Attempt = 3
			f.Review = boundReview(t, rev("disagrees", pt("major")), api.HarnessCodex)
		}, OutcomeException, ExceptionOffCourse, false, false, "disagrees"},
		{"past the last attempt is also used up", func(_ testing.TB, f *Facts) {
			f.Attempt = 9
			f.Review = nil
		}, OutcomeException, ExceptionOffCourse, false, true, ""},
		{"one attempt allowed, it failed", func(_ testing.TB, f *Facts) {
			f.Attempt, f.MaxAttempts = 1, 1
			f.Review = nil
		}, OutcomeException, ExceptionOffCourse, false, true, ""},
		{"second of three fails: rework remains", func(_ testing.TB, f *Facts) {
			f.Attempt = 2
			f.Review = nil
		}, OutcomeRework, "", false, true, ""},
		{"last attempt that passes still passes", func(_ testing.TB, f *Facts) { f.Attempt = 3 }, OutcomePass, "", false, false, ""},

		// "High risk by content with an agreeing review is an exception."
		{"high risk and agreeing review", func(_ testing.TB, f *Facts) {
			f.Risk = api.Risk{Level: api.RiskHigh, Reasons: []string{"contract path changed: api/openapi.yaml"}}
		}, OutcomeException, ExceptionHighRisk, false, false, "contract path"},
		{"high risk on the last attempt is still the question", func(_ testing.TB, f *Facts) {
			f.Attempt = 3
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
			f.Review = boundReview(t, rev("agrees"), " Claude-Code ")
		}, OutcomePass, "", true, false, ""},
		{"unknown builder harness counts as single opinion", func(_ testing.TB, f *Facts) { f.BuilderHarness = "" }, OutcomePass, "", true, false, ""},
		{"same harness rework keeps the mark", func(t testing.TB, f *Facts) {
			f.Review = boundReview(t, rev("concerns", pt("major")), api.HarnessClaudeCode)
		}, OutcomeRework, "", true, false, ""},

		// "Which task kinds the risk rules apply to": implement and fix-ci.
		{"acceptance task ignores risk", func(_ testing.TB, f *Facts) {
			f.Task = api.TaskAcceptance
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
			f.Attempt = 3
			f.Review = nil
		}, OutcomeException, ExceptionOffCourse, false, true, ""},

		// Deny by default on facts the gate cannot read.
		{"review task is never gated", func(_ testing.TB, f *Facts) { f.Task = api.TaskReview }, OutcomeException, ExceptionInvalidFacts, false, false, "task"},
		{"unknown task", func(_ testing.TB, f *Facts) { f.Task = "deploy" }, OutcomeException, ExceptionInvalidFacts, false, false, ""},
		{"no risk level on an implement change", func(_ testing.TB, f *Facts) { f.Risk = api.Risk{} }, OutcomeException, ExceptionInvalidFacts, false, false, "risk"},
		{"made up risk level", func(_ testing.TB, f *Facts) { f.Risk.Level = "none" }, OutcomeException, ExceptionInvalidFacts, false, false, "risk"},
		{"attempt zero", func(_ testing.TB, f *Facts) { f.Attempt = 0 }, OutcomeException, ExceptionInvalidFacts, false, false, "attempt"},
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
		func(f *Facts) { f.Attempt = 3; f.Verify.ChecksPassed = false },
		func(f *Facts) { f.Task = api.TaskReview },
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

// Every combination of the facts. None may pass with a failed verify, a
// regression, or a review that does not agree.
func TestNoCombinationPassesWithoutAllConditions(t *testing.T) {
	type rv struct {
		name string
		r    *api.ReviewFile
		good bool
	}
	reviews := []rv{
		{"none", nil, false},
		{"agrees", rev("agrees"), true},
		{"agrees+minor", rev("agrees", pt("minor")), true},
		{"agrees+blocking", rev("agrees", pt("blocking")), false},
		{"concerns", rev("concerns", pt("major")), false},
		{"concerns empty", rev("concerns"), false},
		{"disagrees", rev("disagrees", pt("blocking")), false},
		{"disagrees empty", rev("disagrees"), false},
		{"wrong subject", &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectSpec, Verdict: "agrees"}, false},
	}
	tasks := []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance, api.TaskOnboard, api.TaskReview, "", "x"}
	risks := []string{api.RiskLow, api.RiskMedium, api.RiskHigh, "", "weird"}
	harnesses := []string{api.HarnessClaudeCode, api.HarnessCodex}
	builders := []string{api.HarnessClaudeCode, ""}
	n, passed := 0, 0
	for _, task := range tasks {
		for _, protOK := range []bool{false, true} {
			for _, checksOK := range []bool{false, true} {
				for _, regress := range []bool{false, true} {
					for _, r := range reviews {
						for _, h := range harnesses {
							for _, risk := range risks {
								for _, builder := range builders {
									for _, attempt := range []int{0, 1, 2, 3, 4} {
										f := Facts{
											Task:           task,
											Verify:         VerifyFacts{ProtectedOK: protOK, ChecksPassed: checksOK},
											Risk:           api.Risk{Level: risk},
											BuilderHarness: builder,
											Attempt:        attempt,
											MaxAttempts:    3,
										}
										if r.r != nil {
											f.Review = boundReview(t, r.r, h)
										}
										if regress {
											f.Acceptance = []api.AcceptanceEntry{{ID: 1, State: api.StateNotWorking, Before: api.StateWorking}}
										} else {
											f.Acceptance = []api.AcceptanceEntry{{ID: 1, State: api.StateWorking, Before: api.StateWorking, Check: "c"}}
										}
										d := Decide(f)
										n++
										if d.Outcome == OutcomePass {
											passed++
											if !protOK || !checksOK || regress || !r.good {
												t.Fatalf("passed with protOK=%v checksOK=%v regress=%v review=%s facts=%+v", protOK, checksOK, regress, r.name, f)
											}
											if risk == api.RiskHigh && (task == api.TaskImplement || task == api.TaskFixCI) {
												t.Fatalf("a high risk change passed without a person: %+v", f)
											}
											if task == api.TaskReview || task == "" || task == "x" || attempt < 1 {
												t.Fatalf("passed on facts that cannot be read: %+v", f)
											}
										}
										if d.Exception == ExceptionHighRisk && (!protOK || !checksOK || regress || !r.good) {
											t.Fatalf("asked a person about a change that did not earn the question: %+v", f)
										}
										if d.Outcome == OutcomeException && d.Exception == "" {
											t.Fatal("exception with no kind")
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
	if passed == 0 {
		t.Fatal("no combination passed, so the walk proves nothing")
	}
	t.Logf("walked %d combinations, %d passed", n, passed)
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
