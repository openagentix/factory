package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Source gives the Floor snapshot as JSON bytes. The server writes the bytes
// as they are. An error from it never reaches a response.
type Source interface {
	Floor(ctx context.Context) ([]byte, error)
}

// Config is what New needs.
type Config struct {
	// Host is the address to bind. Empty means 127.0.0.1. Nothing else is accepted.
	Host string
	// Port is 1024 to 65535. The server never picks another port.
	Port int
	// Mode is "demo" or "live". It is sent in the hello event.
	Mode string
	// Source gives the Floor. With nil, the Floor route answers 503.
	Source Source
	// Actions carries out the three actions of the first release. With nil,
	// every action route answers 503.
	Actions Actions
	// Static is the web app. Required.
	Static fs.FS
	// Logger takes the package's own log lines. Nil discards them.
	Logger *slog.Logger
	// Hub carries events to streams. Nil means a new hub for Mode. Use
	// Server.Hub to publish.
	Hub *Hub
	// PingInterval, StreamMaxAge and WriteDeadline default to 15 seconds, 30
	// minutes and 10 seconds. Tests may make them shorter.
	PingInterval, StreamMaxAge, WriteDeadline time.Duration
	// HeaderTimeout is how long a client may take to send the request
	// headers (default 5 seconds).
	HeaderTimeout time.Duration
	// RequestReadTimeout is how long a whole request, body included, may
	// take to arrive (default 10 seconds). The event stream clears it for
	// itself and uses WriteDeadline and StreamMaxAge instead.
	RequestReadTimeout time.Duration
	// ResponseTimeout is how long a response that is not a stream may take
	// to be written (default 10 seconds).
	ResponseTimeout time.Duration
	// IdleTimeout is how long a kept-alive connection may sit idle (default
	// 60 seconds).
	IdleTimeout time.Duration
	// MaxConns is the number of open connections (default 64). More are not
	// accepted until one closes.
	MaxConns int
	// Now is the clock. Nil means time.Now. For tests.
	Now func() time.Time
}

// Server is the local HTTP server.
type Server struct {
	cfg     Config
	key     []byte
	log     *slog.Logger
	hosts   map[string]bool
	origins map[string]bool
	limiter *limiter
	hub     *Hub
	idem    *idemKeeper
	// actionWait is how long a write may take before the reply is 202. Tests
	// shorten it; nothing outside the package can.
	actionWait time.Duration
	actWG      sync.WaitGroup
	// stopping is set when a stop is requested: no new write starts. actCtx is
	// the context of every write; it ends when the stop's limit is reached.
	actMu    sync.Mutex // one step: the stopping check and the count of a write
	stopping atomic.Bool
	// stopCheckHook is called between the stopping check and the count. Tests only.
	stopCheckHook func()
	actCtx        context.Context
	actCancel     context.CancelFunc
	// Timers for the event stream. Real ones by default. Tests replace them
	// with channels they control. Each returns a channel and a stop function.
	newTicker func(time.Duration) (<-chan time.Time, func())
	newTimer  func(time.Duration) (<-chan time.Time, func())

	mu      sync.RWMutex
	routes  []*route
	started atomic.Bool
	lim     atomic.Pointer[limitListener]
}

