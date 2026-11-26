package repo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hardenedGitConfig is put before every git command. It is a second layer.
// The first is the guard (see checkTrusted), which refuses any repository
// whose configuration the factory did not write. These settings switch off
// what can start a program: the file system monitor, hooks, a pager, an
// external diff and the ext transport.
var hardenedGitConfig = []string{
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.pager=cat",
	"-c", "diff.external=",
	"-c", "protocol.ext.allow=never",
	"-c", "commit.gpgsign=false",
	// No command may start background maintenance: it makes lock files in
	// objects/ that the guard rightly refuses while they exist.
	"-c", "maintenance.auto=false",
	"-c", "gc.auto=0",
}

// gitEnvAllowlist names the variables git may take from this process.
var gitEnvAllowlist = []string{"PATH", "LANG", "LC_ALL", "TMPDIR"}

// gitEnv is the environment of every git command. It is built from an
// allowlist and never copies the process environment, so a token in the
// orchestrator's environment cannot reach git, or a program git starts. The
// user and system git config are ignored.
func gitEnv() []string {
	var env []string
	for _, name := range gitEnvAllowlist {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_CEILING_DIRECTORIES=/",
	)
}

// buildGit builds every git command of this package. It is the only place
// that starts the git program, and a test keeps it that way. There is no
// shell: args is an argument list.
//
// gitDir is the git directory of a bare repository. The command is tied to it
// with --git-dir, so git never searches the file system for another
// repository, and replace objects are off. An empty gitDir is for the few
// commands that need no repository (git init, git config --file): they run in
// a new empty directory that the returned function removes. It is not the
// system temporary directory itself, where another process could have planted
// a .git. GIT_CEILING_DIRECTORIES is set for every command, so git never
// looks above the directory it starts in. Extra variables are each in
// NAME=value form and must be named in identityEnv, or they are dropped.
// The command runs in its own process group, and the whole group is killed
// when ctx ends.
func buildGit(ctx context.Context, gitDir string, extra []string, args []string) (*exec.Cmd, func(), error) {
	full := append([]string{}, hardenedGitConfig...)
	cleanup := func() {}
	var dir, ceiling string
	if gitDir != "" {
		if abs, err := filepath.Abs(gitDir); err == nil {
			gitDir = abs
		}
		full = append(full, "--git-dir="+gitDir, "--no-replace-objects")
		dir, ceiling = gitDir, filepath.Dir(gitDir)
	} else {
		d, err := os.MkdirTemp("", "oxf-git-")
		if err != nil {
			return nil, nil, err
		}
		dir, ceiling = d, filepath.Dir(d)
		cleanup = func() { _ = os.RemoveAll(d) }
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(), "GIT_CEILING_DIRECTORIES="+ceiling)
	for _, kv := range extra {
		if name, _, ok := strings.Cut(kv, "="); ok && identityEnv[name] {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	ownProcessGroup(cmd)
	return cmd, cleanup, nil
}

// gitCommand and gitCommandEnv build a command on a repository, for tests.
// gitDir must not be empty.
func gitCommand(ctx context.Context, gitDir string, args ...string) *exec.Cmd {
	return gitCommandEnv(ctx, gitDir, nil, args...)
}

func gitCommandEnv(ctx context.Context, gitDir string, extra []string, args ...string) *exec.Cmd {
	cmd, _, _ := buildGit(ctx, gitDir, extra, args)
	return cmd
}

// identityEnv names the only extra variables a caller may add to the git
// environment: the identity and time of a commit.
var identityEnv = map[string]bool{
	"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true, "GIT_AUTHOR_DATE": true,
	"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true, "GIT_COMMITTER_DATE": true,
}

// runGit runs git and returns its standard output.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGitIn(ctx, dir, nil, nil, args...)
}

// runGitIn runs git with a reader as standard input and extra identity
// variables, and returns its standard output.
func runGitIn(ctx context.Context, dir string, stdin io.Reader, extraEnv []string, args ...string) ([]byte, error) {
	cmd, cleanup, err := buildGit(ctx, dir, extraEnv, args)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, gitError(args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func gitError(args []string, err error, stderr string) error {
	msg := strings.Join(strings.Fields(stderr), " ")
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	return fmt.Errorf("git %s: %w: %s", args[0], err, msg)
}

// runGitStream runs git with a reader as standard input and a function that
// reads standard output while the command runs.
func runGitStream(ctx context.Context, dir string, stdin io.Reader, read func(io.Reader) error, args ...string) error {
	cmd, cleanup, err := buildGit(ctx, dir, nil, args)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return gitError(args, err, "")
	}
	readErr := read(out)
	_, _ = io.Copy(io.Discard, out)
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return gitError(args, waitErr, stderr.String())
	}
	return nil
}

// resolveCommit turns a revision into a full commit id. A revision that
// starts with "-" is refused, so it can never be read as an option.
func resolveCommit(ctx context.Context, dir, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("bad revision %q", rev)
	}
	out, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("revision %q is not a commit", rev)
	}
	return strings.TrimSpace(string(out)), nil
}
