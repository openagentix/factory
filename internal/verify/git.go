package verify

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
	)
}

// gitCommand builds every git command of this package. It is the only place
// that starts the git program, and a test keeps it that way. There is no
// shell: args is an argument list.
//
// gitDir is the git directory of a bare repository. The command is tied to it
// with --git-dir, so git never searches the file system for another
// repository, and replace objects are off. An empty gitDir is for the few
// commands that need no repository: they run in the temporary directory.
func gitCommand(ctx context.Context, gitDir string, args ...string) *exec.Cmd {
	full := append([]string{}, hardenedGitConfig...)
	dir := os.TempDir()
	if gitDir != "" {
		if abs, err := filepath.Abs(gitDir); err == nil {
			gitDir = abs
		}
		full = append(full, "--git-dir="+gitDir, "--no-replace-objects")
		dir = gitDir
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	return cmd
}

// runGit runs git and returns its standard output.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := gitCommand(ctx, dir, args...)
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
	cmd := gitCommand(ctx, dir, args...)
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
