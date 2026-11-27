//go:build webapp

package web

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// The server's policy allows no inline script, no inline style and no other
// origin (record 0004). These tests read the built files, so they run only
// with the tag webapp, after make web.

var (
	scriptTag = regexp.MustCompile(`(?is)<script\b([^>]*)>`)
	styleTag  = regexp.MustCompile(`(?is)<style\b`)
	styleAttr = regexp.MustCompile(`(?is)\sstyle\s*=`)
	refAttr   = regexp.MustCompile(`(?is)\b(?:href|src|action|poster)\s*=\s*["']?([^"'\s>]+)`)
	cssURL    = regexp.MustCompile(`(?is)url\(\s*["']?([^"')]+)`)
)

func otherOrigin(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.HasPrefix(v, "http:") || strings.HasPrefix(v, "https:") || strings.HasPrefix(v, "//") || strings.HasPrefix(v, "ws:") || strings.HasPrefix(v, "wss:") || strings.HasPrefix(v, "ftp:")
}

func TestBuiltPagesHaveNoInlineScriptStyleOrOtherOrigin(t *testing.T) {
	n := 0
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".html" {
			return err
		}
		n++
		b, err := fs.ReadFile(FS, p)
		if err != nil {
			return err
		}
		s := string(b)
		for _, m := range scriptTag.FindAllStringSubmatch(s, -1) {
			if !regexp.MustCompile(`(?i)\bsrc\s*=`).MatchString(m[1]) {
				t.Errorf("%s: inline script", p)
			}
		}
		if styleTag.MatchString(s) {
			t.Errorf("%s: style element", p)
		}
		if styleAttr.MatchString(s) {
			t.Errorf("%s: style attribute", p)
		}
		for _, m := range refAttr.FindAllStringSubmatch(s, -1) {
			if otherOrigin(m[1]) {
				t.Errorf("%s: reference to another origin: %s", p, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no built HTML file: run make web first")
	}
	if _, err := fs.Stat(FS, "index.html"); err != nil {
		t.Fatal("no index.html")
	}
}

func TestBuiltStylesAndScriptsUseNoOtherOrigin(t *testing.T) {
	css := 0
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".css" {
			return err
		}
		css++
		b, _ := fs.ReadFile(FS, p)
		for _, m := range cssURL.FindAllStringSubmatch(string(b), -1) {
			if otherOrigin(m[1]) {
				t.Errorf("%s: url to another origin: %s", p, m[1])
			}
		}
		if strings.Contains(string(b), "@import") {
			t.Errorf("%s: @import", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if css == 0 {
		t.Error("no stylesheet in the build")
	}
}

func TestBuiltFontsAndLicencesArePresent(t *testing.T) {
	for _, f := range []string{
		"BarlowCondensed-500.woff2", "BarlowCondensed-600.woff2", "BarlowCondensed-700.woff2",
		"IBMPlexSans-Regular.woff2", "IBMPlexSans-Medium.woff2", "IBMPlexSans-SemiBold.woff2",
		"IBMPlexMono-Regular.woff2", "IBMPlexMono-Medium.woff2",
	} {
		b, err := fs.ReadFile(FS, "fonts/"+f)
		if err != nil || len(b) < 1000 || string(b[:4]) != "wOF2" {
			t.Errorf("fonts/%s: missing or not a woff2 file", f)
		}
	}
	for _, f := range []string{"LICENSE-IBM-Plex-Sans.txt", "LICENSE-IBM-Plex-Mono.txt", "LICENSE-Barlow-Condensed.txt"} {
		b, err := fs.ReadFile(FS, "fonts/"+f)
		if err != nil || !strings.Contains(string(b), "SIL OPEN FONT LICENSE") {
			t.Errorf("fonts/%s: missing, or not the OFL text", f)
		}
	}
}
