package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var c40 = strings.Repeat("c", 40)

func holdBody() string {
	return `{"stagingSeq":7,"commit":"` + c40 + `","version":"` + v64 + `"}`
}

func undoBody(change string, also ...string) string {
	b, _ := json.Marshal(map[string]any{"stagingSeq": 7, "commit": c40, "change": change, "alsoRemoved": append([]string{}, also...)})
	return string(b)
}

func get(e *testEnv, path string) *httptest.ResponseRecorder {
	return e.do(req{path: path, hdr: e.auth()})
}

func TestAcceptAndCancelPassTheCheckedValues(t *testing.T) {
	e, fa := actEnv(t)
	if rec := post(e, "/api/v1/holding-area/accept", key1, holdBody()); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := post(e, "/api/v1/holding-area/cancel", key2, holdBody()); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := HoldingRequest{StagingSeq: 7, Commit: c40, Version: v64}
	for _, got := range []HoldingRequest{fa.accepts[0], fa.cancels[0]} {
		if got.Steps == nil {
			t.Error("no steps")
		}
		got.Steps = nil
		if got != want {
			t.Errorf("got %+v", got)
		}
	}
}

func TestAcceptCanAnswerPending(t *testing.T) {
	e, fa := actEnv(t)
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		return ActionResult{Result: ResultPending, JournalSeq: 4}, nil
	}
	rec := post(e, "/api/v1/holding-area/accept", key1, holdBody())
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != 200 || got != `{"mode":"demo","result":"pending","journalSeq":4}` {
		t.Fatalf("%d %s", rec.Code, got)
	}
}

// Attack 4: an old state is stale, and the refusal is the fixed code.
func TestAttack4_StagingRefusalsAreStale(t *testing.T) {
	e, fa := actEnv(t)
	fa.hook = func(context.Context, Steps) (ActionResult, error) { return ActionResult{}, ErrStale }
	for i, r := range []actRoute{actRoutes()[4], actRoutes()[5], actRoutes()[6]} {
		assertErr(t, post(e, r.path, fmt.Sprintf("key-%016d", i), r.body), 409, CodeStale, r.name)
	}
	fa.hook = func(context.Context, Steps) (ActionResult, error) { return ActionResult{}, ErrHostFailed }
	assertErr(t, post(e, "/api/v1/holding-area/accept", "key-zzzzzzzzzzzzzzzz", holdBody()), 502, CodeHostFailed, "host")
}

func TestHoldingBodiesAreChecked(t *testing.T) {
	e, fa := actEnv(t)
	mk := func(seq, commit, ver string) string {
		return `{"stagingSeq":` + seq + `,"commit":` + commit + `,"version":` + ver + `}`
	}
	q := func(s string) string { return `"` + s + `"` }
	bad := map[string]string{
		"empty":            ``,
		"empty object":     `{}`,
		"no seq":           `{"commit":"` + c40 + `","version":"` + v64 + `"}`,
		"no commit":        `{"stagingSeq":7,"version":"` + v64 + `"}`,
		"no version":       `{"stagingSeq":7,"commit":"` + c40 + `"}`,
		"seq zero":         mk("0", q(c40), q(v64)),
		"seq negative":     mk("-3", q(c40), q(v64)),
		"seq string":       mk(q("7"), q(c40), q(v64)),
		"seq fraction":     mk("7.5", q(c40), q(v64)),
		"seq huge":         mk("99999999999999999999", q(c40), q(v64)),
		"seq null":         mk("null", q(c40), q(v64)),
		"commit short":     mk("7", q(c40[:39]), q(v64)),
		"commit upper":     mk("7", q(strings.ToUpper(c40)), q(v64)),
		"commit long":      mk("7", q(c40+"c"), q(v64)),
		"commit blank":     mk("7", q(""), q(v64)),
		"commit number":    mk("7", "5", q(v64)),
		"version short":    mk("7", q(c40), q(v64[:63])),
		"version upper":    mk("7", q(c40), q(strings.ToUpper(v64))),
		"unknown key":      `{"stagingSeq":7,"commit":"` + c40 + `","version":"` + v64 + `","also":[]}`,
		"repeated key":     `{"stagingSeq":7,"stagingSeq":8,"commit":"` + c40 + `","version":"` + v64 + `"}`,
		"case twin":        `{"stagingSeq":7,"StagingSeq":8,"commit":"` + c40 + `","version":"` + v64 + `"}`,
		"not an object":    `[]`,
		"trailing garbage": holdBody() + `{}`,
	}
	for name, body := range bad {
		for _, path := range []string{"/api/v1/holding-area/accept", "/api/v1/holding-area/cancel"} {
			assertErr(t, post(e, path, key1, body), 400, CodeBadRequest, name+" "+path)
		}
	}
	assertErr(t, post(e, "/api/v1/holding-area/accept", "", holdBody()), 400, CodeBadIdempotencyKey, "no key")
	assertErr(t, post(e, "/api/v1/holding-area/accept", "short", holdBody()), 400, CodeBadIdempotencyKey, "short key")
	if fa.calls() != 0 {
		t.Fatalf("Actions called %d times", fa.calls())
	}
}

