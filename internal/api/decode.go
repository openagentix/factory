package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// decodeYAMLStrict decodes exactly one YAML document into out.
// Unknown fields are an error.
func decodeYAMLStrict(data []byte, out any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty input")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("input must hold exactly one document")
	}
	return nil
}

// decodeJSONStrict decodes exactly one JSON value into out.
// Unknown fields are an error.
func decodeJSONStrict(data []byte, out any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty input")
	}
	if err := checkExactKeys(data, reflect.TypeOf(out), ""); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	if dec.More() {
		return errors.New("trailing data after the json value")
	}
	return nil
}

var unmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// checkExactKeys walks the json value next to the Go type and fails when a key
// matches a field only when case is ignored. encoding/json accepts such keys,
// so "Status" would pass for "status". Any other problem (a wrong type, a key
// that matches nothing) is left to the normal decoder, which reports it.
func checkExactKeys(raw []byte, t reflect.Type, where string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(unmarshalerType) {
		return nil
	}
	switch t.Kind() {
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil
		}
		for i, item := range items {
			if err := checkExactKeys(item, t.Elem(), fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		fields := map[string]reflect.StructField{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if name := jsonName(f); name != "" {
				fields[name] = f
			}
		}
		for key, val := range obj {
			f, ok := fields[key]
			if !ok {
				for name := range fields {
					if strings.EqualFold(name, key) {
						return fmt.Errorf("parse json: unknown field %q at %s: names are case sensitive, use %q", key, orRoot(where), name)
					}
				}
				continue // the decoder reports an unknown field
			}
			if err := checkExactKeys(val, f.Type, joinPath(where, key)); err != nil {
				return err
			}
		}
	}
	return nil
}

// jsonName is the json key of a struct field, or "" when it has none.
func jsonName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name
	}
	return f.Name
}

func joinPath(where, key string) string {
	if where == "" {
		return key
	}
	return where + "." + key
}

func orRoot(where string) string {
	if where == "" {
		return "the top level"
	}
	return where
}
