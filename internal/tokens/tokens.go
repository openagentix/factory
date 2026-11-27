// Package tokens reads the propose and accept tokens of a repository from the
// operating system's own credential tool (record 0003, Tokens).
//
// The tool is fixed and has an absolute path: /usr/bin/security on macOS and
// /usr/bin/secret-tool on Linux. PATH is never used. The token comes from the
// tool's standard output and from nowhere else: never an environment
// variable, never a file, no fallback. It is never put in an argument list,
// an error, a log or a formatted value.
package tokens

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/redact"
)

// Source gives the tokens of one repository.
//
// Propose returns the propose token. A missing token is an error.
//
// Accept returns the accept token. found is false, with a nil error, only when
// the tool says for certain that no such token is stored. Any other trouble is
// an error: a failing tool is never read as an absent token.
type Source interface {
	Propose(ctx context.Context, repository string) (githost.ProposeToken, error)
	Accept(ctx context.Context, repository string) (tok githost.AcceptToken, found bool, err error)
}

const (
	maxToken       = 512
	maxOutput      = 4096
	defaultLimit   = 10 * time.Second
	defaultWaitFor = 2 * time.Second
	securityPath   = "/usr/bin/security"
	secretToolPath = "/usr/bin/secret-tool"
)

type toolKind int

const (
	kindSecurity   toolKind = iota // macOS
	kindSecretTool                 // Linux
)

type system struct {
	tool      string
	kind      toolKind
	timeout   time.Duration
	waitDelay time.Duration
	env       []string
	// onSecret receives each token read, so that the caller can build a
	// redact filter. It is set in code only.
	onSecret func(string)
}

// NewSystem returns the source for this operating system. Other systems are
// not supported and get an error. onSecret, which may be nil, is called with
// each token that was read and passed every check, before it is returned.
func NewSystem(onSecret func(secret string)) (Source, error) {
	tool, kind, err := systemTool(runtime.GOOS)
	if err != nil {
		return nil, err
	}
	uid := strconv.Itoa(os.Getuid())
	home := ""
	if kind == kindSecurity {
		// On macOS os/user asks the system user database (getpwuid_r), with or
		// without cgo. The user must be the one this process runs as.
		u, err := user.Current()
		if err != nil || u.Uid != uid {
			return nil, errors.New("tokens: cannot find the home directory of the user")
		}
		home = u.HomeDir
	}
	env, err := scrubbedEnv(runtime.GOOS, home, uid)
	if err != nil {
		return nil, err
	}
	s := newSource(tool, kind, onSecret)
	s.env = env
	return s, nil
}

func newSource(tool string, kind toolKind, onSecret func(string)) *system {
	return &system{tool: tool, kind: kind, timeout: defaultLimit, waitDelay: defaultWaitFor, onSecret: onSecret}
}

// systemTool is the fixed tool for an operating system.
func systemTool(goos string) (string, toolKind, error) {
	switch goos {
	case "darwin":
		return securityPath, kindSecurity, nil
	case "linux":
		return secretToolPath, kindSecretTool, nil
	}
	return "", 0, fmt.Errorf("tokens: %q is not supported; only macOS and Linux with a desktop secret service are", goos)
}

// checkTool refuses a tool path that is not absolute and clean.
func checkTool(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return "", errors.New("tokens: the tool path must be absolute")
	}
	return p, nil
}

// scrubbedEnv is the whole environment of the tool. Nothing is copied from
// the environment of oxf.
//
// On macOS the tool needs HOME to find the keychain. The caller takes it from
// the system user database (see NewSystem). On Linux no HOME is passed at all:
// secret-tool talks to the secret service over D-Bus and needs no home
// directory. This also means that the Linux code never has to trust os/user,
// which in a build without cgo can fall back to $HOME.
func scrubbedEnv(goos, home, uid string) ([]string, error) {
	if n, err := strconv.Atoi(uid); err != nil || n < 0 {
		return nil, errors.New("tokens: the user id is not a number")
	}
	env := []string{"PATH=/usr/bin:/bin", "LANG=C"}
	switch goos {
	case "darwin":
		if home == "" || !filepath.IsAbs(home) || strings.ContainsAny(home, "\x00\r\n=") {
			return nil, errors.New("tokens: the home directory of the user is not an absolute path")
		}
		env = append(env, "HOME="+home)
	case "linux":
		env = append(env, "XDG_RUNTIME_DIR=/run/user/"+uid, "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/"+uid+"/bus")
	default:
		return nil, fmt.Errorf("tokens: %q is not supported", goos)
	}
	return env, nil
}

