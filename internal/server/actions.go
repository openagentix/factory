package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Actions carries out what a person asked for in the web app. The server
// believes nothing the browser sent: it checks only the shape of a request,
// and the implementation decides again, on facts read fresh from the git host
// and the stores, under the same lock that carries the action out.
type Actions interface {
	Answer(ctx context.Context, r AnswerRequest) (ActionResult, error)
	Consequence(ctx context.Context, r ConsequenceRequest) (ActionResult, error)
	Approve(ctx context.Context, r ApproveRequest) (ActionResult, error)
}

// Steps is the progress of the publish steps of one Idempotency-Key, kept by
// the server in memory. A step is marked with the host's id only after the
// host did it.
type Steps interface {
	Done(slot string) (hostID string, ok bool)
	Mark(slot, hostID string)
}

// AnswerRequest answers one question. Text is trimmed and checked.
type AnswerRequest struct {
	Call, Version, Text string
	Steps               Steps
}

// ConsequenceRequest is a yes or a no on one consequence.
type ConsequenceRequest struct {
	Call, Version string
	Yes           bool
	Steps         Steps
}

// ApproveRequest approves one plan.
type ApproveRequest struct {
	Call, Version string
	Steps         Steps
}

// ResultKind says how a write ended.
type ResultKind string

// The kinds of result. "pending" is an Accept that waits for a developer.
const (
	ResultDone    ResultKind = "done"
	ResultPending ResultKind = "pending"
	ResultRunning ResultKind = "running"
)

// ActionResult is what Actions gives back for a write that was done.
// JournalSeq is the highest journal entry the write wrote.
type ActionResult struct {
	Result     ResultKind
	JournalSeq int64
}

// The refusals Actions may return. Anything else is an internal error.
var (
	ErrStale       = errors.New("server: this changed")
	ErrInvalid     = errors.New("server: cannot be sent")
	ErrHostFailed  = errors.New("server: the git host failed")
	ErrUnavailable = errors.New("server: not ready")
	ErrInProgress  = errors.New("server: being done already")
)

func mapError(err error) (int, Code) {
	switch {
	case errors.Is(err, ErrStale):
		return http.StatusConflict, CodeStale
	case errors.Is(err, ErrInProgress):
		return http.StatusConflict, CodeInProgress
	case errors.Is(err, ErrInvalid):
		return http.StatusUnprocessableEntity, CodeUnprocessable
	case errors.Is(err, ErrHostFailed):
		return http.StatusBadGateway, CodeHostFailed
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable, CodeUnavailable
	}
	return http.StatusInternalServerError, CodeInternal
}

const (
	maxAnswerBytes = 4096
	actionTimeout  = 2 * time.Minute
)

var (
	questionID    = regexp.MustCompile(`^q-[1-9][0-9]{0,8}-[1-9][0-9]{0,17}$`)
	consequenceID = regexp.MustCompile(`^c-[1-9][0-9]{0,8}-[A-Za-z0-9_-]{1,64}$`)
	planID        = regexp.MustCompile(`^p-[1-9][0-9]{0,8}$`)
	versionRE     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idemKeyRE     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
)

type bodySumKey struct{}

type answerJSON struct {
	Text    *string `json:"text"`
	Version *string `json:"version"`
}

type versionBody struct {
	Version *string `json:"version"`
}

func (s *Server) registerActions() {
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/calls/{id}/answer",
		NewBody: func() any { return &answerJSON{} }, Handler: s.handleAnswer})
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/calls/{id}/yes",
		NewBody: func() any { return &versionBody{} }, Handler: s.handleConsequence(true)})
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/calls/{id}/no",
		NewBody: func() any { return &versionBody{} }, Handler: s.handleConsequence(false)})
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/calls/{id}/approve",
		NewBody: func() any { return &versionBody{} }, Handler: s.handleApprove})
}

