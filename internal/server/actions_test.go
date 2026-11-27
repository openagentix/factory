package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/floor"
)

var (
	v64  = strings.Repeat("a", 64)
	key1 = "key-aaaaaaaaaaaaaaaa"
	key2 = "key-bbbbbbbbbbbbbbbb"
)

// fakeActions records what the server asks of it. Nothing it is given is
// believed or checked here: the tests look at what reached it.
type fakeActions struct {
	mu      sync.Mutex
	answers []AnswerRequest
	conseq  []ConsequenceRequest
	approve []ApproveRequest
	// hook runs inside every call. It may block, or fail.
	hook    func(ctx context.Context, steps Steps) (ActionResult, error)
	entered chan struct{}
}

func (f *fakeActions) run(ctx context.Context, steps Steps) (ActionResult, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.hook != nil {
		return f.hook(ctx, steps)
	}
	return ActionResult{Result: ResultDone, JournalSeq: 7}, nil
}
func (f *fakeActions) Answer(ctx context.Context, r AnswerRequest) (ActionResult, error) {
	f.mu.Lock()
	f.answers = append(f.answers, r)
	f.mu.Unlock()
	return f.run(ctx, r.Steps)
}
func (f *fakeActions) Consequence(ctx context.Context, r ConsequenceRequest) (ActionResult, error) {
	f.mu.Lock()
	f.conseq = append(f.conseq, r)
	f.mu.Unlock()
	return f.run(ctx, r.Steps)
}
func (f *fakeActions) Approve(ctx context.Context, r ApproveRequest) (ActionResult, error) {
	f.mu.Lock()
	f.approve = append(f.approve, r)
	f.mu.Unlock()
	return f.run(ctx, r.Steps)
}
func (f *fakeActions) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.answers) + len(f.conseq) + len(f.approve)
}

func actEnv(t *testing.T) (*testEnv, *fakeActions) {
	fa := &fakeActions{}
	e := newEnv(t, func(c *Config) { c.Actions = fa })
	return e, fa
}

func jsonHdr(e *testEnv, idem string) map[string]string {
	h := merge(e.auth(), map[string]string{"Content-Type": "application/json"})
	if idem != "" {
		h["Idempotency-Key"] = idem
	}
	return h
}

func post(e *testEnv, path, idem, body string) *httptest.ResponseRecorder {
	return e.do(req{method: "POST", path: path, body: body, hdr: jsonHdr(e, idem)})
}

func answerBody(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text, "version": v64})
	return string(b)
}

const verBody = `{"version":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"}`

// Attack 1 and the exact list of routes.
func TestTheRegisteredRoutesAreExactlyTheExpectedList(t *testing.T) {
	e, _ := actEnv(t)
	var got []string
	for _, r := range e.s.Routes() {
		got = append(got, r.Method+" "+r.Path)
	}
	sort.Strings(got)
	want := []string{
		"GET /api/v1/events", "GET /api/v1/floor",
		"POST /api/v1/calls/{id}/answer", "POST /api/v1/calls/{id}/approve",
		"POST /api/v1/calls/{id}/no", "POST /api/v1/calls/{id}/yes",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes:\n%s", strings.Join(got, "\n"))
	}
}

type actRoute struct{ name, path, body string }

func actRoutes() []actRoute {
	return []actRoute{
		{"answer", "/api/v1/calls/q-5-9/answer", answerBody("blue")},
		{"yes", "/api/v1/calls/c-5-run-1/yes", verBody},
		{"no", "/api/v1/calls/c-5-run-1/no", verBody},
		{"approve", "/api/v1/calls/p-3/approve", verBody},
	}
}

