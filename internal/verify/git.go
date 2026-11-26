package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitEnv is the environment of every git call the verify stage makes. It
// drops the caller's GIT_ variables, so that nothing redirects git to another
// repository, and it ignores the user and system git config, so that no
// setting can run a program on our behalf.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
}

// runGit runs git with an argument list in dir. There is no shell.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.Join(strings.Fields(stderr.String()), " ")
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return stdout.Bytes(), nil
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
