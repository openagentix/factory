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
	// Accept, CancelAccept and Undo act on the holding area. Each decides again
	// that the state the person saw is still the latest one.
	Accept(ctx context.Context, r HoldingRequest) (ActionResult, error)
	CancelAccept(ctx context.Context, r HoldingRequest) (ActionResult, error)
	Undo(ctx context.Context, r UndoRequest) (ActionResult, error)
	// UndoPreview says what an Undo of one change would take out. It changes
	// nothing.
	UndoPreview(ctx context.Context, change string) (UndoPreviewView, error)
}

// HoldingRequest is an Accept or a Cancel. StagingSeq and Commit name the state
// the person saw. Version is the Accept call's version.
type HoldingRequest struct {
	StagingSeq      int64
	Commit, Version string
	Steps           Steps
}

// UndoRequest takes one change out of the holding area. AlsoRemoved is the list
// the preview gave, in the same order, and never nil.
type UndoRequest struct {
	Change      string
	StagingSeq  int64
	Commit      string
	AlsoRemoved []string
	Steps       Steps
}

// UndoTicket names the ticket of one staging entry.
type UndoTicket struct {
	Change string `json:"change"`
	Ticket int    `json:"ticket"`
}

// UndoPreviewView is what leaves the holding area with one change. Tickets has
// the change first, then one per AlsoRemoved entry, in the same order.
type UndoPreviewView struct {
	Change      string
	StagingSeq  int64
	Commit      string
	AlsoRemoved []string
	Tickets     []UndoTicket
}

