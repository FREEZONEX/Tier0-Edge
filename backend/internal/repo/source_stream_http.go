package repo

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/streamexec"

	"gorm.io/gorm"
)

// WithHTTPSourcePort reserves a stable port inside the Source transaction.
// Disabled Sources keep their reservation; deleted Sources release it.
func (r *SourceStreamRepo) WithHTTPSourcePort(ctx context.Context, sourceID int64, requested, first, last int, action func(*gorm.DB, int) error) error {
	if first < 1024 || last < first || last > 65535 || last-first >= 256 || requested != 0 && (requested < first || requested > last) {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(846031000)).Error; err != nil {
			return err
		}
		var sources []Source
		if err := tx.Select("id, document").Where("protocol=? AND deleted_at=0 AND id<>?", "http-api", sourceID).Find(&sources).Error; err != nil {
			return err
		}
		used := map[int]bool{}
		for _, source := range sources {
			var doc struct {
				Role string `json:"role"`
				HTTP struct {
					Port int `json:"port"`
				} `json:"http"`
			}
			if json.Unmarshal([]byte(source.Document), &doc) != nil {
				return ErrInvalidArgument
			}
			if doc.Role == "server" {
				used[doc.HTTP.Port] = true
			}
		}
		if requested == 0 {
			for port := first; port <= last; port++ {
				if !used[port] {
					requested = port
					break
				}
			}
		}
		if requested == 0 || used[requested] {
			return ErrConflict
		}
		return action(tx, requested)
	})
}

// Route templates overlap if each segment can match the same request.
func HTTPRoutesOverlap(left, right streamexec.ProgramEntry) bool {
	if left.SourceID != right.SourceID || left.Method != right.Method {
		return false
	}
	a, b := strings.Split(left.Path, "/"), strings.Split(right.Path, "/")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] && !strings.HasPrefix(a[i], ":") && !strings.HasPrefix(b[i], ":") {
			return false
		}
	}
	return true
}

func (r *SourceStreamRepo) CheckHTTPRouteConflicts(ctx context.Context, streamID int64, manifest streamexec.ProgramManifest) error {
	routes := []streamexec.ProgramEntry{}
	for _, entry := range manifest.Entries {
		if entry.Kind == "http-request" {
			for _, previous := range routes {
				if HTTPRoutesOverlap(previous, entry) {
					return ErrConflict
				}
			}
			routes = append(routes, entry)
		}
	}
	if len(routes) == 0 {
		return nil
	}
	active, err := r.ActiveExecutions(ctx)
	if err != nil {
		return err
	}
	pending, err := r.PendingProgramV2Executions(ctx)
	if err != nil {
		return err
	}
	for _, item := range append(active, pending...) {
		if item.Stream.ID == streamID || item.Stream.Model != "program" {
			continue
		}
		other, err := item.Version.ProgramV2Manifest()
		if err != nil {
			return err
		}
		for _, candidate := range other.Entries {
			if candidate.Kind != "http-request" {
				continue
			}
			for _, route := range routes {
				if HTTPRoutesOverlap(candidate, route) {
					return ErrConflict
				}
			}
		}
	}
	return nil
}

// HTTPRouteGuard serializes route activation, not HTTP request execution.
func (r *SourceStreamRepo) HTTPRouteGuard(ctx context.Context, action func(*gorm.DB) error) error {
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(846031001)).Error; err != nil {
			return err
		}
		return action(tx)
	})
}
