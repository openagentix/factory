package staging

import (
	"context"

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
