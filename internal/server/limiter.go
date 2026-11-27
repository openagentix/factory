package server

import "time"

type limiter struct{}

func newLimiter(now func() time.Time, max int, window time.Duration, capacity int) *limiter {
	return &limiter{}
}
func (l *limiter) fail(addr string) bool { return false }
func (l *limiter) size() int             { return 0 }
