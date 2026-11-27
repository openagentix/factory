package server

import "net/http"

// Code is a fixed error code. Only these codes ever reach a response body.
// No error text, path, header value or key is ever put in a response.
type Code string

// The closed list of error codes.
const (
	CodeBadRequest       Code = "bad_request"
	CodeUnauthorized     Code = "unauthorized"
	CodeForbidden        Code = "forbidden"
	CodeNotFound         Code = "not_found"
	CodeMethodNotAllowed Code = "method_not_allowed"
	CodeMisdirected      Code = "misdirected"
	CodePayloadTooLarge  Code = "payload_too_large"
	CodeUnsupportedMedia Code = "unsupported_media_type"
	CodeTooManyRequests  Code = "too_many_requests"
	CodeUnavailable      Code = "unavailable"
	CodeReadFailed       Code = "read_failed"
	CodeInternal         Code = "internal"
	// Codes of the write routes (record 0004, "Actions").
	CodeStale               Code = "stale"
	CodeInProgress          Code = "in_progress"
	CodeBadIdempotencyKey   Code = "bad_idempotency_key"
	CodeIdempotencyMismatch Code = "idempotency_mismatch"
	CodeUnprocessable       Code = "unprocessable"
	CodeHostFailed          Code = "host_failed"
)

var knownCodes = map[Code]bool{
	CodeBadRequest: true, CodeUnauthorized: true, CodeForbidden: true,
	CodeNotFound: true, CodeMethodNotAllowed: true, CodeMisdirected: true,
	CodePayloadTooLarge: true, CodeUnsupportedMedia: true,
	CodeTooManyRequests: true, CodeUnavailable: true, CodeReadFailed: true,
	CodeInternal: true, CodeStale: true, CodeInProgress: true, CodeBadIdempotencyKey: true,
	CodeIdempotencyMismatch: true, CodeUnprocessable: true, CodeHostFailed: true,
}

// WriteError writes the fixed error body {"error":"<code>"}. An unknown code
// is written as "internal". Handlers added later must use this and nothing
// else for errors.
func WriteError(w http.ResponseWriter, status int, code Code) {
	if !knownCodes[code] {
		code = CodeInternal
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + string(code) + `"}` + "\n"))
}