// Detailer gives the detail of one piece as JSON bytes, or false when there is
// no such piece. An error from it never reaches a response.
type Detailer interface {
	Piece(ctx context.Context, id string) ([]byte, bool, error)
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

const (
	// holdingTarget is the one target of Accept, Cancel and Undo: they never
	// overlap (record 0004, "One write at a time on a target").
	holdingTarget  = "h:holding-area"
	maxAlsoRemoved = 50
)

var (
	commitRE      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	entryIDRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	pieceIDRE     = regexp.MustCompile(`^[tpk][1-9][0-9]{0,8}$`)
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
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/holding-area/accept",
		NewBody: func() any { return &holdingJSON{} }, Handler: s.handleHolding(false)})
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/holding-area/cancel",
		NewBody: func() any { return &holdingJSON{} }, Handler: s.handleHolding(true)})
	s.mustHandle(Route{Method: http.MethodPost, Path: "/api/v1/changes/{id}/undo",
		NewBody: func() any { return &undoJSON{} }, Handler: s.handleUndo})
	s.mustHandle(Route{Method: http.MethodGet, Path: "/api/v1/changes/{id}/undo-preview", Handler: s.handleUndoPreview})
	s.mustHandle(Route{Method: http.MethodGet, Path: "/api/v1/pieces/{id}", Handler: s.handlePiece})
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
	if s.cfg.Actions == nil || s.stopping.Load() {
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
	ctx, cancel := context.WithTimeout(s.actCtx, actionTimeout) // not the request's: it outlives the request
	done := make(chan writeOutcome, 1)
	// The stopping check and the count of the write are one step, so no write
	// can be counted after drain began to wait.
	s.actMu.Lock()
	if s.stopping.Load() {
		s.actMu.Unlock()
		cancel()
		s.idem.finish(rec, http.StatusServiceUnavailable, nil, false, false) // refused: forgotten
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	if s.stopCheckHook != nil {
		s.stopCheckHook()
	}
	s.actWG.Add(1)
	s.actMu.Unlock()
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

// beginStop is called when a stop is requested: no new write starts.
func (s *Server) beginStop() {
	s.actMu.Lock()
	s.stopping.Store(true)
	s.actMu.Unlock()
}

// drain waits for the writes in flight up to the limit. When the limit is
// reached the context of every write ends, so the work stops and its reply is a
// failure, and drain says false. It never reports a write as done.
func (s *Server) drain(limit time.Duration) bool {
	s.beginStop() // idempotent: the wait never starts while a write may still be counted
	done := make(chan struct{})
	go func() { s.actWG.Wait(); close(done) }()
	t := time.NewTimer(limit)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		s.actCancel()
		return false
	}
}

type holdingJSON struct {
	StagingSeq *int64  `json:"stagingSeq"`
	Commit     *string `json:"commit"`
	Version    *string `json:"version"`
}

type undoJSON struct {
	StagingSeq  *int64   `json:"stagingSeq"`
	Commit      *string  `json:"commit"`
	Change      *string  `json:"change"`
	AlsoRemoved []string `json:"alsoRemoved"`
}

// handleHolding serves Accept and, with cancel set, Cancel. The body names the
// state the person saw and the version of the Accept call. Whether they are
// still the latest is decided by Actions, under its own lock.
func (s *Server) handleHolding(cancel bool) ActionFunc {
	return func(w http.ResponseWriter, r *http.Request, body any) {
		b, _ := body.(*holdingJSON)
		if b == nil || b.StagingSeq == nil || b.Commit == nil || b.Version == nil ||
			*b.StagingSeq < 1 || !commitRE.MatchString(*b.Commit) || !versionRE.MatchString(*b.Version) {
			WriteError(w, http.StatusBadRequest, CodeBadRequest)
			return
		}
		key, ok := s.idemKey(r)
		if !ok {
			WriteError(w, http.StatusBadRequest, CodeBadIdempotencyKey)
			return
		}
		seq, commit, version := *b.StagingSeq, *b.Commit, *b.Version
		s.write(w, r, key, holdingTarget, func(ctx context.Context, st Steps) (ActionResult, error) {
			req := HoldingRequest{StagingSeq: seq, Commit: commit, Version: version, Steps: st}
			if cancel {
				return s.cfg.Actions.CancelAccept(ctx, req)
			}
			return s.cfg.Actions.Accept(ctx, req)
		})
	}
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request, body any) {
	id := r.PathValue("id")
	b, _ := body.(*undoJSON)
	if b == nil || b.StagingSeq == nil || b.Commit == nil || b.Change == nil || b.AlsoRemoved == nil ||
		*b.StagingSeq < 1 || !commitRE.MatchString(*b.Commit) || *b.Change != id || len(b.AlsoRemoved) > maxAlsoRemoved {
		WriteError(w, http.StatusBadRequest, CodeBadRequest)
		return
	}
	for _, a := range b.AlsoRemoved {
		if !entryIDRE.MatchString(a) {
			WriteError(w, http.StatusBadRequest, CodeBadRequest)
			return
		}
	}
	key, ok := s.idemKey(r)
	if !ok {
		WriteError(w, http.StatusBadRequest, CodeBadIdempotencyKey)
		return
	}
	req := UndoRequest{Change: id, StagingSeq: *b.StagingSeq, Commit: *b.Commit, AlsoRemoved: append([]string{}, b.AlsoRemoved...)}
	s.write(w, r, key, holdingTarget, func(ctx context.Context, st Steps) (ActionResult, error) {
		req.Steps = st
		return s.cfg.Actions.Undo(ctx, req)
	})
}

type previewReply struct {
	Mode        string       `json:"mode"`
	Change      string       `json:"change"`
	StagingSeq  int64        `json:"stagingSeq"`
	Commit      string       `json:"commit"`
	AlsoRemoved []string     `json:"alsoRemoved"`
	Tickets     []UndoTicket `json:"tickets"`
}

// handleUndoPreview is a read: it changes nothing and needs no key of its own.
// What it returns is only what the person is shown. The Undo that follows
// decides again.
func (s *Server) handleUndoPreview(w http.ResponseWriter, r *http.Request, _ any) {
	if s.cfg.Actions == nil || s.stopping.Load() {
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	v, err := s.cfg.Actions.UndoPreview(ctx, r.PathValue("id"))
	if err != nil {
		status, code := mapError(err)
		if code == CodeInternal {
			s.log.Warn("server: undo preview failed")
		}
		WriteError(w, status, code)
		return
	}
	out := previewReply{Mode: s.cfg.Mode, Change: v.Change, StagingSeq: v.StagingSeq, Commit: v.Commit,
		AlsoRemoved: append([]string{}, v.AlsoRemoved...), Tickets: append([]UndoTicket{}, v.Tickets...)}
	b, err := json.Marshal(out)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(b, '\n'))
}

// handlePiece gives the detail of one piece, as the Detailer made it.
func (s *Server) handlePiece(w http.ResponseWriter, r *http.Request, _ any) {
	id := r.PathValue("id")
	if !pieceIDRE.MatchString(id) {
		WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	if s.cfg.Detail == nil {
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	b, ok, err := s.cfg.Detail.Piece(ctx, id)
	if errors.Is(err, ErrUnavailable) {
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	if err != nil || (ok && !json.Valid(b)) {
		// The error text is not logged or sent. It could hold anything.
		s.log.Warn("server: piece read failed")
		WriteError(w, http.StatusInternalServerError, CodeReadFailed)
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
