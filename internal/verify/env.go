package verify

import "strings"

// envAllowlist is the only set of variables a check or a git-free child
// process starts with. Everything else is dropped, so a variable the
// factory does not know about cannot leak.
var envAllowlist = map[string]bool{
	"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TMPDIR": true, "TERM": true,
}

// sensitiveName reports whether a variable name looks like a credential.
// Names are compared without regard to case.
func sensitiveName(name string) bool {
	n := strings.ToUpper(name)
	for _, part := range []string{"TOKEN", "SECRET", "PASSWORD", "API_KEY"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	for _, prefix := range []string{"ANTHROPIC_", "OPENAI_", "CLAUDE_", "CODEX_", "GH_", "GITHUB_", "AWS_"} {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return n == "SSH_AUTH_SOCK"
}

// ScrubEnv returns the part of environ (a list of NAME=value) that a check
// may see. A name must be on the allowlist and must not look like a
// credential. The second test is a second wall: it keeps working if the
// allowlist is ever widened by mistake.
func ScrubEnv(environ []string) []string {
	var out []string
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || !envAllowlist[name] || sensitiveName(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
