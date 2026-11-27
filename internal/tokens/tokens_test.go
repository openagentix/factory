package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goodToken = "ghp_abcdefghijklmnopqrstuvwx"
const canary = "CANARYtoken0123456789"

type rig struct {
	t       *testing.T
	dir     string
	src     *system
	secrets []string
}

// newRig writes a fake tool script into a temporary directory. D is the
// directory, so a script can leave marks there.
func newRig(t *testing.T, kind toolKind, body string) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir()}
	tool := filepath.Join(r.dir, "fake-tool")
	script := "#!/bin/sh\nD='" + r.dir + "'\n" + body + "\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	r.src = newSource(tool, kind, func(s string) { r.secrets = append(r.secrets, s) })
	r.src.timeout = 5 * time.Second
	r.src.waitDelay = time.Second
	r.src.env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent-home", "LANG=C"}
	return r
}

func (r *rig) ran() bool {
	_, err := os.Stat(filepath.Join(r.dir, "ran"))
	return err == nil
}

func (r *rig) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(r.dir, name))
	return string(b)
}

func printTok(s string) string { return "touch \"$D/ran\"\nprintf '%b' '" + s + "'\n" }

func TestProposeReadsToken(t *testing.T) {
	r := newRig(t, kindSecurity, printTok(goodToken+`\n`))
	tok, err := r.src.Propose(context.Background(), "example/repo")
	if err != nil {
		t.Fatal(err)
	}
	_ = tok
	if len(r.secrets) != 1 || r.secrets[0] != goodToken {
		t.Fatalf("callback saw %d values, want the token once", len(r.secrets))
	}
}

func TestArgumentListPerTool(t *testing.T) {
	cases := map[toolKind]string{
		kindSecurity:   "find-generic-password -s oxf -a example/repo/propose -w",
		kindSecretTool: "lookup service oxf account example/repo/propose",
	}
	for kind, want := range cases {
		r := newRig(t, kind, "echo \"$*\" > \"$D/args\"\n"+printTok(goodToken+`\n`))
		if _, err := r.src.Propose(context.Background(), "example/repo"); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(r.read("args")); got != want {
			t.Errorf("kind %d: args %q want %q", kind, got, want)
		}
		if strings.Contains(r.read("args"), goodToken) {
			t.Error("token in the argument list")
		}
	}
	r := newRig(t, kindSecurity, "echo \"$*\" > \"$D/args\"\n"+printTok(goodToken+`\n`))
	if _, found, err := r.src.Accept(context.Background(), "example/repo"); err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got := strings.TrimSpace(r.read("args")); !strings.Contains(got, "-a example/repo/accept ") {
		t.Errorf("args %q", got)
	}
}

// Attack 1: a program named like the tool, first on PATH, is never run.
func TestPathIsNeverUsed(t *testing.T) {
	r := newRig(t, kindSecurity, printTok(goodToken+`\n`))
	evil := t.TempDir()
	for _, name := range []string{"security", "secret-tool", "fake-tool"} {
		p := filepath.Join(evil, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch '"+evil+"/evil-ran'\necho evilevilevil\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", evil+":"+os.Getenv("PATH"))
	if _, err := r.src.Propose(context.Background(), "example/repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(evil, "evil-ran")); err == nil {
		t.Fatal("a program from PATH ran")
	}
	if len(r.secrets) != 1 || r.secrets[0] != goodToken {
		t.Fatalf("token did not come from the fixed tool")
	}
}

func TestSystemToolPathsAreFixedAndAbsolute(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "/usr/bin/security", "linux": "/usr/bin/secret-tool"} {
		got, _, err := systemTool(goos)
		if err != nil || got != want {
			t.Errorf("%s: %q, %v", goos, got, err)
		}
	}
	for _, goos := range []string{"windows", "freebsd", ""} {
		if _, _, err := systemTool(goos); err == nil {
			t.Errorf("%q accepted", goos)
		}
	}
}

func TestToolPathMustBeAbsolute(t *testing.T) {
	if _, err := checkTool("security"); err == nil {
		t.Error("relative tool path accepted")
	}
	if _, err := checkTool(""); err == nil {
		t.Error("empty tool path accepted")
	}
}

// Attack 2: the environment is never read and never passed on.
func TestEnvironmentIsNeverReadOrPassed(t *testing.T) {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN", "OXF_TOKEN", "OXF_PROPOSE_TOKEN"} {
		t.Setenv(name, "ENVCANARY-"+name)
	}
	r := newRig(t, kindSecurity, "env > \"$D/env\"\n"+printTok(goodToken+`\n`))
	if _, err := r.src.Propose(context.Background(), "example/repo"); err != nil {
		t.Fatal(err)
	}
	if e := r.read("env"); strings.Contains(e, "ENVCANARY") || strings.Contains(e, "GITHUB_TOKEN") {
		t.Fatalf("the tool saw the environment:\n%s", e)
	}
	// An empty answer is an error, never a fallback to the environment.
	r = newRig(t, kindSecurity, "exit 0")
	if _, err := r.src.Propose(context.Background(), "example/repo"); err == nil {
		t.Fatal("empty answer accepted")
	}
	if len(r.secrets) != 0 {
		t.Fatal("a secret was reported")
	}
}