// checkText applies the answer rules of record 0004. The size is counted in
// bytes as sent. White space at both ends is removed first; a text that is
// empty then, is not valid UTF-8, or holds a control, direction, zero-width
// or other format character (a carriage return too) is refused.
func checkText(text string) (string, int, Code) {
	if len(text) > maxAnswerBytes {
		return "", http.StatusRequestEntityTooLarge, CodePayloadTooLarge
	}
	t := strings.TrimSpace(text)
	if t == "" || !utf8.ValidString(t) {
		return "", http.StatusUnprocessableEntity, CodeUnprocessable
	}
	visible := false
	for _, r := range t {
		if shows(r) {
			visible = true
		}
		switch {
		case r == '\n' || r == '\t':
		case r == utf8.RuneError, // a lone surrogate or a bad byte, replaced by the decoder
			unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == ' ', r == ' ', r == '᠎':
			return "", http.StatusUnprocessableEntity, CodeUnprocessable
		}
	}
	if !visible {
		return "", http.StatusUnprocessableEntity, CodeUnprocessable // nothing a person can see
	}
	return t, 0, ""
}

// shows says whether a code point is something a person can see as a mark of
// its own: a letter, number, punctuation or symbol that is not a filler, a
// joiner, a variation selector, a default-ignorable code point, the object
// replacement or replacement character, the blank braille pattern or the
// musical null notehead. Marks, private use, unassigned code points and
// surrogates never count.
func shows(r rune) bool {
	switch r {
	case '\uFFFC', '\uFFFD', '\u2800', 0x1D159:
		return false
	}
	if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Variation_Selector, r) ||
		unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) {
		return false
	}
	return unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S)
}

