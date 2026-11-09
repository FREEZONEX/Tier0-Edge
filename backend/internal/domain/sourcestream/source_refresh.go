package sourcestream

import "context"

type sourceRefreshKey struct{}

// DeferSourceRefresh lets an atomic document batch invalidate connections only
// after its outer transaction commits. A preview or rolled-back batch discards it.
func (s *Service) DeferSourceRefresh(ctx context.Context) (context.Context, func()) {
	if _, deferred := ctx.Value(sourceRefreshKey{}).(map[int64]bool); deferred {
		return ctx, func() {}
	}
	ids := map[int64]bool{}
	return context.WithValue(ctx, sourceRefreshKey{}, ids), func() {
		for id := range ids {
			s.releaseSource(id)
		}
	}
}
func (s *Service) refreshSource(ctx context.Context, id int64) {
	if ids, ok := ctx.Value(sourceRefreshKey{}).(map[int64]bool); ok {
		ids[id] = true
		return
	}
	s.releaseSource(id)
}
