package floor

import "regexp"

// ErrCallStale is returned by CheckCall for every reason a request must be
// refused as "this changed": the call is gone, its version differs, it does
// not list the action, or the request itself is not well formed.
const ErrCallStale = staleError("floor: the call changed")

// staleError is a constant error, so the package keeps no variable.
type staleError string

func (e staleError) Error() string { return string(e) }

var versionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CheckCall decides again, on a fresh snapshot, whether a person's request may
// run. Nothing the browser sent is believed: the call must exist exactly once
// in f, its version must be the one the person was shown, and its actions must
// list the one asked for at this moment. Anything else is ErrCallStale. It is
// pure. The caller reads the snapshot and acts under its own lock.
func CheckCall(f Floor, id string, action ActionName, version string) (Call, error) {
	if id == "" || !action.Valid() || !versionPattern.MatchString(version) {
		return Call{}, ErrCallStale
	}
	var found *Call
	for i := range f.Calls {
		if f.Calls[i].ID != id {
			continue
		}
		if found != nil {
			return Call{}, ErrCallStale // listed twice: contradictory
		}
		found = &f.Calls[i]
	}
	if found == nil || found.Version != version {
		return Call{}, ErrCallStale
	}
	for _, a := range found.Actions {
		if a == action {
			return *found, nil
		}
	}
	return Call{}, ErrCallStale
}

// RefusalCodes lists the error codes of the server that have a sentence on the
// screen, as "refusal.<code>" phrases.
func RefusalCodes() []string {
	return []string{"stale", "in_progress", "host_failed", "unprocessable", "payload_too_large", "unavailable", "internal", "not_found", "unauthorized", "too_many_requests", "read_failed", "bad_request", "bad_idempotency_key", "idempotency_mismatch", "forbidden", "misdirected", "method_not_allowed", "unsupported_media_type"}
}
