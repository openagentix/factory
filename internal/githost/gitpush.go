package githost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// tokenEnv is the name of the environment variable that carries the token to
// the credential helper of one child process. The value never appears in an
// argument list, a URL or a file.
const tokenEnv = "OXF_GIT_TOKEN"

var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)

// pusher sends one commit to one branch with the git program.
//
// The agent may have changed the configuration of the directory it worked in.
// That configuration can redirect a push, name a proxy, or name programs to
// run. So no network git command ever runs in that directory. For each push
// the pusher makes a fresh temporary bare repository that it owns, fetches the
// one commit from the source directory by a local object copy, and pushes from
// the temporary repository. The remote is built from the configured host and
// repository name, and is never read from any repository.
type pusher struct {
	scheme  string // "https", or "http" for a loopback host in tests
	host    string // as git reports it to a credential helper
	remote  string
	gitPath string
	token   string
	// extra holds extra -c options for the push command. Tests set it to
	// point the fixed remote at a local bare repository. It is never set in
	// production code.
	extra []string
}

func lookGit() (string, error) { return exec.LookPath("git") }

func isLoopback(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// newPusher checks the host and finds git. The remote is always an https URL.
// With loopbackHTTP set (tests only), a loopback host gets an http URL.
func newPusher(host string, repo Repo, gitPath, token string, loopbackHTTP bool) (*pusher, error) {
	if !hostRE.MatchString(host) {
		return nil, errors.New("githost: bad git host name")
	}
	if repo.Owner == "" || repo.Name == "" || strings.ContainsAny(repo.Owner+repo.Name, "/ ?#@:\\") {
		return nil, errors.New("githost: bad repository name")
	}
	scheme := "https"
	if loopbackHTTP && isLoopback(host) {
		scheme = "http"
	}
	if gitPath == "" {
		var err error
		if gitPath, err = lookGit(); err != nil {
			return nil, fmt.Errorf("githost: git program not found: %w", err)
		}
	}
	credHost := host
	switch {
	case scheme == "https":
		credHost = strings.TrimSuffix(host, ":443")
	case scheme == "http":
		credHost = strings.TrimSuffix(host, ":80")
	}
	return &pusher{
		scheme: scheme, host: credHost, remote: scheme + "://" + host + "/" + repo.Owner + "/" + repo.Name + ".git",
		gitPath: gitPath, token: token,
	}, nil
}

// credentialHelper returns a helper that gives the token only when git asks
// for exactly the expected protocol and host. For anything else it gives
// nothing. It is an inline snippet, so no file holds it, and the token comes
// from the environment of the one child process.
func (p *pusher) credentialHelper() string {
	return `!f() { test "$1" = get || exit 0; p=; h=; while IFS== read -r k v; do case "$k" in protocol) p="$v";; host) h="$v";; esac; done; ` +
		`if [ "$p" = '` + p.scheme + `' ] && [ "$h" = '` + p.host + `' ]; then echo username=x-access-token; echo "password=$` + tokenEnv + `"; fi; }; f`
}

// pushConfig is the -c list of the push command.
func (p *pusher) pushConfig() []string {
	allow := "protocol." + p.scheme + ".allow=always"
	cfg := []string{
		"-c", "protocol.allow=never", "-c", allow,
		"-c", "credential.helper=" + p.credentialHelper(),
		"-c", "http.proxy=", "-c", "http.followRedirects=false",
	}
	return append(cfg, p.extra...)
}

// gitCall is one git command. All git commands in this package go through
// pusher.git, and nowhere else builds one.
type gitCall struct {
	dir   string
	cfg   []string // extra -c options, as pairs "-c", "k=v"
	args  []string
	stdin string
	token bool // pass the token in the environment of this child only
}

// git is the one place that starts the git program. It uses an argument list,
// a fixed set of safe options and an explicit environment.
func (p *pusher) git(ctx context.Context, tmpHome string, c gitCall) (string, error) {
	args := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=user",
		"-c", "transfer.fsckObjects=true",
	}
	args = append(args, c.cfg...)
	if c.dir != "" {
		args = append(args, "-C", c.dir)
	}
	args = append(args, c.args...)
	cmd := exec.CommandContext(ctx, p.gitPath, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"HOME=" + tmpHome,
		"XDG_CONFIG_HOME=" + tmpHome,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	}
	if c.token {
		cmd.Env = append(cmd.Env, tokenEnv+"="+p.token)
	}
	if c.stdin != "" {
		cmd.Stdin = strings.NewReader(c.stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := redact(strings.TrimSpace(out.String()), p.token)
		if len(text) > 2000 {
			text = text[:2000]
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("githost: git stopped: %w", ctx.Err())
		}
		return "", fmt.Errorf("githost: git %s failed: %v: %s", firstArg(c.args), exitText(err), text)
	}
	return redact(strings.TrimSpace(out.String()), p.token), nil
}

func firstArg(a []string) string {
	if len(a) > 0 {
		return a[0]
	}
	return ""
}

// push sends one commit to one branch of the remote. The callers have
// already applied the push rules.
func (p *pusher) push(ctx context.Context, dir, commit, branch string, force bool) error {
	if err := checkCommit(commit); err != nil {
		return err
	}
	if err := checkBranchName(branch); err != nil {
		return err
	}
	src, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "oxf-push-")
	if err != nil {
		return fmt.Errorf("githost: cannot make a work directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o700); err != nil {
		return err
	}
	repo := filepath.Join(tmp, "repo")

	if _, err := p.git(ctx, tmp, gitCall{args: []string{"init", "--bare", "--quiet", repo}}); err != nil {
		return err
	}
	// A local object copy of exactly one commit. Nothing from the source
	// repository's configuration is used by this repository.
	const ref = "refs/oxf/push"
	_, err = p.git(ctx, tmp, gitCall{
		dir:  repo,
		cfg:  []string{"-c", "uploadpack.packObjectsHook=", "-c", "uploadpack.allowAnySHA1InWant=true"},
		args: []string{"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--", src, "+" + commit + ":" + ref},
	})
	if err != nil {
		return err
	}
	got, err := p.git(ctx, tmp, gitCall{dir: repo, args: []string{"rev-parse", "--verify", "--quiet", ref + "^{commit}"}})
	if err != nil || got != commit {
		return errors.New("githost: the commit is not in the source directory")
	}

	args := []string{"push", "--no-verify", "--porcelain"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", p.remote, commit+":refs/heads/"+branch)
	_, err = p.git(ctx, tmp, gitCall{dir: repo, cfg: p.pushConfig(), args: args, token: true})
	return err
}

func exitText(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit status %d", ee.ExitCode())
	}
	return "could not run git"
}