func TestUndoBodiesAreChecked(t *testing.T) {
	e, fa := actEnv(t)
	path := "/api/v1/changes/run-1/undo"
	many := make([]string, 51)
	for i := range many {
		many[i] = fmt.Sprintf("run-%d", i+2)
	}
	good := func(change, also string) string {
		return `{"stagingSeq":7,"commit":"` + c40 + `","change":"` + change + `","alsoRemoved":` + also + `}`
	}
	manyJSON, _ := json.Marshal(many)
	bad := map[string]string{
		"change differs from the route": good("run-9", `[]`),
		"change blank":                  good("", `[]`),
		"also missing":                  `{"stagingSeq":7,"commit":"` + c40 + `","change":"run-1"}`,
		"also null":                     good("run-1", `null`),
		"also not a list":               good("run-1", `"run-2"`),
		"also holds a number":           good("run-1", `[5]`),
		"also holds null":               good("run-1", `[null]`),
		"also holds a bad id":           good("run-1", `["run 2"]`),
		"also holds a dotted id":        good("run-1", `["run.2"]`),
		"also holds a long id":          good("run-1", `["`+strings.Repeat("a", 65)+`"]`),
		"also holds a blank":            good("run-1", `[""]`),
		"fifty one":                     good("run-1", string(manyJSON)),
		"a version is not sent":         `{"stagingSeq":7,"commit":"` + c40 + `","change":"run-1","alsoRemoved":[],"version":"` + v64 + `"}`,
		"seq zero":                      `{"stagingSeq":0,"commit":"` + c40 + `","change":"run-1","alsoRemoved":[]}`,
		"commit short":                  `{"stagingSeq":7,"commit":"abc","change":"run-1","alsoRemoved":[]}`,
		"empty":                         ``,
	}
	for name, body := range bad {
		assertErr(t, post(e, path, key1, body), 400, CodeBadRequest, name)
	}
	if fa.calls() != 0 {
		t.Fatalf("Actions called %d times", fa.calls())
	}
	// Ids that the route does not take are 404 before anything else.
	for _, p := range []string{"/api/v1/changes/run.1/undo", "/api/v1/changes/%2e%2e/undo", "/api/v1/changes/" + strings.Repeat("a", 65) + "/undo"} {
		assertErr(t, post(e, p, key1, undoBody("run-1")), 404, CodeNotFound, p)
	}
	// Fifty is the most.
	fifty, _ := json.Marshal(many[:50])
	if rec := post(e, path, key2, good("run-1", string(fifty))); rec.Code != 200 {
		t.Fatalf("fifty: %d %s", rec.Code, rec.Body)
	}
}

// Attack 22: the list is passed on exactly, in the same order, with the state.
func TestUndoPassesTheListInOrder(t *testing.T) {
	e, fa := actEnv(t)
	rec := post(e, "/api/v1/changes/run-1/undo", key1, undoBody("run-1", "run-5", "run-2", "run-9"))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := fa.undos[0]
	if got.Change != "run-1" || got.StagingSeq != 7 || got.Commit != c40 || !reflect.DeepEqual(got.AlsoRemoved, []string{"run-5", "run-2", "run-9"}) || got.Steps == nil {
		t.Fatalf("%+v", got)
	}
	// An empty list is passed as empty, never nil-turned-missing.
	post(e, "/api/v1/changes/run-1/undo", key2, undoBody("run-1"))
	if fa.undos[1].AlsoRemoved == nil || len(fa.undos[1].AlsoRemoved) != 0 {
		t.Fatalf("%#v", fa.undos[1].AlsoRemoved)
	}
}

