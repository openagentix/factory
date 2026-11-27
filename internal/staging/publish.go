package staging

import (
	"context"
	"errors"

	"github.com/openagentix/factory/internal/repo"
)

// publish is the one place that writes staging. It moves the local staging
// ref, with a compare and swap against old, and force pushes the commit to
// the host. Then it marks the state published. Nothing else in the module
// calls PushStaging or sets the staging ref, and a test keeps it that way.
func (s *Staging) publish(ctx context.Context, st State, old string) error {
	if err := s.cfg.Repo.SetRef(ctx, repo.StagingRef, st.Commit, old); err != nil {
		return err
	}
	if err := s.cfg.Host.PushStaging(ctx, s.cfg.Repo.Dir(), st.Commit); err != nil {
		return err
	}
	return s.cfg.Store.MarkPublished(ctx, st.Seq)
}

// readStagingRef reads the staging ref of the factory repository. A ref that
// does not exist is ZeroID. A repository that is not trusted is an error.
func (s *Staging) readStagingRef(ctx context.Context) (string, error) {
	c, err := s.cfg.Repo.Resolve(ctx, repo.StagingRef)
	if err != nil {
		if s.infra(ctx, err) {
			return "", err
		}
		return repo.ZeroID, nil
	}
	if !commitRE.MatchString(c) {
		return "", errors.New("staging: the staging ref does not name a commit by its full id")
	}
	return c, nil
}

// stagingConflict is the error for a staging ref that is not what a rebuild
// read at its start.
func stagingConflict(expected, actual string) error {
	return &repo.RefConflictError{Ref: repo.StagingRef, Expected: expected, Actual: actual}
}
