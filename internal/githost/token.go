package githost

import (
	"errors"
	"fmt"
	"strings"
)

// ProposeToken is the credential for the propose actions. It prints as
// "[redacted]" in every format. It is a different type from AcceptToken, so
// one cannot be passed where the other is wanted.
type ProposeToken struct{ v string }

// AcceptToken is the credential for the accept actions.
type AcceptToken struct{ v string }

func checkToken(s string) error {
	if s == "" {
		return errors.New("githost: token is empty")
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return errors.New("githost: token holds white space")
	}
	return nil
}

// NewProposeToken wraps a propose token.
func NewProposeToken(s string) (ProposeToken, error) {
	if err := checkToken(s); err != nil {
		return ProposeToken{}, err
	}
	return ProposeToken{v: s}, nil
}

// NewAcceptToken wraps an accept token.
func NewAcceptToken(s string) (AcceptToken, error) {
	if err := checkToken(s); err != nil {
		return AcceptToken{}, err
	}
	return AcceptToken{v: s}, nil
}

func (ProposeToken) String() string               { return "[redacted]" }
func (ProposeToken) GoString() string             { return "[redacted]" }
func (t ProposeToken) Format(s fmt.State, _ rune) { fmt.Fprint(s, "[redacted]") }
func (AcceptToken) String() string                { return "[redacted]" }
func (AcceptToken) GoString() string              { return "[redacted]" }
func (t AcceptToken) Format(s fmt.State, _ rune)  { fmt.Fprint(s, "[redacted]") }

// redact removes every secret from text that may be shown or returned.
func redact(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[redacted]")
		}
	}
	return s
}