func TestScrubbedEnvKeys(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ENVCANARY")
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "XDG_RUNTIME_DIR": true, "DBUS_SESSION_BUS_ADDRESS": true}
	for _, goos := range []string{"darwin", "linux"} {
		env, err := scrubbedEnv(goos, "/home/alice", "1000")
		if err != nil {
			t.Fatal(err)
		}
		hasHome := false
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			if !allowed[k] {
				t.Errorf("%s: key %q passed to the tool", goos, k)
			}
			if strings.Contains(v, "ENVCANARY") {
				t.Errorf("value from the environment: %s", kv)
			}
			if k == "HOME" && v == "/home/alice" {
				hasHome = true
			}
		}
		if !hasHome {
			t.Errorf("%s: no HOME", goos)
		}
	}
	if _, err := scrubbedEnv("linux", "relative/home", "1000"); err == nil {
		t.Error("relative home accepted")
	}
	if _, err := scrubbedEnv("linux", "", "1000"); err == nil {
		t.Error("empty home accepted")
	}
}

func TestStdinIsEmpty(t *testing.T) {
	r := newRig(t, kindSecurity, "cat > \"$D/stdin\"\n"+printTok(goodToken+`\n`))
	if _, err := r.src.Propose(context.Background(), "example/repo"); err != nil {
		t.Fatal(err)
	}
	if got := r.read("stdin"); got != "" {
		t.Fatalf("stdin held %q", got)
	}
}

// Attack 3 and 4: what the tool can do wrong.
func TestToolMisbehaviour(t *testing.T) {
	cases := map[string]string{
		"exit nonzero":        `exit 3`,
		"exit zero, empty":    `exit 0`,
		"only a newline":      printTok(`\n`),
		"two lines":           printTok(goodToken + `\n` + goodToken + `\n`),
		"token and more":      printTok(goodToken + `\nextra\n`),
		"one MiB":             "head -c 1048576 /dev/zero | tr '\\000' a",
		"then hangs":          printTok(goodToken+`\n`) + "exec sleep 30",
		"never exits":         "exec sleep 30",
		"leading spaces":      printTok(`   ` + goodToken + `\n`),
		"trailing spaces":     printTok(goodToken + `   \n`),
		"inner space":         printTok(`ghp_abcdefgh ijklmnop\n`),
		"NUL inside":          printTok(`ghp_abcdefgh\000ijklmnop\n`),
		"escape sequence":     printTok(`ghp_abcdefgh\033[31mijklmnop\n`),
		"tab inside":          printTok(`ghp_abcdefgh\tijklmnop\n`),
		"carriage return mid": printTok(`ghp_abcdefgh\rijklmnop\n`),
		"too short":           printTok(`abc\n`),
		"too long":            "printf 'a%.0s' $(seq 1 600); echo",
		"non-ASCII":           printTok(`ghp_abcdefgh\303\251ijklmnop\n`),
		"two newlines":        printTok(goodToken + `\n\n`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, kindSecurity, body)
			r.src.timeout = 400 * time.Millisecond
			r.src.waitDelay = 200 * time.Millisecond
			_, err := r.src.Propose(context.Background(), "example/repo")
			if err == nil {
				t.Fatal("accepted")
			}
			if len(r.secrets) != 0 {
				t.Fatal("a bad token was reported as a secret")
			}
			_, found, err := r.src.Accept(context.Background(), "example/repo")
			if err == nil || found {
				t.Fatalf("accept: found=%v err=%v", found, err)
			}
			for _, bad := range []string{"abcdefgh", "ijklmnop", "aaaaaaaa"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("error shows token text: %v", err)
				}
			}
		})
	}
}

