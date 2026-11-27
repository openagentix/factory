package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

// newKey makes 32 random bytes, written in URL-safe base64. It is made fresh
// in memory at each start. This package never writes it anywhere itself.
func newKey() ([]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, errors.New("server: cannot make a key")
	}
	return []byte(base64.RawURLEncoding.EncodeToString(raw[:])), nil
}

// keyMatches compares in constant time. A different length returns early,
// so only the length can leak. The length is fixed and public.
func keyMatches(key []byte, presented string) bool {
	return subtle.ConstantTimeCompare(key, []byte(presented)) == 1
}