// The holding area is one target: Accept, Cancel and Undo do not overlap.
func TestHoldingWritesShareOneTarget(t *testing.T) {
	e, fa := actEnv(t)
	fa.entered = make(chan struct{}, 4)
	release := make(chan struct{})
	fa.hook = func(ctx context.Context, _ Steps) (ActionResult, error) {
		<-release
		return ActionResult{Result: ResultDone, JournalSeq: 1}, nil
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- post(e, "/api/v1/holding-area/accept", key1, holdBody()) }()
	<-fa.entered
	assertErr(t, post(e, "/api/v1/holding-area/cancel", key2, holdBody()), 409, CodeInProgress, "cancel while accept")
	assertErr(t, post(e, "/api/v1/changes/run-1/undo", "key-cccccccccccccccc", undoBody("run-1")), 409, CodeInProgress, "undo while accept")
	assertErr(t, post(e, "/api/v1/holding-area/accept", "key-dddddddddddddddd", holdBody()), 409, CodeInProgress, "accept twice")
	close(release)
	if rec := <-first; rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	if fa.calls() != 1 {
		t.Fatalf("Actions called %d times", fa.calls())
	}
}

// Attack 8, 9, 10 for Accept: a double click is one Accept, a replay is the
// same bytes, and the same key with another state is a mismatch.
func TestAcceptIsIdempotent(t *testing.T) {
	e, fa := actEnv(t)
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 4)
	for i := range recs {
		wg.Add(1)
		go func() { defer wg.Done(); recs[i] = post(e, "/api/v1/holding-area/accept", key1, holdBody()) }()
	}
	wg.Wait()
	done := 0
	for _, r := range recs {
		if r.Code == 200 {
			done++
		} else if r.Code != 409 {
			t.Errorf("%d %s", r.Code, r.Body)
		}
	}
	if done == 0 || len(fa.accepts) != 1 {
		t.Fatalf("done %d, accepts %d", done, len(fa.accepts))
	}
	again := post(e, "/api/v1/holding-area/accept", key1, holdBody())
	if again.Code != 200 || len(fa.accepts) != 1 {
		t.Fatalf("replay: %d, accepts %d", again.Code, len(fa.accepts))
	}
	other := strings.Replace(holdBody(), `"stagingSeq":7`, `"stagingSeq":8`, 1)
	assertErr(t, post(e, "/api/v1/holding-area/accept", key1, other), 422, CodeIdempotencyMismatch, "other state")
}

// Attack 41: a slow undo answers 202, a retry is in_progress, then the replay.
func TestSlowUndoAnswers202AndRunsOnce(t *testing.T) {
	e, fa := actEnv(t)
	e.s.actionWait = 20 * time.Millisecond
	release := make(chan struct{})
	fa.entered = make(chan struct{}, 4)
	fa.hook = func(context.Context, Steps) (ActionResult, error) {
		<-release
		return ActionResult{Result: ResultDone, JournalSeq: 3}, nil
	}
	rec := post(e, "/api/v1/changes/run-1/undo", key1, undoBody("run-1"))
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != 202 || got != `{"mode":"demo","result":"running"}` {
		t.Fatalf("%d %s", rec.Code, got)
	}
	assertErr(t, post(e, "/api/v1/changes/run-1/undo", key1, undoBody("run-1")), 409, CodeInProgress, "retry while running")
	close(release)
	e.s.waitActions()
	again := post(e, "/api/v1/changes/run-1/undo", key1, undoBody("run-1"))
	if again.Code != 200 || len(fa.undos) != 1 {
		t.Fatalf("replay %d, undos %d", again.Code, len(fa.undos))
	}
}

func TestUndoPreviewRoute(t *testing.T) {
	e, fa := actEnv(t)
	fa.previewOut = UndoPreviewView{Change: "run-1", StagingSeq: 7, Commit: c40, AlsoRemoved: []string{"run-2"},
		Tickets: []UndoTicket{{Change: "run-1", Ticket: 4}, {Change: "run-2", Ticket: 5}}}
	rec := get(e, "/api/v1/changes/run-1/undo-preview")
	want := `{"mode":"demo","change":"run-1","stagingSeq":7,"commit":"` + c40 + `","alsoRemoved":["run-2"],"tickets":[{"change":"run-1","ticket":4},{"change":"run-2","ticket":5}]}`
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	assertSecure(t, rec, true, "preview")
	if !reflect.DeepEqual(fa.previews, []string{"run-1"}) {
		t.Fatalf("%v", fa.previews)
	}
	// An empty list is [], never null.
	fa.previewOut = UndoPreviewView{Change: "run-1", StagingSeq: 7, Commit: c40, Tickets: []UndoTicket{{Change: "run-1", Ticket: 4}}}
	if body := get(e, "/api/v1/changes/run-1/undo-preview").Body.String(); !strings.Contains(body, `"alsoRemoved":[]`) {
		t.Fatalf("%s", body)
	}
}

