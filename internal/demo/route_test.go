package demo

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

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
