//go:build !webapp

package web

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestPlaceholderIsThere(t *testing.T) {
	b, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "make web") {
		t.Error("the placeholder does not say how to build the web app")
	}
	if regexp.MustCompile(`(?is)<script\b|\sstyle\s*=|<style\b|<link\b`).MatchString(s) {
		t.Error("the placeholder has a script, a style or a link")
	}
}

// Text that someone else wrote is isolated for direction and breaks long words
// (fix round 1). This reads the stylesheet source.
func TestUntrustedTextIsolatesDirection(t *testing.T) {
	b, err := os.ReadFile("src/app.css")
	if err != nil {
		t.Fatal(err)
	}
	var rule string
	for _, r := range strings.Split(string(b), "}") {
		if regexp.MustCompile(`unicode-bidi\s*:\s*isolate`).MatchString(r) {
			rule = r
			break
		}
	}
	for _, sel := range []string{".tag .t", ".ttl", ".lt", ".tx", ".repo", ".who", ".tag .id"} {
		if !strings.Contains(rule, sel) {
			t.Errorf("selector %s is not isolated", sel)
		}
	}
	if !regexp.MustCompile(`overflow-wrap\s*:\s*anywhere`).MatchString(rule) {
		t.Error("no overflow-wrap: anywhere")
	}
}
