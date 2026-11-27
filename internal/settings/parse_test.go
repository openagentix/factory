package settings

import (
	"strings"
	"testing"
)

const goodYAML = `apiVersion: factory.openagentix.org/v1alpha1
kind: MachineSettings
machineId: alice-laptop
dataDir: /home/alice/.local/share/oxf
harnesses:
  - { name: claude-code, path: /usr/local/bin/claude }
  - { name: codex, path: /usr/local/bin/codex }
webPort: 8080
`

func TestParseGood(t *testing.T) {
	s, err := Parse([]byte(goodYAML))
	if err != nil {
		t.Fatal(err)
	}
	if s.MachineID != "alice-laptop" || s.DataDir != "/home/alice/.local/share/oxf" || s.WebPort != 8080 ||
		len(s.Harnesses) != 2 || s.Harnesses[0] != (Harness{"claude-code", "/usr/local/bin/claude"}) ||
		s.Harnesses[1] != (Harness{"codex", "/usr/local/bin/codex"}) {
		t.Fatalf("got %+v", s)
	}
}

func TestParseWebPortIsOptional(t *testing.T) {
	s, err := Parse([]byte(strings.Replace(goodYAML, "webPort: 8080\n", "", 1)))
	if err != nil || s.WebPort != 0 {
		t.Fatalf("%+v %v", s, err)
	}
	one := strings.Replace(goodYAML, "  - { name: codex, path: /usr/local/bin/codex }\n", "", 1)
	if s, err = Parse([]byte(one)); err != nil || len(s.Harnesses) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	for _, p := range []string{"1024", "65535"} {
		if _, err := Parse([]byte(strings.Replace(goodYAML, "8080", p, 1))); err != nil {
			t.Errorf("port %s refused: %v", p, err)
		}
	}
}

