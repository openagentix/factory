package runner

import (
	"fmt"
	"os"
	"strings"
)

// baseEnv are the only variables copied from the factory's environment.
var baseEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TERM", "SHELL"}

// modelKeyNames are the only variables that may carry a model key to a
// metered run: the key, the bearer token and the base address names that
// Claude Code and Codex document. Any other name is refused, because a
// variable such as LD_PRELOAD or BASH_ENV would run code in the agent.
var modelKeyNames = map[string]bool{
	"ANTHROPIC_API_KEY":    true,
	"ANTHROPIC_AUTH_TOKEN": true,
	"ANTHROPIC_BASE_URL":   true,
	"OPENAI_API_KEY":       true,
	"OPENAI_BASE_URL":      true,
	"CODEX_API_KEY":        true,
}

// checkKeyNames refuses a key variable that is not on the list.
func checkKeyNames(s Spec) error {
	if !s.Metered {
		return nil
	}
	for _, k := range s.KeyEnv {
		if !modelKeyNames[k] {
			return fmt.Errorf("runner: %q is not a model key variable the runner passes", k)
		}
	}
	return nil
}

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
			if !modelKeyNames[k] {
				return nil, fmt.Errorf("runner: %q is not a model key variable the runner passes", k)
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