// Attacks 1, 2, 5, 6, 7: no key, wrong key flood, cross-site, rebinding.
func TestActionRoutesAreGuardedAndNeverCallActions(t *testing.T) {
	e, fa := actEnv(t)
	j := map[string]string{"Content-Type": "application/json", "Idempotency-Key": key1}
	for _, r := range actRoutes() {
		assertErr(t, e.do(req{method: "POST", path: r.path, body: r.body, hdr: j}), 401, CodeUnauthorized, r.name+" no key")
		assertErr(t, e.do(req{method: "POST", path: r.path, body: r.body, host: "evil.test:18765", hdr: merge(e.auth(), j)}), 421, CodeMisdirected, r.name+" rebinding")
		for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
			cross := map[string]string{"Content-Type": ct, "Origin": "https://evil.test", "Sec-Fetch-Site": "cross-site", "Idempotency-Key": key1}
			assertErr(t, e.do(req{method: "POST", path: r.path, body: r.body, hdr: merge(cross, e.auth())}), 403, CodeForbidden, r.name+" cross-site "+ct)
			same := merge(e.auth(), map[string]string{"Content-Type": ct, "Idempotency-Key": key1})
			assertErr(t, e.do(req{method: "POST", path: r.path, body: r.body, hdr: same}), 415, CodeUnsupportedMedia, r.name+" form "+ct)
		}
		rec := e.do(req{method: "POST", path: r.path, body: r.body, hdr: merge(e.auth(), j, map[string]string{"Origin": "https://evil.test"})})
		assertErr(t, rec, 403, CodeForbidden, r.name+" cross-site fetch")
		for h := range rec.Header() {
			if strings.HasPrefix(strings.ToLower(h), "access-control-") {
				t.Errorf("%s: %s", r.name, h)
			}
		}
	}
	// a wrong-key flood is limited after 10, and never reaches Actions
	var last int
	for i := 0; i < 12; i++ {
		last = e.do(req{method: "POST", path: actRoutes()[0].path, body: actRoutes()[0].body, remote: "10.4.4.4:1",
			hdr: map[string]string{"Authorization": "Bearer nope", "Content-Type": "application/json", "Idempotency-Key": key1}}).Code
	}
	if last != 429 {
		t.Fatalf("flood ended with %d", last)
	}
	if fa.calls() != 0 {
		t.Fatalf("Actions called %d times behind a failed guard", fa.calls())
	}
}

// Attack 3 and the shape of the reply.
func TestEachActionRouteCallsActionsOnceWithTheCheckedValues(t *testing.T) {
	e, fa := actEnv(t)
	for i, r := range actRoutes() {
		rec := post(e, r.path, fmt.Sprintf("key-%016d", i), r.body)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", r.name, rec.Code, rec.Body)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"mode":"demo","result":"done","journalSeq":7}` {
			t.Fatalf("%s: body %s", r.name, got)
		}
		assertSecure(t, rec, true, r.name)
	}
	if len(fa.answers) != 1 || fa.answers[0].Call != "q-5-9" || fa.answers[0].Version != v64 || fa.answers[0].Text != "blue" || fa.answers[0].Steps == nil {
		t.Fatalf("answers = %+v", fa.answers)
	}
	if len(fa.conseq) != 2 || !fa.conseq[0].Yes || fa.conseq[1].Yes || fa.conseq[0].Call != "c-5-run-1" {
		t.Fatalf("conseq = %+v", fa.conseq)
	}
	if len(fa.approve) != 1 || fa.approve[0].Call != "p-3" {
		t.Fatalf("approve = %+v", fa.approve)
	}
}

func TestActionsNilMeansEveryActionRouteIs503(t *testing.T) {
	e := newEnv(t)
	for _, r := range actRoutes() {
		assertErr(t, post(e, r.path, key1, r.body), 503, CodeUnavailable, r.name)
	}
}