var (
	ownerPattern = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9-]{0,38}\z`)
	namePattern  = regexp.MustCompile(`\A[A-Za-z0-9._][A-Za-z0-9._-]{0,99}\z`)
)

// account is the account name for a repository and a role. The repository
// must be a strict owner/name. An account never starts with a dash, so it can
// never be read as a flag.
func account(repo, role string) (string, error) {
	if role != "propose" && role != "accept" {
		return "", errors.New("tokens: unknown token role")
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || !ownerPattern.MatchString(owner) || !namePattern.MatchString(name) || name == "." || name == ".." {
		return "", errors.New("tokens: the repository must be written owner/name, with letters, digits, dots, dashes and underscores only")
	}
	return owner + "/" + name + "/" + role, nil
}

func (s *system) args(acct string) []string {
	if s.kind == kindSecretTool {
		return []string{"lookup", "service", "oxf", "account", acct}
	}
	return []string{"find-generic-password", "-s", "oxf", "-a", acct, "-w"}
}

// isNotFound is the one result that means "no such token is stored".
func (s *system) isNotFound(r result) bool {
	if r.code < 0 || len(r.stdout) != 0 {
		return false
	}
	switch s.kind {
	case kindSecurity:
		return r.code == 44 // errSecItemNotFound
	default:
		// UNVERIFIED: secret-tool exits 1 with no output when nothing matches.
		// It has not been checked that a dismissed unlock prompt gives
		// something else. If it also gives exit 1 with no output, it is read
		// as "no accept token". That fails toward less power: the run has no
		// accept token and cannot accept anything. For the propose token the
		// same result is an error.
		return r.code == 1 && r.stderrBytes == 0
	}
}

// fetch runs the tool and returns the token, or found=false for a real
// "not found". Errors never hold the output of the tool.
func (s *system) fetch(ctx context.Context, repo, role string) (tok string, found bool, err error) {
	acct, err := account(repo, role)
	if err != nil {
		return "", false, err
	}
	r, err := s.run(ctx, s.args(acct))
	if err != nil {
		return "", false, fmt.Errorf("tokens: reading the %s token of %s: %w", role, repo, err)
	}
	if r.code != 0 {
		if s.isNotFound(r) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("tokens: the credential tool failed while reading the %s token of %s (exit code %d)", role, repo, r.code)
	}
	tok, err = cleanToken(r.stdout)
	if err != nil {
		return "", false, fmt.Errorf("tokens: the %s token of %s is not usable: %w", role, repo, err)
	}
	return tok, true, nil
}

// cleanToken removes one line end from the output and checks the rest. A
// token is 8 to 512 printable ASCII characters with no white space.
func cleanToken(out []byte) (string, error) {
	s := string(out)
	if t, ok := strings.CutSuffix(s, "\r\n"); ok {
		s = t
	} else if t, ok := strings.CutSuffix(s, "\n"); ok {
		s = t
	}
	switch {
	case s == "":
		return "", errors.New("the tool printed nothing")
	case len(s) > maxToken:
		return "", errors.New("the value is too long")
	case len(s) < redact.MinSecretLen:
		return "", errors.New("the value is too short")
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= 0x20 || c >= 0x7f {
			return "", errors.New("the value holds a space, a control character or a character that is not ASCII")
		}
	}
	return s, nil
}

func (s *system) report(tok string) {
	if s.onSecret != nil {
		s.onSecret(tok)
	}
}

// Propose implements Source.
func (s *system) Propose(ctx context.Context, repository string) (githost.ProposeToken, error) {
	tok, found, err := s.fetch(ctx, repository, "propose")
	if err != nil {
		return githost.ProposeToken{}, err
	}
	if !found {
		return githost.ProposeToken{}, fmt.Errorf("tokens: no propose token is stored for %s", repository)
	}
	t, err := githost.NewProposeToken(tok)
	if err != nil {
		return githost.ProposeToken{}, errors.New("tokens: the propose token was refused")
	}
	s.report(tok)
	return t, nil
}

// Accept implements Source. It is the only way to read the accept token, and
// it reads the propose token too: one value must never serve as both, because
// then a run that may only propose would hold the power to accept. When the
// propose token cannot be read, or equals the accept token, the accept token is
// refused. Callers must not add another way around this check.
func (s *system) Accept(ctx context.Context, repository string) (githost.AcceptToken, bool, error) {
	tok, found, err := s.fetch(ctx, repository, "accept")
	if err != nil || !found {
		return githost.AcceptToken{}, false, err
	}
	ptok, pfound, err := s.fetch(ctx, repository, "propose")
	if err != nil || !pfound {
		return githost.AcceptToken{}, false, fmt.Errorf("tokens: the accept token of %s is refused because the propose token cannot be read to check that they differ", repository)
	}
	s.report(ptok)
	if subtle.ConstantTimeCompare([]byte(tok), []byte(ptok)) == 1 {
		// The value was reported above, so a redact filter holds it.
		return githost.AcceptToken{}, false, fmt.Errorf("tokens: the accept token of %s is refused because it is the same value as the propose token", repository)
	}
	t, err := githost.NewAcceptToken(tok)
	if err != nil {
		return githost.AcceptToken{}, false, errors.New("tokens: the accept token was refused")
	}
	s.report(tok)
	return t, true, nil
}
