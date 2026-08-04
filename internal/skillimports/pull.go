package skillimports

import "context"

// Pull fetches every configured source, reconciles it with local content, and
// projects the successful results. It is the only command that advances tracked
// imports; pinned imports stay at their locked commits. It never commits or
// pushes upstream.
func (s *Service) Pull(ctx context.Context) error {
	state, err := s.loadState()
	if err != nil {
		return err
	}
	report := &Report{}
	if len(state.config.Skills.Imports) == 0 {
		report.Note("no skill imports are configured; add one with 'al skills add <repository> <selector>'")
		return s.finish(report)
	}

	var plan *reconcilePlan
	err = s.withWorkspace(ctx, "pull", func(space *workspace) error {
		resolved, reconcileErr := s.reconcile(ctx, space, state, reconcileOptions{AdvanceTracked: true}, report)
		if reconcileErr != nil {
			return reconcileErr
		}
		plan = resolved
		if !plan.changed(state.lock.Entries) {
			return nil
		}
		return s.applyPlan(state, plan, "")
	})
	if err != nil {
		report.Note("no local state was changed")
		if s.Out != nil {
			report.Write(s.Out)
		}
		return err
	}

	// Successful results are projected even when other skills failed, so a
	// partial pull still reaches the clients. A pull where every source failed
	// reconciled nothing, so there is no result to project and a projection
	// failure there would only obscure the real error.
	if report.Succeeded() {
		s.runProjection(report)
	}
	return s.finish(report)
}