// Attacks 11, 12, 14, 26, 27, 28, 29: nothing malformed reaches Actions.
func TestMalformedRequestsNeverReachActions(t *testing.T) {
	e, fa := actEnv(t)
	ans := "/api/v1/calls/q-5-9/answer"
	yes := "/api/v1/calls/c-5-run-1/yes"
	type tc struct {
		name, path, idem, body string
		status                 int
		code                   Code
	}
	good := answerBody("x")
	cases := []tc{
		{"no idempotency key", ans, "", good, 400, CodeBadIdempotencyKey},
		{"short key", ans, "short", good, 400, CodeBadIdempotencyKey},
		{"long key", ans, strings.Repeat("k", 65), good, 400, CodeBadIdempotencyKey},
		{"bad char key", ans, "key-aaaaaaaaaaaaaa!!", good, 400, CodeBadIdempotencyKey},
		{"version upper case", ans, key1, `{"text":"x","version":"` + strings.Repeat("A", 64) + `"}`, 400, CodeBadRequest},
		{"version 63", ans, key1, `{"text":"x","version":"` + strings.Repeat("a", 63) + `"}`, 400, CodeBadRequest},
		{"version number", ans, key1, `{"text":"x","version":5}`, 400, CodeBadRequest},
		{"version missing", ans, key1, `{"text":"x"}`, 400, CodeBadRequest},
		{"text missing", ans, key1, `{"version":"` + v64 + `"}`, 400, CodeBadRequest},
		{"empty body", ans, key1, ``, 400, CodeBadRequest},
		{"empty object", ans, key1, `{}`, 400, CodeBadRequest},
		{"unknown key", ans, key1, `{"text":"x","version":"` + v64 + `","extra":1}`, 400, CodeBadRequest},
		{"repeated key", ans, key1, `{"text":"x","text":"y","version":"` + v64 + `"}`, 400, CodeBadRequest},
		{"case twin", ans, key1, `{"text":"x","Text":"y","version":"` + v64 + `"}`, 400, CodeBadRequest},
		{"yes empty body", yes, key1, ``, 400, CodeBadRequest},
		{"yes extra key", yes, key1, `{"version":"` + v64 + `","yes":true}`, 400, CodeBadRequest},
		{"answer on c id", "/api/v1/calls/c-5-run-1/answer", key1, good, 404, CodeNotFound},
		{"yes on q id", "/api/v1/calls/q-5-9/yes", key1, verBody, 404, CodeNotFound},
		{"approve on q id", "/api/v1/calls/q-5-9/approve", key1, verBody, 404, CodeNotFound},
		{"dots", "/api/v1/calls/q-1-1.2/answer", key1, good, 404, CodeNotFound},
		{"dotdot", "/api/v1/calls/../answer", key1, good, 404, CodeNotFound},
		{"encoded dotdot", "/api/v1/calls/%2e%2e/answer", key1, good, 404, CodeNotFound},
		{"65 chars", "/api/v1/calls/q-" + strings.Repeat("1", 63) + "/answer", key1, good, 404, CodeNotFound},
		{"unicode", "/api/v1/calls/q-5-%C3%A9/answer", key1, good, 404, CodeNotFound},
		{"leading zero", "/api/v1/calls/q-05-9/answer", key1, good, 404, CodeNotFound},
		{"q without comment", "/api/v1/calls/q-5/answer", key1, good, 404, CodeNotFound},
		{"c without run", "/api/v1/calls/c-5-/yes", key1, verBody, 404, CodeNotFound},
		{"p with extra", "/api/v1/calls/p-3-4/approve", key1, verBody, 404, CodeNotFound},
		{"s id", "/api/v1/calls/s-project-checks/approve", key1, verBody, 404, CodeNotFound},
		{"too big body", ans, key1, `{"text":"` + strings.Repeat("a", 17<<10) + `","version":"` + v64 + `"}`, 413, CodePayloadTooLarge},
		// text rules
		{"4097 bytes", ans, key1, answerBody(strings.Repeat("a", 4097)), 413, CodePayloadTooLarge},
		{"2 byte char over", ans, key1, answerBody(strings.Repeat("a", 4095) + "é"), 413, CodePayloadTooLarge},
		{"empty text", ans, key1, answerBody(""), 422, CodeUnprocessable},
		{"spaces only", ans, key1, answerBody(" \n\t "), 422, CodeUnprocessable},
		{"NUL", ans, key1, answerBody("a\x00b"), 422, CodeUnprocessable},
		{"ESC", ans, key1, answerBody("a\x1bb"), 422, CodeUnprocessable},
		{"CR", ans, key1, answerBody("a\rb"), 422, CodeUnprocessable},
		{"CRLF", ans, key1, answerBody("a\r\nb"), 422, CodeUnprocessable},
		{"DEL", ans, key1, answerBody("a\x7fb"), 422, CodeUnprocessable},
		{"C1", ans, key1, answerBody("a\u0085b"), 422, CodeUnprocessable},
		{"RLO", ans, key1, answerBody("a\u202eb"), 422, CodeUnprocessable},
		{"isolate", ans, key1, answerBody("a\u2066b"), 422, CodeUnprocessable},
		{"LRM", ans, key1, answerBody("a\u200eb"), 422, CodeUnprocessable},
		{"ZWSP", ans, key1, answerBody("a\u200bb"), 422, CodeUnprocessable},
		{"ZWJ", ans, key1, answerBody("a\u200db"), 422, CodeUnprocessable},
		{"ZWNJ", ans, key1, answerBody("a\u200cb"), 422, CodeUnprocessable},
		{"word joiner", ans, key1, answerBody("a\u2060b"), 422, CodeUnprocessable},
		{"BOM", ans, key1, answerBody("a\ufeffb"), 422, CodeUnprocessable},
		{"line separator", ans, key1, answerBody("a\u2028b"), 422, CodeUnprocessable},
		{"lone surrogate", ans, key1, `{"text":"a\ud800b","version":"` + v64 + `"}`, 422, CodeUnprocessable},
		{"invalid utf8", ans, key1, "{\"text\":\"a\xffb\",\"version\":\"" + v64 + "\"}", 422, CodeUnprocessable},
	}
	for _, c := range cases {
		rec := post(e, c.path, c.idem, c.body)
		assertErr(t, rec, c.status, c.code, c.name)
	}
	// two Idempotency-Key headers
	hr := httptest.NewRequest("POST", ans, strings.NewReader(good))
	hr.Host = "127.0.0.1:18765"
	hr.Header.Set("Authorization", e.auth()["Authorization"])
	hr.Header.Set("Content-Type", "application/json")
	hr.Header["Idempotency-Key"] = []string{key1, key2}
	assertErr(t, serve(e, hr), 400, CodeBadIdempotencyKey, "two keys")
	if fa.calls() != 0 {
		t.Fatalf("Actions called %d times for refused requests", fa.calls())
	}
	// the limits themselves are allowed
	for name, text := range map[string]string{"exactly 4096": strings.Repeat("a", 4096), "tab and newline": "a\tb\nc", "unicode": "café 中文"} {
		if rec := post(e, ans, "key-"+fmt.Sprintf("%016d", len(name)), answerBody(text)); rec.Code != 200 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	found := false
	for _, a := range fa.answers {
		found = found || a.Text == strings.Repeat("a", 4096)
	}
	if !found {
		t.Errorf("the text was changed on the way")
	}
}

// Refusals from Actions have a fixed status and body, and no error text.
func TestActionErrorsMapToFixedCodesAndLeakNothing(t *testing.T) {
	e, fa := actEnv(t)
	k := e.key()
	secret := fmt.Errorf("token %s at /home/someone/.ssh: %w", k, ErrHostFailed)
	for i, c := range []struct {
		err    error
		status int
		code   Code
	}{
		{ErrStale, 409, CodeStale},
		{fmt.Errorf("wrapped %s: %w", k, ErrStale), 409, CodeStale},
		{ErrInvalid, 422, CodeUnprocessable},
		{secret, 502, CodeHostFailed},
		{ErrUnavailable, 503, CodeUnavailable},
		{errors.New("disk " + k + " /etc/passwd"), 500, CodeInternal},
		{context.DeadlineExceeded, 500, CodeInternal},
	} {
		err := c.err
		fa.hook = func(context.Context, Steps) (ActionResult, error) { return ActionResult{}, err }
		rec := post(e, actRoutes()[0].path, fmt.Sprintf("key-%016d", i), actRoutes()[0].body)
		assertErr(t, rec, c.status, c.code, fmt.Sprint(c.err))
		if strings.Contains(rec.Body.String(), k) || strings.Contains(e.logs.String(), k) || strings.Contains(e.logs.String(), "passwd") || strings.Contains(e.logs.String(), ".ssh") {
			t.Fatalf("error text leaked: body %q logs %q", rec.Body, e.logs.String())
		}
	}
	// A reply that is not done or pending is never believed.
	for i, res := range []ActionResult{{}, {Result: "maybe"}, {Result: ResultDone, JournalSeq: -1}, {Result: ResultRunning, JournalSeq: 1}} {
		res := res
		fa.hook = func(context.Context, Steps) (ActionResult, error) { return res, nil }
		assertErr(t, post(e, actRoutes()[1].path, fmt.Sprintf("key-%016d", 100+i), actRoutes()[1].body), 500, CodeInternal, fmt.Sprintf("result %+v", res))
	}
}

// Attacks 8 and 9: a double click, and a replay.
func TestSameKeyTwiceAtOnceRunsOnceThenReplays(t *testing.T) {
	e, fa := actEnv(t)
	release := make(chan struct{})
	fa.entered = make(chan struct{}, 4)
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		<-release
		return ActionResult{Result: ResultDone, JournalSeq: 3}, nil
	}
	r := actRoutes()[0]
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- post(e, r.path, key1, r.body) }()
	<-fa.entered // the first is inside Actions
	assertErr(t, post(e, r.path, key1, r.body), 409, CodeInProgress, "second press while running")
	close(release)
	rec := <-first
	if rec.Code != 200 {
		t.Fatalf("first: %d", rec.Code)
	}
	again := post(e, r.path, key1, r.body)
	if again.Code != 200 || again.Body.String() != rec.Body.String() {
		t.Fatalf("replay differs: %d %q vs %q", again.Code, again.Body, rec.Body)
	}
	if fa.calls() != 1 {
		t.Fatalf("Actions called %d times", fa.calls())
	}
}