// Every bad input is a mutation of the good file, and none may load.
func TestParseRefuses(t *testing.T) {
	rep := func(old, new string) string { return strings.Replace(goodYAML, old, new, 1) }
	cases := map[string]string{
		// shape
		"empty file":          "",
		"only blanks":         "  \n\n",
		"only a comment":      "# nothing\n",
		"null document":       "null\n",
		"a list":              "- a\n- b\n",
		"unknown field":       goodYAML + "extra: 1\n",
		"unknown nested":      rep("path: /usr/local/bin/claude }", "path: /usr/local/bin/claude, args: x }"),
		"wrong case key":      rep("machineId", "machineID"),
		"duplicate key":       goodYAML + "machineId: bob\n",
		"duplicate nested":    rep("{ name: claude-code, path: /usr/local/bin/claude }", "{ name: claude-code, name: codex, path: /usr/local/bin/claude }"),
		"two documents":       goodYAML + "---\n" + goodYAML,
		"two documents empty": goodYAML + "---\n",
		"anchor":              rep("machineId: alice-laptop", "machineId: &id alice-laptop"),
		"alias":               "x: &a alice\n" + rep("machineId: alice-laptop", "machineId: *a"),
		"merge key":           "base: &b {name: codex, path: /usr/bin/codex}\n" + rep("{ name: codex, path: /usr/local/bin/codex }", "{ <<: *b }"),
		"custom tag":          rep("machineId: alice-laptop", "machineId: !custom alice"),
		"tab indent":          "apiVersion: factory.openagentix.org/v1alpha1\nkind: MachineSettings\nmachineId: a\ndataDir: /d/x\nharnesses:\n\t- { name: codex, path: /usr/bin/codex }\n",
		"NUL byte":            rep("alice-laptop", "alice\x00laptop"),
		"binary garbage":      "\xff\xfe\x00\x01",
		"10 MiB":              goodYAML + "# " + strings.Repeat("a", 10<<20) + "\n",
		// apiVersion and kind
		"missing apiVersion": rep("apiVersion: factory.openagentix.org/v1alpha1\n", ""),
		"wrong apiVersion":   rep("v1alpha1", "v1"),
		"empty apiVersion":   rep("factory.openagentix.org/v1alpha1", `""`),
		"missing kind":       rep("kind: MachineSettings\n", ""),
		"wrong kind":         rep("MachineSettings", "FactorySettings"),
		"kind lower case":    rep("MachineSettings", "machinesettings"),
		// machineId
		"missing machineId": rep("machineId: alice-laptop\n", ""),
		"null machineId":    rep("machineId: alice-laptop", "machineId:"),
		"empty machineId":   rep("alice-laptop", `""`),
		"65 character id":   rep("alice-laptop", strings.Repeat("a", 65)),
		"id with slash":     rep("alice-laptop", "alice/laptop"),
		"id with space":     rep("alice-laptop", `"alice laptop"`),
		"id with newline":   rep("alice-laptop", `"alice\nlaptop"`),
		"id with unicode":   rep("alice-laptop", "alicé"),
		"id with colon":     rep("alice-laptop", `"alice:laptop"`),
		"id is a number":    rep("alice-laptop", "12345"),
		"id is a list":      rep("alice-laptop", "[a, b]"),
		// dataDir
		"missing dataDir":    rep("dataDir: /home/alice/.local/share/oxf\n", ""),
		"empty dataDir":      rep("/home/alice/.local/share/oxf", `""`),
		"relative dataDir":   rep("/home/alice/.local/share/oxf", "data/oxf"),
		"tilde dataDir":      rep("/home/alice/.local/share/oxf", "~/x"),
		"dotdot dataDir":     rep("/home/alice/.local/share/oxf", "/home/alice/../bob/oxf"),
		"dot segment":        rep("/home/alice/.local/share/oxf", "/home/./alice/oxf"),
		"double slash":       rep("/home/alice/.local/share/oxf", "/home//alice/oxf"),
		"trailing slash":     rep("/home/alice/.local/share/oxf", "/home/alice/oxf/"),
		"root dataDir":       rep("/home/alice/.local/share/oxf", "/"),
		"newline in dataDir": rep("/home/alice/.local/share/oxf", `"/home/alice/o\nxf"`),
		"tab in dataDir":     rep("/home/alice/.local/share/oxf", `"/home/alice/o\txf"`),
		"dataDir list":       rep("/home/alice/.local/share/oxf", "[/a, /b]"),
		// harnesses
		"missing harnesses": "apiVersion: factory.openagentix.org/v1alpha1\nkind: MachineSettings\nmachineId: a\ndataDir: /d/x\n",
		"empty harnesses":   "apiVersion: factory.openagentix.org/v1alpha1\nkind: MachineSettings\nmachineId: a\ndataDir: /d/x\nharnesses: []\n",
		"null harnesses":    "apiVersion: factory.openagentix.org/v1alpha1\nkind: MachineSettings\nmachineId: a\ndataDir: /d/x\nharnesses:\n",
		"three harnesses":   rep("webPort", "  - { name: codex, path: /usr/local/bin/codex3 }\nwebPort"),
		"duplicate harness": rep("name: codex", "name: claude-code"),
		"stub harness":      rep("name: codex", "name: stub"),
		"unknown harness":   rep("name: codex", "name: aider"),
		"upper case name":   rep("name: codex", "name: Codex"),
		"empty name":        rep("name: codex", `name: ""`),
		"missing name":      rep("name: codex, ", ""),
		"missing path":      rep(", path: /usr/local/bin/codex", ""),
		"relative path":     rep("/usr/local/bin/codex", "codex"),
		"dotdot path":       rep("/usr/local/bin/codex", "/usr/local/../bin/codex"),
		"dash path":         rep("/usr/local/bin/codex", "-/codex"),
		"tilde path":        rep("/usr/local/bin/codex", "~/bin/codex"),
		"newline path":      rep("/usr/local/bin/codex", `"/usr/local/bin/co\ndex"`),
		"escape path":       rep("/usr/local/bin/codex", `"/usr/local/bin/co\x1bdex"`),
		"empty path":        rep("/usr/local/bin/codex", `""`),
		"harness is string": "apiVersion: factory.openagentix.org/v1alpha1\nkind: MachineSettings\nmachineId: a\ndataDir: /d/x\nharnesses: codex\n",
		"harness item list": "apiVersion: factory.openagentix.org/v1alpha1\nkind: MachineSettings\nmachineId: a\ndataDir: /d/x\nharnesses: [[a]]\n",
		// webPort
		"port 1023":   rep("8080", "1023"),
		"port 65536":  rep("8080", "65536"),
		"port 0":      rep("8080", "0"),
		"port -1":     rep("8080", "-1"),
		"port string": rep("8080", `"8080"`),
		"port float":  rep("8080", "8080.5"),
		"port float0": rep("8080", "8080.0"),
		"port hex":    rep("8080", "0x1F90"),
		"port octal":  rep("8080", "0o17620"),
		"port under":  rep("8080", "8_080"),
		"port bool":   rep("8080", "true"),
		"port null":   rep("8080", "null"),
		"port empty":  rep("webPort: 8080", "webPort:"),
		"port huge":   rep("8080", "99999999999999999999"),
		"port list":   rep("8080", "[8080]"),
		"port plus":   rep("8080", "+8080"),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if s, err := Parse([]byte(in)); err == nil {
				t.Fatalf("loaded: %+v", s)
			}
		})
	}
}

func TestParseErrorsNameTheProblem(t *testing.T) {
	_, err := Parse([]byte(strings.Replace(goodYAML, "name: codex", "name: stub", 1)))
	if err == nil || !strings.Contains(err.Error(), "stub") {
		t.Fatalf("err = %v", err)
	}
	_, err = Parse([]byte(strings.Replace(goodYAML, "8080", "1023", 1)))
	if err == nil || !strings.Contains(err.Error(), "webPort") {
		t.Fatalf("err = %v", err)
	}
	_, err = Parse([]byte(strings.Replace(goodYAML, "machineId: alice-laptop\n", "", 1)))
	if err == nil || !strings.Contains(err.Error(), "machineId") {
		t.Fatalf("err = %v", err)
	}
}

func TestPathWithSpaceIsAllowed(t *testing.T) {
	in := strings.Replace(goodYAML, "/usr/local/bin/claude", `"/Users/alice/Library/Application Support/bin/claude"`, 1)
	if _, err := Parse([]byte(in)); err != nil {
		t.Fatalf("a path with a space was refused: %v", err)
	}
}
