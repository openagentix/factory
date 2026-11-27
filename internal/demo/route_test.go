package demo

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/server"
)

// The whole way: a press over HTTP reaches the real demo, and a replay of the
// press does nothing more.
func TestAnswerOverTheServerAndItsReplay(t *testing.T) {
	d := beatsOnly(t, "write_checks", "question")
	srv, err := server.New(server.Config{Port: 18766, Mode: "demo", Source: d, Actions: d, Static: fstest.MapFS{"index.html": {Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	key := strings.SplitN(srv.Link(), "#t=", 2)[1]
	c := mustCall(t, d, "q-5-")
	body := `{"text":"Yes, allow it.","version":"` + c.Version + `"}`
	press := func(idem string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/calls/"+c.ID+"/answer", strings.NewReader(body))
		r.Host = "127.0.0.1:18766"
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", idem)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	first := press("press-aaaaaaaaaaaaaaaa")
	if first.Code != 200 || !strings.HasPrefix(first.Body.String(), `{"mode":"demo","result":"done","journalSeq":`) {
		t.Fatalf("first: %d %s", first.Code, first.Body)
	}
	again := press("press-aaaaaaaaaaaaaaaa")
	if again.Code != 200 || again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	if n := countKindOf(comments(t, d, 5), githost.KindAnswer); n != 1 {
		t.Fatalf("answer comments = %d", n)
	}
	other := press("press-bbbbbbbbbbbbbbbb") // a new key: the call is gone
	if other.Code != 409 || strings.TrimSpace(other.Body.String()) != `{"error":"stale"}` {
		t.Fatalf("new key: %d %s", other.Code, other.Body)
	}
	if countKind(t, d, "answered") != 1 {
		t.Fatal("one answered entry expected")
	}
	srv.Handler() // the server holds no goroutine of its own
}

// The whole way for the holding area: the piece detail, the undo preview, the
// Undo and the Accept over HTTP, against the real demo.
func TestHoldingAreaOverTheServer(t *testing.T) {
	d := beatsOnly(t, "write_checks", "build_move", "checks_fail", "second_try")
	srv, err := server.New(server.Config{Port: 18767, Mode: "demo", Source: d, Actions: d, Detail: d, Static: fstest.MapFS{"index.html": {Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	key := strings.SplitN(srv.Link(), "#t=", 2)[1]
	do := func(method, path, idem, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:18767"
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("Authorization", "Bearer "+key)
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", idem)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	det := do("GET", "/api/v1/pieces/t3", "", "")
	var got struct {
		Mode string         `json:"mode"`
		Undo *floor.UndoRef `json:"undo"`
	}
	if det.Code != 200 || json.Unmarshal(det.Body.Bytes(), &got) != nil || got.Mode != "demo" || got.Undo == nil {
		t.Fatalf("piece: %d %s", det.Code, det.Body)
	}
	if rec := do("GET", "/api/v1/pieces/t99", "", ""); rec.Code != 404 {
		t.Fatalf("unknown piece: %d", rec.Code)
	}
	pv := do("GET", "/api/v1/changes/"+got.Undo.Change+"/undo-preview", "", "")
	var view struct {
		Mode        string   `json:"mode"`
		Change      string   `json:"change"`
		StagingSeq  int64    `json:"stagingSeq"`
		Commit      string   `json:"commit"`
		AlsoRemoved []string `json:"alsoRemoved"`
		Tickets     []struct {
			Change string `json:"change"`
			Ticket int    `json:"ticket"`
		} `json:"tickets"`
	}
	if pv.Code != 200 || json.Unmarshal(pv.Body.Bytes(), &view) != nil || view.Mode != "demo" || len(view.AlsoRemoved) != 1 || len(view.Tickets) != 2 ||
		view.Tickets[0].Ticket != 3 || view.Tickets[1].Ticket != 4 || view.StagingSeq != got.Undo.StagingSeq || view.Commit != got.Undo.Commit {
		t.Fatalf("preview: %d %s", pv.Code, pv.Body)
	}
	// The change that depends on the first is offered too, and undoing it alone is refused with a wrong list.
	body, _ := json.Marshal(map[string]any{"stagingSeq": view.StagingSeq, "commit": view.Commit, "change": view.Change, "alsoRemoved": []string{}})
	if rec := do("POST", "/api/v1/changes/"+view.Change+"/undo", "undo-aaaaaaaaaaaaaaaa", string(body)); rec.Code != 409 || strings.TrimSpace(rec.Body.String()) != `{"error":"stale"}` {
		t.Fatalf("wrong list: %d %s", rec.Code, rec.Body)
	}
	body, _ = json.Marshal(map[string]any{"stagingSeq": view.StagingSeq, "commit": view.Commit, "change": view.Change, "alsoRemoved": view.AlsoRemoved})
	first := do("POST", "/api/v1/changes/"+view.Change+"/undo", "undo-bbbbbbbbbbbbbbbb", string(body))
	if first.Code != 200 || !strings.HasPrefix(first.Body.String(), `{"mode":"demo","result":"done","journalSeq":`) {
		t.Fatalf("undo: %d %s", first.Code, first.Body)
	}
	if again := do("POST", "/api/v1/changes/"+view.Change+"/undo", "undo-bbbbbbbbbbbbbbbb", string(body)); again.Code != 200 || again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	if n := countKind(t, d, "undone"); n != 2 {
		t.Fatalf("undone entries = %d", n)
	}
	// What is left is the checks only: there is no Accept call, so an Accept is stale.
	acc, _ := json.Marshal(map[string]any{"stagingSeq": view.StagingSeq + 1, "commit": strings.Repeat("a", 40), "version": strings.Repeat("a", 64)})
	if rec := do("POST", "/api/v1/holding-area/accept", "acc-cccccccccccccccc", string(acc)); rec.Code != 409 {
		t.Fatalf("accept with nothing to accept: %d %s", rec.Code, rec.Body)
	}
}