// Contract point 1: one write at a time on a target, whatever the key.
func TestTwoWritesOnOneTargetWithDifferentKeysRunOnce(t *testing.T) {
	e, fa := actEnv(t)
	release := make(chan struct{})
	fa.entered = make(chan struct{}, 4)
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		<-release
		return ActionResult{Result: ResultDone, JournalSeq: 1}, nil
	}
	r := actRoutes()[0]
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- post(e, r.path, key1, r.body) }()
	<-fa.entered
	// another text, another key: still refused
	assertErr(t, post(e, r.path, key2, answerBody("other")), 409, CodeInProgress, "second answer, other key")
	// yes and no on one consequence share a target
	conseqDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { conseqDone <- post(e, actRoutes()[1].path, "key-cccccccccccccccc", actRoutes()[1].body) }()
	<-fa.entered
	assertErr(t, post(e, actRoutes()[2].path, "key-dddddddddddddddd", actRoutes()[2].body), 409, CodeInProgress, "no while yes runs")
	// a different target is not blocked
	other := make(chan *httptest.ResponseRecorder, 1)
	go func() { other <- post(e, actRoutes()[3].path, "key-eeeeeeeeeeeeeeee", actRoutes()[3].body) }()
	<-fa.entered
	close(release)
	for _, ch := range []chan *httptest.ResponseRecorder{first, conseqDone, other} {
		if rec := <-ch; rec.Code != 200 {
			t.Fatalf("got %d", rec.Code)
		}
	}
	if fa.calls() != 3 {
		t.Fatalf("Actions called %d times, want 3", fa.calls())
	}
}

