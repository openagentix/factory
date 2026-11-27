package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	maxBodyBytes = 16 << 10
	maxJSONDepth = 16
)

var errBadJSON = errors.New("bad json")

// readBody applies the write rules: Content-Type exactly application/json,
// a body of at most 16 KiB, and strict decoding. On success it returns the
// decoded value. On failure it returns a status and a fixed code.
//
// An empty body is allowed (the value stays empty), because some actions
// send nothing. This is a ruling: the record is silent.
func readBody(w http.ResponseWriter, r *http.Request, rt *route) (any, int, Code) {
	if ct := r.Header["Content-Type"]; len(ct) != 1 || ct[0] != "application/json" {
		return nil, http.StatusUnsupportedMediaType, CodeUnsupportedMedia
	}
	if r.ContentLength > maxBodyBytes {
		return nil, http.StatusRequestEntityTooLarge, CodePayloadTooLarge
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, http.StatusRequestEntityTooLarge, CodePayloadTooLarge
		}
		return nil, http.StatusBadRequest, CodeBadRequest
	}
	var target any = &struct{}{}
	if rt.newBody != nil {
		if target = rt.newBody(); target == nil {
			return nil, http.StatusInternalServerError, CodeInternal
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return target, 0, ""
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, http.StatusBadRequest, CodeBadRequest
	}
	return target, 0, ""
}

// decodeStrict refuses invalid JSON, a top level that is not an object, a
// repeated key at any depth, an unknown field, a wrong type and anything
// after the first value.
func decodeStrict(raw []byte, target any) error {
	if err := checkObject(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errBadJSON
	}
	return nil
}

type jsonFrame struct {
	object    bool
	expectKey bool
	keys      map[string]struct{}
}

// checkObject walks the tokens. It finds repeated keys, which encoding/json
// silently accepts. Keys are compared in lower case, because encoding/json
// matches field names without regard to case, so "Text" and "text" would
// both set the same field. It is iterative, with a depth cap.
func checkObject(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var st []jsonFrame
	done := false
	complete := func() {
		if len(st) == 0 {
			done = true
		} else if top := &st[len(st)-1]; top.object {
			top.expectKey = true
		}
	}
	first := true
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if len(st) != 0 || !done {
				return errBadJSON
			}
			return nil
		}
		if err != nil {
			return err
		}
		if done {
			return errBadJSON
		}
		if first {
			if d, ok := tok.(json.Delim); !ok || d != '{' {
				return errBadJSON
			}
			first = false
		}
		if len(st) > 0 {
			top := &st[len(st)-1]
			if top.object && top.expectKey {
				if d, ok := tok.(json.Delim); ok && d == '}' {
					st = st[:len(st)-1]
					complete()
					continue
				}
				key, ok := tok.(string)
				if !ok {
					return errBadJSON
				}
				key = strings.ToLower(key)
				if _, dup := top.keys[key]; dup {
					return errBadJSON
				}
				top.keys[key] = struct{}{}
				top.expectKey = false
				continue
			}
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				if len(st) >= maxJSONDepth {
					return errBadJSON
				}
				f := jsonFrame{object: d == '{'}
				if f.object {
					f.expectKey = true
					f.keys = map[string]struct{}{}
				}
				st = append(st, f)
			default: // ']'
				st = st[:len(st)-1]
				complete()
			}
			continue
		}
		complete()
	}
}
