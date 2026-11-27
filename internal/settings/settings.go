// Package settings loads and checks the machine settings and takes the two
// lock files (record 0003, Machine settings and Locks).
//
// The file is strict. An unknown, missing or duplicate field, a wrong type, an
// alias, an anchor, a tag, a second document, a relative path or a harness
// that is not allowed stops the load. Nothing is guessed and nothing has a
// default except the optional webPort, which stays 0 when it is not written.
package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	// APIVersion is the only version of the file this build reads.
	APIVersion = "factory.openagentix.org/v1alpha1"
	// Kind is the only kind of the file.
	Kind = "MachineSettings"
	// MaxSize is the size cap of the file, in bytes.
	MaxSize = 64 << 10

	maxPath = 4096
)

// Harness is one harness program.
type Harness struct{ Name, Path string }

// Settings is the content of settings.yaml.
type Settings struct {
	MachineID string
	DataDir   string
	Harnesses []Harness
	// WebPort is 0 when the file does not set it. Otherwise 1024 to 65535.
	WebPort int
}

var machineIDPattern = regexp.MustCompile(`\A[A-Za-z0-9._-]{1,64}\z`)
var portPattern = regexp.MustCompile(`\A[0-9]{1,6}\z`)

// Parse reads and checks the text of a settings file.
func Parse(data []byte) (Settings, error) {
	if len(data) > MaxSize {
		return Settings{}, fmt.Errorf("settings: the file is larger than %d bytes", MaxSize)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Settings{}, errors.New("settings: the file is empty")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return Settings{}, errors.New("settings: the file holds no document")
		}
		return Settings{}, fmt.Errorf("settings: cannot read the file as YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Settings{}, errors.New("settings: the file must hold exactly one document")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return Settings{}, errors.New("settings: the file holds no document")
	}
	if err := plain(doc.Content[0]); err != nil {
		return Settings{}, err
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return Settings{}, errors.New("settings: the file must be a mapping of fields")
	}
	fields, err := fieldsOf(root, "the file", "apiVersion", "kind", "machineId", "dataDir", "harnesses", "webPort")
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	api, err := text(fields, "apiVersion")
	if err != nil {
		return Settings{}, err
	}
	if api != APIVersion {
		return Settings{}, fmt.Errorf("settings: apiVersion must be %s", APIVersion)
	}
	kind, err := text(fields, "kind")
	if err != nil {
		return Settings{}, err
	}
	if kind != Kind {
		return Settings{}, fmt.Errorf("settings: kind must be %s", Kind)
	}
	if s.MachineID, err = text(fields, "machineId"); err != nil {
		return Settings{}, err
	}
	if !machineIDPattern.MatchString(s.MachineID) {
		return Settings{}, errors.New("settings: machineId must be 1 to 64 characters of A-Z a-z 0-9 . _ -")
	}
	if s.DataDir, err = text(fields, "dataDir"); err != nil {
		return Settings{}, err
	}
	if err := checkPath("dataDir", s.DataDir); err != nil {
		return Settings{}, err
	}
	if s.Harnesses, err = harnesses(fields["harnesses"]); err != nil {
		return Settings{}, err
	}
	if n, ok := fields["webPort"]; ok {
		if n.Kind != yaml.ScalarNode || n.Tag != "!!int" || !portPattern.MatchString(n.Value) {
			return Settings{}, errors.New("settings: webPort must be a whole number written in decimal")
		}
		p, _ := strconv.Atoi(n.Value)
		if p < 1024 || p > 65535 {
			return Settings{}, errors.New("settings: webPort must be from 1024 to 65535")
		}
		s.WebPort = p
	}
	return s, nil
}

// plain refuses anchors, aliases and explicit tags anywhere in the tree.
func plain(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("settings: anchors and aliases are not allowed")
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return errors.New("settings: explicit YAML tags are not allowed")
	}
	for _, c := range n.Content {
		if err := plain(c); err != nil {
			return err
		}
	}
	return nil
}

// fieldsOf turns a mapping into a map by key. An unknown key, a duplicate key
// and a key that is not plain text are errors.
func fieldsOf(m *yaml.Node, where string, allowed ...string) (map[string]*yaml.Node, error) {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	out := map[string]*yaml.Node{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return nil, fmt.Errorf("settings: %s has a key that is not plain text", where)
		}
		if !ok[k.Value] {
			return nil, fmt.Errorf("settings: %s has an unknown field %q", where, safe(k.Value))
		}
		if _, dup := out[k.Value]; dup {
			return nil, fmt.Errorf("settings: %s sets %q twice", where, k.Value)
		}
		out[k.Value] = v
	}
	return out, nil
}

func safe(s string) string {
	if len(s) > 40 {
		s = s[:40]
	}
	return strconv.QuoteToASCII(s)
}

// text reads a required field that must be a string.
func text(fields map[string]*yaml.Node, name string) (string, error) {
	n, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("settings: %s is required", name)
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", fmt.Errorf("settings: %s must be text (put numbers and other values in quotes)", name)
	}
	if n.Value == "" {
		return "", fmt.Errorf("settings: %s is empty", name)
	}
	return n.Value, nil
}

func harnesses(n *yaml.Node) ([]Harness, error) {
	if n == nil {
		return nil, errors.New("settings: harnesses is required")
	}
	if n.Kind != yaml.SequenceNode {
		return nil, errors.New("settings: harnesses must be a list")
	}
	if len(n.Content) < 1 || len(n.Content) > 2 {
		return nil, errors.New("settings: harnesses must hold one or two entries")
	}
	var out []Harness
	seen := map[string]bool{}
	for i, item := range n.Content {
		where := fmt.Sprintf("harnesses[%d]", i)
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("settings: %s must be a mapping of name and path", where)
		}
		f, err := fieldsOf(item, where, "name", "path")
		if err != nil {
			return nil, err
		}
		name, err := text(f, "name")
		if err != nil {
			return nil, fmt.Errorf("%w (in %s)", err, where)
		}
		path, err := text(f, "path")
		if err != nil {
			return nil, fmt.Errorf("%w (in %s)", err, where)
		}
		switch name {
		case "claude-code", "codex":
		case "stub":
			return nil, errors.New("settings: the stub harness is not allowed on a machine")
		default:
			return nil, fmt.Errorf("settings: unknown harness %s; use claude-code or codex", safe(name))
		}
		if seen[name] {
			return nil, fmt.Errorf("settings: harness %s is listed twice", name)
		}
		seen[name] = true
		if err := checkPath(where+".path", path); err != nil {
			return nil, err
		}
		out = append(out, Harness{Name: name, Path: path})
	}
	return out, nil
}

// checkPath accepts an absolute, clean path of printable characters. A space
// is allowed, because a program is started without a shell. A path with "..",
// ".", "//" or a closing slash is not clean and is refused.
func checkPath(field, p string) error {
	switch {
	case p == "" || len(p) > maxPath:
		return fmt.Errorf("settings: %s must be 1 to %d characters", field, maxPath)
	case !filepath.IsAbs(p):
		return fmt.Errorf("settings: %s must be an absolute path", field)
	case p == "/" || filepath.Clean(p) != p:
		return fmt.Errorf("settings: %s must be a clean path with no .., ., // or closing slash", field)
	case strings.TrimSpace(p) != p:
		return fmt.Errorf("settings: %s must not start or end with a space", field)
	}
	for _, r := range p {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("settings: %s holds a character that cannot be printed", field)
		}
	}
	return nil
}