// Attack 10.
func TestSameKeyOtherBodyOrRouteIsMismatchAndNothingRuns(t *testing.T) {
	e, fa := actEnv(t)
	r := actRoutes()[0]
	if rec := post(e, r.path, key1, r.body); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	assertErr(t, post(e, r.path, key1, answerBody("blue!")), 422, CodeIdempotencyMismatch, "other text")
	assertErr(t, post(e, "/api/v1/calls/q-5-10/answer", key1, r.body), 422, CodeIdempotencyMismatch, "other id")
	assertErr(t, post(e, actRoutes()[1].path, key1, actRoutes()[1].body), 422, CodeIdempotencyMismatch, "other route")
	if fa.calls() != 1 {
		t.Fatalf("calls = %d", fa.calls())
	}
}

// Contract point 3: a refused write is not kept; a host failure is, and
// carries on with the same steps.
func TestRefusedIsJudgedAgainAndHostFailedCarriesOnWithTheSameSteps(t *testing.T) {
	e, fa := actEnv(t)
	r := actRoutes()[0]
	n := 0
	fa.hook = func(_ context.Context, steps Steps) (ActionResult, error) {
		n++
		switch n {
		case 1:
			return ActionResult{}, ErrStale
		case 2:
			steps.Mark("post", "comment-77")
			return ActionResult{}, ErrHostFailed
		default:
			if id, ok := steps.Done("post"); !ok || id != "comment-77" {
				t.Errorf("the finished step was lost: %q %v", id, ok)
			}
			return ActionResult{Result: ResultDone, JournalSeq: 9}, nil
		}
	}
	assertErr(t, post(e, r.path, key1, r.body), 409, CodeStale, "refused")
	assertErr(t, post(e, r.path, key1, r.body), 502, CodeHostFailed, "host failed")
	if rec := post(e, r.path, key1, r.body); rec.Code != 200 {
		t.Fatalf("carry on: %d", rec.Code)
	}
	if fa.calls() != 3 {
		t.Fatalf("calls = %d", fa.calls())
	}
	// a new key does not see the old key's steps
	n = 1
	fa.hook = func(_ context.Context, steps Steps) (ActionResult, error) {
		if _, ok := steps.Done("post"); ok {
			t.Error("steps of another key were visible")
		}
		return ActionResult{Result: ResultDone}, nil
	}
	post(e, "/api/v1/calls/q-5-11/answer", key2, r.body)
}