// New checks the config and makes a Server with a fresh key.
func New(cfg Config) (*Server, error) {
	// Deny by default: only known good values pass. The messages never hold
	// a value from the config, so nothing odd can reach a log through them.
	if cfg.Host != "" && cfg.Host != "127.0.0.1" {
		return nil, errors.New("server: only 127.0.0.1 can be listened on in this release")
	}
	if cfg.Port < 1024 || cfg.Port > 65535 {
		return nil, errors.New("server: port must be from 1024 to 65535")
	}
	if cfg.Mode != "demo" && cfg.Mode != "live" {
		return nil, errors.New("server: mode must be demo or live")
	}
	if cfg.Static == nil {
		return nil, errors.New("server: no web app files")
	}
	if cfg.PingInterval < 0 || cfg.StreamMaxAge < 0 || cfg.WriteDeadline < 0 ||
		cfg.HeaderTimeout < 0 || cfg.RequestReadTimeout < 0 || cfg.ResponseTimeout < 0 ||
		cfg.IdleTimeout < 0 || cfg.MaxConns < 0 {
		return nil, errors.New("server: a duration cannot be negative")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Hub == nil {
		cfg.Hub = NewHub(cfg.Mode)
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 15 * time.Second
	}
	if cfg.StreamMaxAge <= 0 {
		cfg.StreamMaxAge = 30 * time.Minute
	}
	if cfg.WriteDeadline <= 0 {
		cfg.WriteDeadline = 10 * time.Second
	}
	if cfg.HeaderTimeout == 0 {
		cfg.HeaderTimeout = 5 * time.Second
	}
	if cfg.RequestReadTimeout == 0 {
		cfg.RequestReadTimeout = 10 * time.Second
	}
	if cfg.ResponseTimeout == 0 {
		cfg.ResponseTimeout = 10 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 64
	}
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, key: key, log: cfg.Logger, hub: cfg.Hub, newTicker: realTicker, newTimer: realTimer, hosts: map[string]bool{}, origins: map[string]bool{}}
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		s.hosts[fmt.Sprintf("%s:%d", h, cfg.Port)] = true
		s.origins[fmt.Sprintf("http://%s:%d", h, cfg.Port)] = true
	}
	s.limiter = newLimiter(cfg.Now, 10, time.Minute, 1024)
	s.idem = newIdemKeeper(maxIdemKeys)
	s.actionWait = 8 * time.Second
	s.actCtx, s.actCancel = context.WithCancel(context.Background())
	s.registerActions()
	s.mustHandle(Route{Method: http.MethodGet, Path: "/api/v1/floor", Handler: s.handleFloor})
	s.mustHandle(Route{Method: http.MethodGet, Path: "/api/v1/events", Handler: s.handleEvents})
	return s, nil
}

func (s *Server) mustHandle(rt Route) {
	if err := s.Handle(rt); err != nil {
		panic("server: bad built-in route: " + err.Error())
	}
}

// Hub is where events are published.
func (s *Server) Hub() *Hub { return s.hub }

// Link is the address to open, with the key in the fragment. The fragment
// never goes to the server. Callers must treat the result as secret.
func (s *Server) Link() string {
	return fmt.Sprintf("http://127.0.0.1:%d/#t=%s", s.cfg.Port, s.key)
}

// Handler is the whole server: guards first, then routes.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

func isAPIPath(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }

func (s *Server) serve(rw http.ResponseWriter, r *http.Request) {
	api := isAPIPath(r.URL.Path)
	w := newSecureWriter(rw, api)
	// Every request has a read deadline, body included. A client that
	// announces a body and never sends it cannot hold the connection. The
	// event stream clears this for itself. Errors are ignored: some writers
	// (tests) cannot set deadlines.
	_ = http.NewResponseController(rw).SetReadDeadline(time.Now().Add(s.cfg.RequestReadTimeout))
	defer func() {
		if rec := recover(); rec != nil {
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			// The panic value is not logged. It could hold anything.
			s.log.Error("server: handler panic")
			if !w.wrote {
				WriteError(w, http.StatusInternalServerError, CodeInternal)
			}
		}
	}()
	if !s.hostAllowed(r) {
		WriteError(w, http.StatusMisdirectedRequest, CodeMisdirected)
		return
	}
	if !s.originAllowed(r) || !fetchSiteAllowed(r) {
		WriteError(w, http.StatusForbidden, CodeForbidden)
		return
	}
	if api {
		s.serveAPI(w, r)
		return
	}
	s.serveStatic(w, r)
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	switch s.checkKey(r) {
	case keyMissing, keyWrong:
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized)
		return
	case keyLimited:
		WriteError(w, http.StatusTooManyRequests, CodeTooManyRequests)
		return
	}
	rt, allow := s.match(r)
	if rt == nil {
		if len(allow) > 0 {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed)
			return
		}
		WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	var body any
	if rt.write {
		var status int
		var code Code
		var sum [sha256.Size]byte
		if body, sum, status, code = readBody(w, r, rt); code != "" {
			WriteError(w, status, code)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), bodySumKey{}, sum))
	}
	rt.handler(w, r, body)
}

func (s *Server) handleFloor(w http.ResponseWriter, r *http.Request, _ any) {
	if s.cfg.Source == nil {
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	b, err := s.cfg.Source.Floor(ctx)
	if err != nil || !json.Valid(b) {
		// The error text is not logged or sent. It could hold anything.
		s.log.Warn("server: floor read failed")
		WriteError(w, http.StatusInternalServerError, CodeReadFailed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func realTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

func realTimer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}
