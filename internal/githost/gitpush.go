package githost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// tokenEnv is the name of the environment variable that carries the token to
// the credential helper. The value never appears in an argument list.
const tokenEnv = "OXF_GIT_TOKEN"

// credentialHelper is a shell snippet that reads the token from the
// environment. Only the variable name is in the argument list.
const credentialHelper = `!f() { echo username=x-access-token; echo "password=$` + tokenEnv + `"; }; f`

// pusher runs git push with the git program. It uses an argument list, never
// a shell, and never puts the token in an argument, a URL or an error.
type pusher struct {
	remote  string
	gitPath string
	token   string
}

func lookGit() (string, error) { return exec.LookPath("git") }

// newPusher checks the remote and finds git. An empty gitPath means the git
// on the path. The remote must be an https or http URL, a file URL or an
// absolute path, with no user name or password in it.
func newPusher(remote, gitPath, token string) (*pusher, error) {
	switch {
	case remote == "" || strings.HasPrefix(remote, "-"):
		return nil, errors.New("githost: bad remote")
	case strings.HasPrefix(remote, "/"):
	default:
		u, err := url.Parse(remote)
		if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "file") {
			return nil, errors.New("githost: the remote must be an http, https or file URL with no credentials, or an absolute path")
		}
	}
	if gitPath == "" {
		var err error
		if gitPath, err = lookGit(); err != nil {
			return nil, fmt.Errorf("githost: git program not found: %w", err)
		}
	}
	return &pusher{remote: remote, gitPath: gitPath, token: token}, nil
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
	args := []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=" + credentialHelper,
		"-c", "core.hooksPath=/dev/null",
		"-c", "protocol.ext.allow=never",
		"-C", dir,
		"push", "--no-verify", "--porcelain",
	}
	if force {
		args = append(args, "--force")
	}
	args = append(args, p.remote, commit+":refs/heads/"+branch)
	cmd := exec.CommandContext(ctx, p.gitPath, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		tokenEnv + "=" + p.token,
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := redact(strings.TrimSpace(out.String()), p.token)
		if len(text) > 2000 {
			text = text[:2000]
		}
		if ctx.Err() != nil {
			return fmt.Errorf("githost: git push stopped: %w", ctx.Err())
		}
		return fmt.Errorf("githost: git push failed: %v: %s", exitText(err), text)
	}
	return nil
}

func exitText(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit status %d", ee.ExitCode())
	}
	return "could not run git"
}