// Contract point 3: the table is bounded, finished keys go first, running ones never.
func TestKeyTableIsBounded(t *testing.T) {
	e, fa := actEnv(t)
	e.s.idem.max = 3
	release := make(chan struct{})
	fa.entered = make(chan struct{}, 8)
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		<-release
		return ActionResult{Result: ResultDone}, nil
	}
	var chans []chan *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		ch := make(chan *httptest.ResponseRecorder, 1)
		chans = append(chans, ch)
		id := fmt.Sprintf("/api/v1/calls/q-%d-9/answer", i+1)
		go func(k string) { ch <- post(e, id, k, r0body) }(fmt.Sprintf("key-%016d", i))
		<-fa.entered
	}
	// all three are running: a fourth write gets 503
	assertErr(t, post(e, "/api/v1/calls/q-9-9/answer", "key-ffffffffffffffff", r0body), 503, CodeUnavailable, "table full of running keys")
	close(release)
	for _, ch := range chans {
		<-ch
	}
	// all finished: a new key drops the oldest finished one
	if rec := post(e, "/api/v1/calls/q-9-9/answer", "key-ffffffffffffffff", r0body); rec.Code != 200 {
		t.Fatalf("new key: %d", rec.Code)
	}
	if len(e.s.idem.recs) != 3 {
		t.Fatalf("table has %d records", len(e.s.idem.recs))
	}
	before := fa.calls()
	// the dropped key is judged as a new press: it runs again
	post(e, "/api/v1/calls/q-1-9/answer", "key-0000000000000000", r0body)
	if fa.calls() != before+1 {
		t.Fatalf("a dropped key was replayed")
	}
}

var r0body = answerBody("blue")

