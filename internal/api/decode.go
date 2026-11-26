package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
