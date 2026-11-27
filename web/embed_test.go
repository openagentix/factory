//go:build !webapp

package web

import (
	"io/fs"
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