// Attack 41 and the 202 path.
func TestSlowWriteGets202ThenReplays(t *testing.T) {
	e, fa := actEnv(t)
	e.s.actionWait = 20 * time.Millisecond
	release := make(chan struct{})
	finished := make(chan struct{})
	fa.entered = make(chan struct{}, 4)
	fa.hook = func(ctx context.Context, _ Steps) (ActionResult, error) {
		<-release
		defer close(finished)
		return ActionResult{Result: ResultDone, JournalSeq: 4}, nil
	}
	r := actRoutes()[3]
	rec := post(e, r.path, key1, r.body)
	if rec.Code != 202 || strings.TrimSpace(rec.Body.String()) != `{"mode":"demo","result":"running"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	assertErr(t, post(e, r.path, key1, r.body), 409, CodeInProgress, "retry while running")
	assertErr(t, post(e, r.path, key2, r.body), 409, CodeInProgress, "other key while running")
	close(release)
	<-finished
	e.s.waitActions()
	again := post(e, r.path, key1, r.body)
	if again.Code != 200 || strings.TrimSpace(again.Body.String()) != `{"mode":"demo","result":"done","journalSeq":4}` {
		t.Fatalf("after: %d %s", again.Code, again.Body)
	}
	if fa.calls() != 1 {
		t.Fatalf("calls = %d, want one run", fa.calls())
	}
}

func TestWriteThatFailsAfter202KeepsItsKeyAsFailed(t *testing.T) {
	e, fa := actEnv(t)
	e.s.actionWait = 20 * time.Millisecond
	release := make(chan struct{})
	var n atomic.Int32
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		if n.Add(1) == 1 {
			<-release
			return ActionResult{}, ErrStale // a refusal, but after the 202
		}
		return ActionResult{Result: ResultDone, JournalSeq: 5}, nil
	}
	r := actRoutes()[3]
	if rec := post(e, r.path, key1, r.body); rec.Code != 202 {
		t.Fatalf("%d", rec.Code)
	}
	close(release)
	e.s.waitActions()
	// the same key is never silently "done": it runs again and is judged
	rec := post(e, r.path, key1, r.body)
	if n.Load() != 2 || rec.Code != 200 {
		t.Fatalf("calls %d status %d", n.Load(), rec.Code)
	}
}

// The context given to Actions does not end with the request.
func TestActionsContextOutlivesTheRequest(t *testing.T) {
	e, fa := actEnv(t)
	var seen error
	fa.hook = func(ctx context.Context, _ Steps) (ActionResult, error) {
		seen = ctx.Err()
		if _, ok := ctx.Deadline(); !ok {
			t.Error("no deadline")
		}
		return ActionResult{Result: ResultDone}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hr := httptest.NewRequest("POST", actRoutes()[3].path, strings.NewReader(actRoutes()[3].body)).WithContext(ctx)
	hr.Host = "127.0.0.1:18765"
	for k, v := range jsonHdr(e, key1) {
		hr.Header.Set(k, v)
	}
	if rec := serve(e, hr); rec.Code != 200 || seen != nil {
		t.Fatalf("%d %v", rec.Code, seen)
	}
}

// Contract point 5 and the closed list: every code has a sentence.
func TestEveryCodeHasASentenceOnTheScreen(t *testing.T) {
	var have []string
	for c := range knownCodes {
		have = append(have, string(c))
	}
	sort.Strings(have)
	want := append([]string(nil), floor.RefusalCodes()...)
	sort.Strings(want)
	if strings.Join(have, ",") != strings.Join(want, ",") {
		t.Fatalf("codes of the server: %v\ncodes with a sentence: %v", have, want)
	}
	p := floor.Phrases()
	for _, c := range have {
		if p[floor.PhraseKey("refusal."+c)] == "" {
			t.Errorf("no sentence for %s", c)
		}
	}
}

// Contract C: a failed write frees its target at once.
func TestFailedWriteFreesItsTarget(t *testing.T) {
	e, fa := actEnv(t)
	fa.hook = func(context.Context, Steps) (ActionResult, error) { return ActionResult{}, ErrHostFailed }
	r := actRoutes()[0]
	assertErr(t, post(e, r.path, key1, r.body), 502, CodeHostFailed, "first")
	fa.hook = nil
	if rec := post(e, r.path, key2, r.body); rec.Code != 200 {
		t.Fatalf("another key after a failure: %d %s", rec.Code, rec.Body)
	}
}

// Contract C: at the limit the oldest DONE key goes first, then the oldest
// FAILED one; a running key never.
func TestKeyTableDropsDoneBeforeFailed(t *testing.T) {
	e, fa := actEnv(t)
	e.s.idem.max = 2
	failNext := true
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		if failNext {
			failNext = false
			return ActionResult{}, ErrHostFailed
		}
		return ActionResult{Result: ResultDone}, nil
	}
	p := func(n int) string { return fmt.Sprintf("/api/v1/calls/q-%d-9/answer", n) }
	post(e, p(1), "key-1111111111111111", r0body) // failed, oldest
	post(e, p(2), "key-2222222222222222", r0body) // done
	post(e, p(3), "key-3333333333333333", r0body) // table full: drops the done one, not the failed one
	e.s.idem.mu.Lock()
	_, failedKept := e.s.idem.recs["key-1111111111111111"]
	_, doneKept := e.s.idem.recs["key-2222222222222222"]
	e.s.idem.mu.Unlock()
	if !failedKept || doneKept {
		t.Fatalf("failed kept %v, done kept %v", failedKept, doneKept)
	}
}

// Contract D: white space at both ends is removed before Actions sees it.
func TestAnswerTextIsTrimmed(t *testing.T) {
	e, fa := actEnv(t)
	post(e, actRoutes()[0].path, key1, answerBody("  \n blue one \t\n"))
	if len(fa.answers) != 1 || fa.answers[0].Text != "blue one" {
		t.Fatalf("%+v", fa.answers)
	}
}

// F5: an answer with nothing to see is empty.
func TestAnswersThatLookBlankAreRefused(t *testing.T) {
	e, fa := actEnv(t)
	for name, r := range map[string]rune{"hangul filler": 0x3164, "braille blank": 0x2800, "choseong filler": 0x115F,
		"halfwidth hangul filler": 0xFFA0, "combining grapheme joiner": 0x034F, "variation selector 16": 0xFE0F,
		"variation selector 17": 0xE0100, "mongolian vowel separator": 0x180E, "hangul jungseong filler": 0x1160,
		"nbsp": 0xA0, "ideographic space": 0x3000, "tag space": 0xE0020} {
		for _, text := range []string{string(r), " " + string(r) + " ", string(r) + string(r) + "\n"} {
			assertErr(t, post(e, "/api/v1/calls/q-5-9/answer", key1, answerBody(text)), 422, CodeUnprocessable, name)
		}
	}
	if fa.calls() != 0 {
		t.Fatalf("Actions called %d times", fa.calls())
	}
	// Visible text next to an invisible mark is still not refused for the mark alone.
	if rec := post(e, "/api/v1/calls/q-5-9/answer", key2, answerBody("ok \u00e9")); rec.Code != 200 {
		t.Fatalf("visible text: %d", rec.Code)
	}
}

// N5: one table of samples, shared with the page (web/src/lib/answer-samples.json).
func TestSharedAnswerSamples(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/lib/answer-samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var samples []struct {
		Name string `json:"name"`
		Text string `json:"text"`
		OK   bool   `json:"ok"`
	}
	if err := json.Unmarshal(raw, &samples); err != nil || len(samples) < 20 {
		t.Fatalf("samples: %v (%d)", err, len(samples))
	}
	for i, smp := range samples {
		e, fa := actEnv(t)
		rec := post(e, "/api/v1/calls/q-5-9/answer", fmt.Sprintf("key-%016d", i), answerBody(smp.Text))
		if smp.OK && (rec.Code != 200 || fa.calls() != 1) {
			t.Errorf("%s: %d %s", smp.Name, rec.Code, rec.Body)
		}
		if !smp.OK {
			assertErr(t, rec, 422, CodeUnprocessable, smp.Name)
			if fa.calls() != 0 {
				t.Errorf("%s: Actions was called", smp.Name)
			}
		}
	}
}
