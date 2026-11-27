package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Listen binds 127.0.0.1 on the configured port. A busy port is an error.
// No other port is ever tried.
func (s *Server) Listen() (net.Listener, error) {
	s.started.Store(true) // routes are fixed from here on
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.Port)))
	if err != nil {
		return nil, fmt.Errorf("server: cannot listen on 127.0.0.1 port %d: %w", s.cfg.Port, err)
	}
	return ln, nil
}

// ListenAndServe binds and serves until ctx ends, then shuts down.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve serves on ln, which must be 127.0.0.1 on the configured port. It
// returns nil after a clean stop. Requests inherit ctx, so streams end when
// ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ta, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !ta.IP.Equal(net.IPv4(127, 0, 0, 1)) || ta.Port != s.cfg.Port {
		_ = ln.Close()
		return errors.New("server: listener is not 127.0.0.1 on the configured port")
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: s.cfg.HeaderTimeout,
		WriteTimeout:      s.cfg.ResponseTimeout, // the stream sets its own, write by write
		IdleTimeout:       s.cfg.IdleTimeout,
		MaxHeaderBytes:    16 << 10,
		// "OPTIONS *" must reach the guards like any request.
		DisableGeneralOptionsHandler: true,
		BaseContext:                  func(net.Listener) context.Context { return ctx },
		// The library's own error lines can hold request text. Discard them.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	s.started.Store(true)
	lim := newLimitListener(ln, s.cfg.MaxConns)
	s.lim.Store(lim)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(lim) }()
	s.log.Info("server: listening", "port", s.cfg.Port)
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("server: stopped with an error")
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
	}
	<-errc
	s.log.Info("server: stopped")
	return nil
}
