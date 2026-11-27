package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/openagentix/factory/web"
)

const secretText = "OUTSIDE-THE-FS-SECRET"

// dirEnv serves a real directory that has a secret next to it, so a path
// trick that escapes would show the secret.
func dirEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(secretText), 0o600); err != nil {
		t.Fatal(err)
	}
	pub := filepath.Join(root, "pub")
	if err := os.MkdirAll(filepath.Join(pub, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pub, "index.html"), []byte(testIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pub, "app", "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(filepath.Join(root, "secret.txt"), filepath.Join(pub, "link.txt"))
	return newEnv(t, func(c *Config) { c.Static = os.DirFS(pub) })
}

func TestStaticPathTricksNeverEscapeOrFail(t *testing.T) {
	e := dirEnv(t)
	malformed := []string{
		"/../secret.txt", "/%2e%2e/secret.txt", "/%2E%2E/secret.txt", "//secret.txt", "//etc/passwd",
		"/app/../../secret.txt", "/app/%2e%2e/%2e%2e/secret.txt", "/a\\..\\secret.txt", "/%5c..%5csecret.txt",
		"/a%00b", "/index.html%00.png", "/./index.html", "/app/./x.js", "/app//x.js", "/..", "/%2e", "/a%0ab", "/a%7f",
		"/..%2fsecret.txt", "/%2e%2e%2fsecret.txt",
	}
	for _, p := range malformed {
		rec := e.do(req{path: p})
		if strings.Contains(rec.Body.String(), secretText) {
			t.Errorf("%q served the secret", p)
		}
		if rec.Code >= 500 {
			t.Errorf("%q gave %d", p, rec.Code)
		}
		assertErr(t, rec, 404, CodeNotFound, p)
		assertSecure(t, rec, false, p)
	}
	// a symlink inside the directory that points outside is not followed
	// out: os.DirFS opens it, so the server refuses anything that is not a
	// regular file inside the tree
	rec := e.do(req{path: "/link.txt"})
	if strings.Contains(rec.Body.String(), secretText) {
		t.Error("symlink to a file outside was served")
	}
	for _, p := range []string{"/app", "/app/", "/goals/", "/missing/deep/path"} {
		if rec := e.do(req{path: p}); rec.Code != 200 || rec.Body.String() != testIndex {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if rec := e.do(req{path: "/app/x.js"}); rec.Code != 200 || rec.Body.String() != "1" {
		t.Errorf("x.js: %d", rec.Code)
	}
}

var (
	scriptTag = regexp.MustCompile(`(?is)<script\b([^>]*)>`)
	srcAttr   = regexp.MustCompile(`(?i)\ssrc\s*=`)
	styleAttr = regexp.MustCompile(`(?is)<[a-z][^>]*\sstyle\s*=`)
	styleTag  = regexp.MustCompile(`(?i)<style\b`)
)

// lintHTML returns the problems that a strict style-src and script-src would
// block: an inline script, a style attribute or a style element.
func lintHTML(b []byte) []string {
	var out []string
	for _, m := range scriptTag.FindAllSubmatch(b, -1) {
		if !srcAttr.Match(append([]byte(" "), m[1]...)) {
			out = append(out, "inline script")
		}
	}
	if styleAttr.Match(b) {
		out = append(out, "style attribute")
	}
	if styleTag.Match(b) {
		out = append(out, "style element")
	}
	return out
}

func TestLintHTMLFindsWhatItShould(t *testing.T) {
	for name, h := range map[string]string{
		"inline script": `<script>alert(1)</script>`,
		"inline upper":  `<SCRIPT type="module">x</SCRIPT>`,
		"style attr":    `<p style="color:red">x</p>`,
		"style attr nl": "<p\nSTYLE = 'a'>x</p>",
		"style element": `<style>p{}</style>`,
		"src in text":   `<script>var src = 1</script>`,
		"data-src only": `<script data-src="a.js"></script>`,
	} {
		if len(lintHTML([]byte(h))) == 0 {
			t.Errorf("%s: not found", name)
		}
	}
	for name, h := range map[string]string{
		"external": `<script src="/a.js"></script>`,
		"module":   `<script type="module" src="/a.js"></script>`,
		"plain":    `<p class="x">hello</p>`,
	} {
		if p := lintHTML([]byte(h)); len(p) != 0 {
			t.Errorf("%s: %v", name, p)
		}
	}
}

// Any index.html the server serves must pass the lint, including the
// placeholder in the binary.
func TestServedIndexHasNoInlineScriptOrStyle(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Static = web.FS })
	for _, p := range []string{"/", "/index.html", "/goals", "/anything/else"} {
		rec := e.do(req{path: p})
		if rec.Code != 200 {
			t.Fatalf("%s: %d", p, rec.Code)
		}
		if probs := lintHTML(rec.Body.Bytes()); len(probs) != 0 {
			t.Errorf("%s: %v", p, probs)
		}
		if !strings.Contains(rec.Body.String(), "make web") {
			t.Errorf("%s: the placeholder does not say how to build the web app", p)
		}
	}
	// every html file in the embedded tree
	_ = fs.WalkDir(web.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") {
			b, _ := fs.ReadFile(web.FS, p)
			if probs := lintHTML(b); len(probs) != 0 {
				t.Errorf("%s: %v", p, probs)
			}
		}
		return nil
	})
}
