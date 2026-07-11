package web

import "context"

// mutationService is the web layer's single boundary for filesystem changes
// that must be persisted to git. Keeping the mutation callback inside the
// Syncer's lock prevents a background pull, an HTTP request, or a scheduled
// task from observing or committing a half-finished change.
//
// The Syncer is deliberately supplied by the caller rather than stored here:
// it is created only once the board exists (after an optional clone), while
// handlers and no-sync tests can use the same zero-value service.
type mutationService struct{}

func (mutationService) run(ctx context.Context, sync *Syncer, message string, mutate func() error) error {
	return sync.MutateAndCommit(ctx, message, mutate)
}