func (s *Server) idemKey(r *http.Request) (string, bool) {
	v := r.Header["Idempotency-Key"]
	if len(v) != 1 || !idemKeyRE.MatchString(v[0]) {
		return "", false
	}
	return v[0], true
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request, body any) {
	id := r.PathValue("id")
	if !questionID.MatchString(id) {
		WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	b, _ := body.(*answerJSON)
	if b == nil || b.Text == nil || b.Version == nil || !versionRE.MatchString(*b.Version) {
		WriteError(w, http.StatusBadRequest, CodeBadRequest)
		return
	}
	key, ok := s.idemKey(r)
	if !ok {
		WriteError(w, http.StatusBadRequest, CodeBadIdempotencyKey)
		return
	}
	text, status, code := checkText(*b.Text)
	if code != "" {
		WriteError(w, status, code)
		return
	}
	version := *b.Version
	s.write(w, r, key, "q:"+id, func(ctx context.Context, st Steps) (ActionResult, error) {
		return s.cfg.Actions.Answer(ctx, AnswerRequest{Call: id, Version: version, Text: text, Steps: st})
	})
}

func (s *Server) handleConsequence(yes bool) ActionFunc {
	return func(w http.ResponseWriter, r *http.Request, body any) {
		id := r.PathValue("id")
		if !consequenceID.MatchString(id) {
			WriteError(w, http.StatusNotFound, CodeNotFound)
			return
		}
		version, ok := versionOf(body)
		if !ok {
			WriteError(w, http.StatusBadRequest, CodeBadRequest)
			return
		}
		key, ok := s.idemKey(r)
		if !ok {
			WriteError(w, http.StatusBadRequest, CodeBadIdempotencyKey)
			return
		}
		s.write(w, r, key, "c:"+id, func(ctx context.Context, st Steps) (ActionResult, error) {
			return s.cfg.Actions.Consequence(ctx, ConsequenceRequest{Call: id, Version: version, Yes: yes, Steps: st})
		})
	}
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request, body any) {
	id := r.PathValue("id")
	if !planID.MatchString(id) {
		WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	version, ok := versionOf(body)
	if !ok {
		WriteError(w, http.StatusBadRequest, CodeBadRequest)
		return
	}
	key, ok := s.idemKey(r)
	if !ok {
		WriteError(w, http.StatusBadRequest, CodeBadIdempotencyKey)
		return
	}
	s.write(w, r, key, "p:"+id, func(ctx context.Context, st Steps) (ActionResult, error) {
		return s.cfg.Actions.Approve(ctx, ApproveRequest{Call: id, Version: version, Steps: st})
	})
}

func versionOf(body any) (string, bool) {
	b, _ := body.(*versionBody)
	if b == nil || b.Version == nil || !versionRE.MatchString(*b.Version) {
		return "", false
	}
	return *b.Version, true
}

type writeOutcome struct {
	status int
	reply  []byte
}

func (s *Server) reply(status int, v any) writeOutcome {
	b, err := json.Marshal(v)
	if err != nil {
		return errorOutcome(http.StatusInternalServerError, CodeInternal)
	}
	return writeOutcome{status, append(b, '\n')}
}

func errorOutcome(status int, code Code) writeOutcome {
	return writeOutcome{status, []byte(`{"error":"` + string(code) + `"}` + "\n")}
}

func put(w http.ResponseWriter, o writeOutcome) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(o.status)
	_, _ = w.Write(o.reply)
}

// write runs one write under the idempotency record of its key and the lock
// of its target. The work runs in a context that does not end with the
// request. After actionWait the caller gets 202 and the work goes on.
func (s *Server) write(w http.ResponseWriter, r *http.Request, key, target string, fn func(context.Context, Steps) (ActionResult, error)) {
	if s.cfg.Actions == nil {
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	sum, _ := r.Context().Value(bodySumKey{}).([sha256.Size]byte)
	route := r.Method + " " + r.URL.Path
	rec, out := s.idem.begin(key, route, target, sum)
	switch out {
	case outMismatch:
		WriteError(w, http.StatusUnprocessableEntity, CodeIdempotencyMismatch)
		return
	case outBusy:
		WriteError(w, http.StatusConflict, CodeInProgress)
		return
	case outFull:
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	case outReplay:
		put(w, writeOutcome{rec.status, rec.reply})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), actionTimeout)
	done := make(chan writeOutcome, 1)
	s.actWG.Add(1)
	go func() {
		defer s.actWG.Done()
		defer cancel()
		o, ok, hostFailed := s.runWrite(ctx, rec, fn)
		s.idem.finish(rec, o.status, o.reply, ok, hostFailed)
		done <- o
	}()
	t := time.NewTimer(s.actionWait)
	defer t.Stop()
	select {
	case o := <-done:
		put(w, o)
	case <-t.C:
		if s.idem.detach(rec) {
			put(w, s.reply(http.StatusAccepted, map[string]string{"mode": s.cfg.Mode, "result": string(ResultRunning)}))
			return
		}
		put(w, <-done)
	}
}

type doneReply struct {
	Mode       string `json:"mode"`
	Result     string `json:"result"`
	JournalSeq int64  `json:"journalSeq"`
}

// runWrite calls fn and turns its end into a reply. Nothing from an error
// reaches the reply or the log.
func (s *Server) runWrite(ctx context.Context, rec *idemRec, fn func(context.Context, Steps) (ActionResult, error)) (o writeOutcome, ok, hostFailed bool) {
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("server: action panic")
			o, ok, hostFailed = errorOutcome(http.StatusInternalServerError, CodeInternal), false, false
		}
	}()
	res, err := fn(ctx, rec.steps)
	if err != nil {
		status, code := mapError(err)
		if code == CodeInternal {
			s.log.Warn("server: action failed")
		}
		return errorOutcome(status, code), false, code == CodeHostFailed || code == CodeInternal
	}
	if (res.Result != ResultDone && res.Result != ResultPending) || res.JournalSeq < 0 {
		s.log.Warn("server: action gave an unusable result")
		return errorOutcome(http.StatusInternalServerError, CodeInternal), false, true
	}
	return s.reply(http.StatusOK, doneReply{Mode: s.cfg.Mode, Result: string(res.Result), JournalSeq: res.JournalSeq}), true, false
}

// waitActions waits for every write still running. For tests.
func (s *Server) waitActions() { s.actWG.Wait() }
