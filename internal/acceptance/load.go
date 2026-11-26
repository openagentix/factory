package acceptance

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
)

// Limits of a manifest. A larger one is refused with a clear error.
const (
	// MaxManifestBytes is the size of the manifest file.
	MaxManifestBytes = 1 << 20
	// MaxCriteria is the number of criteria of one goal.
	MaxCriteria = 500
	// MaxTextBytes is the length of one criterion's wording.
	MaxTextBytes = 2000
	// MaxRunBytes is the length of one check command.
	MaxRunBytes = 4096
)

// ManifestName is the file name of the manifest in a goal's directory.
const ManifestName = "manifest.json"

// ManifestPath is the path of a goal's manifest, relative to the repository
// root. An empty acceptancePath means api.DefaultAcceptancePath.
func ManifestPath(acceptancePath string, goal int) string {
	return GoalDir(acceptancePath, goal) + "/" + ManifestName
}

// GoalDir is the directory of a goal's manifest and check files.
func GoalDir(acceptancePath string, goal int) string {
	if acceptancePath == "" {
		acceptancePath = api.DefaultAcceptancePath
	}
	return acceptancePath + "/" + strconv.Itoa(goal)
}

// plainName reports whether s is made only of ASCII letters, digits, "-", "_"
// and ".". This is a rule, not a normaliser: a file system may unify two
// Unicode spellings of one name, and ASCII has only one spelling.
func plainName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// checkAcceptancePath refuses a path that is not plain ASCII (letters,
// digits, "-", "_", "." and "/" between parts) or has a ".", ".." or ".git"
// part. An empty path means the default.
func checkAcceptancePath(p string) error {
	if p == "" {
		return nil
	}
	bad := func() error {
		return fmt.Errorf("the acceptance path %q must be plain ASCII: letters, digits, - _ . and / between parts, with no . or .. part and no .git part", repo.Visible(p, 100))
	}
	if len(p) > 200 {
		return bad()
	}
	for _, part := range strings.Split(p, "/") {
		if !plainName(part) || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return bad()
		}
	}
	return nil
}

// LoadManifest reads the manifest of a goal from a commit of the factory's
// own repository. The commit is the acceptance source. The file is read from
// git objects, with a size cap, and must be a regular file. The manifest must
// name the same goal, and must stay inside the limits.
func LoadManifest(ctx context.Context, r *repo.Repo, source, acceptancePath string, goal int) (*api.Manifest, error) {
	if goal < 1 {
		return nil, fmt.Errorf("acceptance: the goal number must be 1 or more")
	}
	if err := checkAcceptancePath(acceptancePath); err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	data, err := r.ReadFile(ctx, source, ManifestPath(acceptancePath, goal), MaxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	m, err := api.ParseManifest(data)
	if err != nil {
		return nil, err
	}
	if m.Goal != goal {
		return nil, fmt.Errorf("acceptance manifest: the file for goal %d names goal %d", goal, m.Goal)
	}
	if err := checkManifestLimits(m); err != nil {
		return nil, err
	}
	return m, nil
}

func checkManifestLimits(m *api.Manifest) error {
	if len(m.Criteria) > MaxCriteria {
		return fmt.Errorf("acceptance manifest: %d criteria, more than the limit of %d", len(m.Criteria), MaxCriteria)
	}
	for _, c := range m.Criteria {
		if len(c.Text) > MaxTextBytes {
			return fmt.Errorf("acceptance manifest: the wording of criterion %d is longer than %d bytes", c.ID, MaxTextBytes)
		}
		if len(c.Run) > MaxRunBytes {
			return fmt.Errorf("acceptance manifest: the command of criterion %d is longer than %d bytes", c.ID, MaxRunBytes)
		}
	}
	return nil
}
