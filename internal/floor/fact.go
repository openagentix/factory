package floor

import (
	"errors"
	"strconv"
)

type factKind uint8

const (
	factNone factKind = iota
	factNum
	factText // a validated code, machine id, station key or risk reason
)

// Fact is a value that may fill a fixed sentence: a number, a reason code, a
// machine id, a station key, or a risk reason. It is never a free string. The
// only way to make a valid Fact is a constructor, and each constructor
// validates. The zero Fact is invalid and will not encode.
type Fact struct {
	kind factKind
	n    int64
	s    string
}

// maxFactNumber keeps numbers small enough to be counts, numbers of tickets
// and goals, or attempts. Times are not Facts.
const maxFactNumber = 1 << 31

func NumFact(n int64) (Fact, bool) {
	if n < 0 || n > maxFactNumber {
		return Fact{}, false
	}
	return Fact{kind: factNum, n: n}, true
}

func CodeFact(c ReasonCode) (Fact, bool) {
	if !c.Valid() {
		return Fact{}, false
	}
	return Fact{kind: factText, s: string(c)}, true
}

func MachineFact(id string) (Fact, bool) {
	if !ValidMachineID(id) {
		return Fact{}, false
	}
	return Fact{kind: factText, s: id}, true
}

func StationFact(k StationKey) (Fact, bool) {
	if !k.Valid() {
		return Fact{}, false
	}
	return Fact{kind: factText, s: string(k)}, true
}

func RiskFact(r RiskReason) (Fact, bool) {
	if !r.Valid() {
		return Fact{}, false
	}
	return Fact{kind: factText, s: string(r)}, true
}

func (f Fact) Valid() bool { return f.kind == factNum || (f.kind == factText && f.s != "") }

// Number returns the number of a numeric Fact.
func (f Fact) Number() (int64, bool) { return f.n, f.kind == factNum }

// Text returns the text of a Fact that is not a number.
func (f Fact) Text() (string, bool) { return f.s, f.kind == factText }

func (f Fact) MarshalJSON() ([]byte, error) {
	switch f.kind {
	case factNum:
		return []byte(strconv.FormatInt(f.n, 10)), nil
	case factText:
		if f.s == "" {
			break
		}
		return []byte(strconv.Quote(f.s)), nil // validated ASCII only
	}
	return nil, errors.New("floor: invalid fact")
}

// ValidMachineID reports whether s is 1 to 64 of A-Z a-z 0-9 . _ -
func ValidMachineID(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}