func TestTrimsExactlyOneLineEnd(t *testing.T) {
	for name, out := range map[string]string{"lf": goodToken + `\n`, "crlf": goodToken + `\r\n`, "none": goodToken} {
		r := newRig(t, kindSecurity, printTok(out))
		if _, err := r.src.Propose(context.Background(), "example/repo"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(r.secrets) != 1 || r.secrets[0] != goodToken {
			t.Fatalf("%s: got %q", name, r.secrets)
		}
	}
}

func TestStderrIsNeverCopied(t *testing.T) {
	r := newRig(t, kindSecurity, "echo 'ghp_STDERRCANARY0123456789 leaked' >&2\nexit 3")
	_, err := r.src.Propose(context.Background(), "example/repo")
	if err == nil || strings.Contains(err.Error(), "STDERRCANARY") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeadlineReturnsAndKillsTheGroup(t *testing.T) {
	r := newRig(t, kindSecurity, "echo $$ > \"$D/pid\"\nsleep 30\n")
	r.src.timeout = 300 * time.Millisecond
	r.src.waitDelay = 200 * time.Millisecond
	start := time.Now()
	_, err := r.src.Propose(context.Background(), "example/repo")
	if err == nil {
		t.Fatal("no error")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestContextCancelStopsTheCall(t *testing.T) {
	r := newRig(t, kindSecurity, "exec sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.src.Propose(ctx, "example/repo"); err == nil {
		t.Fatal("no error")
	}
}

// Attack 5: only a real "not found" is absent.
func TestAcceptNotFoundRulePerTool(t *testing.T) {
	type tc struct {
		name  string
		kind  toolKind
		body  string
		found bool
		isErr bool
	}
	cases := []tc{
		{"security 44 empty", kindSecurity, "echo 'item not found' >&2\nexit 44", false, false},
		{"security 44 with output", kindSecurity, "echo something\nexit 44", false, true},
		{"security 1", kindSecurity, "exit 1", false, true},
		{"security 45", kindSecurity, "exit 45", false, true},
		{"security 128", kindSecurity, "exit 128", false, true},
		{"security found", kindSecurity, printTok(goodToken + `\n`), true, false},
		{"security 0 empty", kindSecurity, "exit 0", false, true},
		{"secret-tool 1 silent", kindSecretTool, "exit 1", false, false},
		{"secret-tool 1 with stderr", kindSecretTool, "echo 'cannot connect to dbus' >&2\nexit 1", false, true},
		{"secret-tool 1 with stdout", kindSecretTool, "echo x\nexit 1", false, true},
		{"secret-tool 2", kindSecretTool, "exit 2", false, true},
		{"secret-tool 44", kindSecretTool, "exit 44", false, true},
		{"secret-tool 0 empty", kindSecretTool, "exit 0", false, true},
		{"secret-tool found", kindSecretTool, printTok(goodToken + `\n`), true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.kind, c.body)
			_, found, err := r.src.Accept(context.Background(), "example/repo")
			if (err != nil) != c.isErr || found != c.found {
				t.Fatalf("found=%v err=%v", found, err)
			}
			if !c.found && len(r.secrets) != 0 {
				t.Fatal("secret reported for an absent token")
			}
		})
	}
}

func TestProposeNotFoundIsAnError(t *testing.T) {
	for kind, body := range map[toolKind]string{kindSecurity: "exit 44", kindSecretTool: "exit 1"} {
		r := newRig(t, kind, body)
		if _, err := r.src.Propose(context.Background(), "example/repo"); err == nil {
			t.Errorf("kind %d: absent propose token accepted", kind)
		}
	}
}

func TestMissingToolIsAnErrorForBoth(t *testing.T) {
	src := newSource(filepath.Join(t.TempDir(), "nope"), kindSecurity, nil)
	if _, err := src.Propose(context.Background(), "example/repo"); err == nil {
		t.Error("propose")
	}
	if _, found, err := src.Accept(context.Background(), "example/repo"); err == nil || found {
		t.Errorf("accept found=%v err=%v", found, err)
	}
	dir := newSource(t.TempDir(), kindSecurity, nil)
	if _, found, err := dir.Accept(context.Background(), "example/repo"); err == nil || found {
		t.Errorf("directory as tool: found=%v err=%v", found, err)
	}
}

// Attack 6: the repository name builds the account.
func TestRepositoryNameIsStrict(t *testing.T) {
	bad := []string{
		"", "example", "example/", "/repo", "a/b/c", "exa mple/repo", "example/re po", "-example/repo",
		"example/-repo", "../repo", "example/..", "example/.", "./repo", "example/repo\n", "exam\nple/repo",
		"example/repo\x00", "exa\tmple/repo", strings.Repeat("a", 300) + "/repo", "example/" + strings.Repeat("a", 300),
		"example/repo/propose", "exa$mple/repo", "example/re;po", "example/re`po", "exa:mple/repo", "example\\repo",
		"ex_ample/repo", "exa.mple/repo", "-a/repo", "example/repo ", " example/repo", "éxample/repo",
	}
	for _, name := range bad {
		r := newRig(t, kindSecurity, printTok(goodToken+`\n`))
		if _, err := r.src.Propose(context.Background(), name); err == nil {
			t.Errorf("propose accepted %q", name)
		}
		if _, _, err := r.src.Accept(context.Background(), name); err == nil {
			t.Errorf("accept accepted %q", name)
		}
		if r.ran() {
			t.Errorf("the tool ran for %q", name)
		}
	}
	good := []string{"example/repo", "Example/Repo", "a/b", "ex-ample/re.po_1", "example/.github", "a1/" + strings.Repeat("b", 100)}
	for _, name := range good {
		r := newRig(t, kindSecurity, printTok(goodToken+`\n`))
		if _, err := r.src.Propose(context.Background(), name); err != nil {
			t.Errorf("%q refused: %v", name, err)
		}
	}
}

func TestAccountNeverStartsWithADash(t *testing.T) {
	for _, repo := range []string{"example/repo", "a/-b", "-a/b"} {
		acct, err := account(repo, "propose")
		if err == nil && strings.HasPrefix(acct, "-") {
			t.Errorf("%q gives account %q", repo, acct)
		}
	}
	if _, err := account("example/repo", "merge"); err == nil {
		t.Error("an unknown role was accepted")
	}
}

// Attack 7: the token never shows, in any form.
func TestNothingShowsTheToken(t *testing.T) {
	scripts := map[string]string{
		"found":     printTok(canary + `\n`),
		"inner":     printTok(canary + ` more\n`),
		"nul":       printTok(canary + `\000x\n`),
		"two lines": printTok(canary + `\n` + canary + `\n`),
		"stderr":    "echo " + canary + " >&2\nexit 3",
		"stdout+rc": "echo " + canary + "\nexit 3",
		"long":      printTok(strings.Repeat(canary, 60) + `\n`),
	}
	for name, body := range scripts {
		r := newRig(t, kindSecurity, body)
		p, perr := r.src.Propose(context.Background(), "example/repo")
		a, _, aerr := r.src.Accept(context.Background(), "example/repo")
		var shown []string
		for _, e := range []error{perr, aerr} {
			for ; e != nil; e = errors.Unwrap(e) {
				shown = append(shown, e.Error())
			}
		}
		for _, v := range []any{p, a, &p, &a, r.src} {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
				shown = append(shown, fmt.Sprintf(verb, v))
			}
			if b, err := json.Marshal(v); err == nil {
				shown = append(shown, string(b))
			}
		}
		func() {
			defer func() { shown = append(shown, fmt.Sprint(recover())) }()
			panic(fmt.Sprintf("%v %v", perr, p))
		}()
		for _, s := range shown {
			if strings.Contains(s, canary) || strings.Contains(s, "CANARYtoken") {
				t.Errorf("%s: the token shows in %q", name, s)
			}
		}
	}
}
