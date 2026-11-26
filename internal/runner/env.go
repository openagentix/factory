package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// baseEnv are the only variables copied from the factory's environment.
var baseEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TERM", "SHELL"}

var (
	keyNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	// hostSecret matches names that carry a git host or a login: tokens,
	// credential helpers, the SSH agent socket, askpass programs.
	hostSecret = regexp.MustCompile(`(?i)(^GIT|GITHUB|GITLAB|^GH_|SSH|CREDENTIAL|NETRC|ASKPASS|SOCK|PASSWORD|^HOME$|^PATH$|^TMPDIR$|^FACTORY_)`)
)

// buildEnv makes the environment of the agent from an allowlist. adapter is
// what the adapter adds, and is passed as given.
func buildEnv(s Spec, tmp string, adapter []string) ([]string, error) {
	src := s.Environ
	if src == nil {
		src = os.Environ()
	}
	have := map[string]string{}
	for _, kv := range src {
		if k, v, ok := strings.Cut(kv, "="); ok {
			have[k] = v
		}
	}
	var env []string
	for _, k := range baseEnv {
		if v, ok := have[k]; ok {
			env = append(env, k+"="+v)
		}
	}
	env = append(env, "TMPDIR="+tmp)
	if s.Metered {
		for _, k := range s.KeyEnv {
			if !keyNamePattern.MatchString(k) || hostSecret.MatchString(k) {
				return nil, fmt.Errorf("runner: %q is not allowed as a model key variable", k)
			}
			if v, ok := have[k]; ok && v != "" {
				env = append(env, k+"="+v)
			}
		}
	}
	for _, kv := range adapter {
		if strings.Contains(kv, "=") {
			env = append(env, kv)
		}
	}
	return env, nil
}
