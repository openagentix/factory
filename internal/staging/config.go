package staging

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/verify"
)

// DefaultMaxEntries is the default of Config.MaxEntries.
const DefaultMaxEntries = 50

// Config is everything a Staging needs. Every field that gates a decision
// must be set on purpose: a blank value is an error, never a default that
// lets a change in.
type Config struct {
	Repo *repo.Repo
	// Host is the propose side of the git host. Staging calls PushStaging,
	// OpenPullRequest and GetPullRequest on it, and nothing else.
	Host githost.Proposer
	// Accept is optional. Without it Accept opens or updates the staging pull
	// request for a developer and staging stays frozen until that pull
	// request is merged or closed.
	Accept githost.Accepter
	Store  Store

	// Factory and Tasks give the protected path rules. Tasks maps the task
	// name to its task file, and must hold every name in AllowedTasks.
	Factory *api.Factory
	Tasks   map[string]*api.Task
	// AllowedTasks lists the tasks whose changes may be added: implement,
	// fix-ci and acceptance, or fewer. An empty list admits nothing.
	AllowedTasks []string

	// Checks are the task checks run on every replayed step and on the
	// default commit alone. An empty list is an error unless NoChecks is
	// true, which is the one way to say that no check is intended.
	Checks   []api.Check
	NoChecks bool
	// AcceptanceSource returns, for a goal, the original commit of the
	// goal's passed acceptance change: the reviewed source of its checks. It
	// is read once per goal and rebuild. Nil is an error unless
	// NoAcceptance is true. An error or a blank answer stops the rebuild.
	AcceptanceSource func(ctx context.Context, goal int) (string, error)
	NoAcceptance     bool

	// MaxEntries is the most changes in the live and waiting lists together.
	// Zero means DefaultMaxEntries. A negative value is an error.
	MaxEntries int

	// DefaultBranch is the name of the default branch, the base of the
	// staging pull request.
	DefaultBranch string
	// Author is the factory's own author for the rebuilt commits.
	Author repo.Author

	CheckOptions      verify.CheckOptions
	AcceptanceOptions acceptance.Options
	// Now is the clock, only for the time an Accept started. Nil is
	// time.Now.
	Now func() time.Time
}

// Staging rebuilds, publishes and accepts the one staging branch of one
// factory repository on one machine. All its methods are safe to call from
// many goroutines: they run one at a time. It does not claim staging across
// machines.
//
// The login-token check for Accept is not here. It belongs to the web layer.
// The methods of Staging trust their caller to have asked the right person.
type Staging struct {
	cfg Config
	mu  sync.Mutex

	// Seams for tests in this package.
	runChecks     func(ctx context.Context, repoDir, commit string, checks []api.Check, opts verify.CheckOptions) ([]verify.CheckOutcome, error)
	runAcceptance func(ctx context.Context, r *repo.Repo, req acceptance.Request, opts acceptance.Options) (*acceptance.Report, error)
}

// New checks the configuration and returns a Staging.
func New(cfg Config) (*Staging, error) {
	bad := func(format string, a ...any) (*Staging, error) {
		return nil, fmt.Errorf("staging: bad configuration: "+format, a...)
	}
	switch {
	case cfg.Repo == nil:
		return bad("no repository")
	case cfg.Host == nil:
		return bad("no git host")
	case cfg.Store == nil:
		return bad("no store")
	case cfg.Factory == nil:
		return bad("no factory file")
	case strings.TrimSpace(cfg.DefaultBranch) == "":
		return bad("no default branch name")
	case strings.TrimSpace(cfg.Author.Name) == "" || strings.TrimSpace(cfg.Author.Email) == "":
		return bad("no author for the rebuilt commits")
	case len(cfg.Checks) == 0 && !cfg.NoChecks:
		return nil, ErrNoChecks
	case len(cfg.Checks) > 0 && cfg.NoChecks:
		return bad("checks are set and NoChecks is true")
	case cfg.AcceptanceSource == nil && !cfg.NoAcceptance:
		return bad("no acceptance source and NoAcceptance is not set")
	case cfg.AcceptanceSource != nil && cfg.NoAcceptance:
		return bad("an acceptance source is set and NoAcceptance is true")
	case cfg.MaxEntries < 0:
		return bad("MaxEntries must be at least 1")
	case len(cfg.AllowedTasks) == 0:
		return bad("no task is allowed, so nothing could be added")
	}
	for _, name := range cfg.AllowedTasks {
		if !allowedTaskNames[name] {
			return bad("task %q cannot reach staging", name)
		}
		if t := cfg.Tasks[name]; t == nil || t.Metadata.Name != name {
			return bad("no task file for %q", name)
		}
	}
	for i, c := range cfg.Checks {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Run) == "" {
			return bad("check %d has no name or no command", i)
		}
	}
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}
	s := &Staging{cfg: cfg, runChecks: verify.RunChecks, runAcceptance: acceptance.Run}
	if s.cfg.Now == nil {
		s.cfg.Now = time.Now
	}
	return s, nil
}

func (s *Staging) taskAllowed(name string) bool {
	for _, n := range s.cfg.AllowedTasks {
		if n == name {
			return true
		}
	}
	return false
}

// frozen reports whether an Accept is pending.
func (s *Staging) frozen(ctx context.Context) (bool, error) {
	_, ok, err := s.cfg.Store.GetPending(ctx)
	return ok, err
}

// infra reports whether an error from the repository must stop a rebuild
// and must not be blamed on one change: the context ended, or the repository
// no longer passes its guard. The package repo does not tell a guard failure
// from an ordinary failure by the error alone, so after any error this makes
// one cheap guarded call as a probe.
func (s *Staging) infra(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	_, perr := s.cfg.Repo.Resolve(ctx, repo.MirrorRef)
	return perr != nil
}