func TestUndoPreviewRefusals(t *testing.T) {
	e, fa := actEnv(t)
	fa.previewErr = ErrStale
	assertErr(t, get(e, "/api/v1/changes/run-1/undo-preview"), 409, CodeStale, "stale")
	fa.previewErr = ErrHostFailed
	assertErr(t, get(e, "/api/v1/changes/run-1/undo-preview"), 502, CodeHostFailed, "host")
	fa.previewErr = errors.New("secret path /home/x and key abc")
	rec := get(e, "/api/v1/changes/run-1/undo-preview")
	assertErr(t, rec, 500, CodeInternal, "internal")
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(e.logs.String(), "secret") {
		t.Error("error text leaked")
	}
	n := len(fa.previews)
	for _, p := range []string{"/api/v1/changes/run.1/undo-preview", "/api/v1/changes/%2e%2e/undo-preview"} {
		assertErr(t, get(e, p), 404, CodeNotFound, p)
	}
	// The key, the host and the method are guarded.
	assertErr(t, e.do(req{path: "/api/v1/changes/run-1/undo-preview"}), 401, CodeUnauthorized, "no key")
	assertErr(t, e.do(req{path: "/api/v1/changes/run-1/undo-preview", host: "evil.test:18765", hdr: e.auth()}), 421, CodeMisdirected, "rebinding")
	assertErr(t, e.do(req{method: "POST", path: "/api/v1/changes/run-1/undo-preview", hdr: jsonHdr(e, key1), body: "{}"}), 405, CodeMethodNotAllowed, "post")
	if len(fa.previews) != n {
		t.Error("a guarded request reached Actions")
	}
	// No Actions: 503.
	assertErr(t, get(newEnv(t), "/api/v1/changes/run-1/undo-preview"), 503, CodeUnavailable, "no actions")
}

// fakeDetail answers the piece route.
type fakeDetail struct {
	body []byte
	ok   bool
	err  error
	ids  []string
}

func (f *fakeDetail) Piece(_ context.Context, id string) ([]byte, bool, error) {
	f.ids = append(f.ids, id)
	return f.body, f.ok, f.err
}

func TestPieceRoute(t *testing.T) {
	fd := &fakeDetail{body: []byte(`{"schemaVersion":1,"piece":{"id":"t57"},"undo":null}`), ok: true}
	e := newEnv(t, func(c *Config) { c.Detail = fd })
	rec := get(e, "/api/v1/pieces/t57")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != string(fd.body) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	assertSecure(t, rec, true, "piece")
	for _, id := range []string{"p40", "k40"} {
		if get(e, "/api/v1/pieces/"+id).Code != 200 {
			t.Errorf("%s", id)
		}
	}
	n := len(fd.ids)
	for _, id := range []string{"x5", "t0", "t05", "T5", "t", "tt5", "t57.1", "%2e%2e", "t5700000000000", "t-1"} {
		assertErr(t, get(e, "/api/v1/pieces/"+id), 404, CodeNotFound, id)
	}
	if len(fd.ids) != n {
		t.Errorf("a bad id reached the detail: %v", fd.ids[n:])
	}
	assertErr(t, e.do(req{path: "/api/v1/pieces/t57"}), 401, CodeUnauthorized, "no key")
	fd.ok = false
	assertErr(t, get(e, "/api/v1/pieces/t58"), 404, CodeNotFound, "unknown piece")
	fd.ok, fd.err = true, errors.New("secret detail text")
	rec = get(e, "/api/v1/pieces/t57")
	assertErr(t, rec, 500, CodeReadFailed, "read failed")
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(e.logs.String(), "secret") {
		t.Error("error text leaked")
	}
	fd.err = ErrUnavailable // the demo is not ready yet
	assertErr(t, get(e, "/api/v1/pieces/t57"), 503, CodeUnavailable, "not ready")
	fd.err, fd.body = nil, []byte(`not json`)
	assertErr(t, get(e, "/api/v1/pieces/t57"), 500, CodeReadFailed, "not json")
	assertErr(t, get(newEnv(t), "/api/v1/pieces/t57"), 503, CodeUnavailable, "no detail")
}
