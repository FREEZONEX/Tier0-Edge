package sourcestream

import (
	"context"

	"backend/internal/repo"
	"gorm.io/gorm"
)

// State changes preserve the connection revision and all historical data.
// The same dependency proof used by configuration writes fences the transition.
func (s *Service) SetSourceState(ctx context.Context, id, revision int64, desired, confirmed string) (map[string]any, error) {
	if desired != "enabled" && desired != "disabled" {
		return nil, invalid("desiredState", "invalid")
	}
	action := "enable"
	if desired == "disabled" {
		action = "disable"
	}
	ctx, err := s.BindSourceImpact(ctx, id, revision, action, nil)
	if err != nil {
		return nil, err
	}
	item, err := repo.NewSourceStreamRepo(ctx).ChangeSource(ctx, id, revision, func(tx *gorm.DB, item *repo.Source) error {
		if err := checkImpact(ctx, tx, id, confirmed); err != nil {
			return err
		}
		item.DesiredState = desired
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.refreshSource(ctx, id)
	return sourceView(item), nil
}
