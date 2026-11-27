package server

import "net/http"

// Stubs. The real guards come next.
func (s *Server) hostAllowed(r *http.Request) bool   { return true }
func (s *Server) originAllowed(r *http.Request) bool { return true }
func fetchSiteAllowed(r *http.Request) bool          { return true }

type keyResult int

const (
	keyOK keyResult = iota
	keyMissing
	keyWrong
	keyLimited
)

func (s *Server) checkKey(r *http.Request) keyResult { return keyOK }
