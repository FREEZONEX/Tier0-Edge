package unsv2

import (
	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"sort"
	"time"
)

const (
	RecordSubscribeMaxCollections = 64
	RecordSubscribeMaxRecordIDs   = 100
	RecordSubscribeMaxEvents      = 100
	RecordSubscribeMaxWaitMS      = 15000
	RecordSubscribeRetentionMS    = int64(24 * time.Hour / time.Millisecond)
	recordSubscribeFallback       = 5 * time.Second
	recordSubscribeMaxWaiters     = 500
)

type RecordSubscribeRequest struct {
	CollectionIDs []string `json:"collectionIds,omitempty"`
	RecordIDs     []string `json:"recordIds,omitempty"`
	Cursor        string   `json:"cursor,omitempty"`
	WaitMS        int      `json:"waitMs,omitempty"`
	Limit         int      `json:"limit,omitempty"`
}
type RecordChangeEvent struct {
	EventID      string `json:"eventId"`
	CollectionID string `json:"collectionId"`
	RecordID     string `json:"recordId"`
	Revision     int64  `json:"revision"`
	Change       string `json:"change"`
	OccurredAt   int64  `json:"occurredAt"`
	Restricted   bool   `json:"restricted"`
}
type RecordSubscribeReset struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
type RecordSubscribeResponse struct {
	Events []RecordChangeEvent   `json:"events"`
	Cursor *string               `json:"cursor"`
	Reset  *RecordSubscribeReset `json:"reset"`
}
type subscribeScope struct {
	collections, records []string
	binding              string
}

func normalizeRecordSubscribe(r *RecordSubscribeRequest) error {
	if len(r.CollectionIDs) > RecordSubscribeMaxCollections || len(r.RecordIDs) > RecordSubscribeMaxRecordIDs || r.WaitMS < 0 || r.WaitMS > RecordSubscribeMaxWaitMS || r.Limit < 0 || r.Limit > RecordSubscribeMaxEvents || len(r.Cursor) > recordChangesMaxCursor {
		return invalid("request", "invalid record subscription bounds")
	}
	if len(r.RecordIDs) > 0 && len(r.CollectionIDs) != 1 {
		return invalid("recordIds", "recordIds require exactly one collectionId")
	}
	if r.Limit == 0 {
		r.Limit = RecordSubscribeMaxEvents
	}
	for path, list := range map[string]*[]string{"collectionIds": &r.CollectionIDs, "recordIds": &r.RecordIDs} {
		seen := map[string]bool{}
		for _, id := range *list {
			if !unsmeta.ValidID(id) || seen[id] {
				return invalid(path, "unique valid identities required")
			}
			seen[id] = true
		}
		*list = append([]string(nil), (*list)...)
		sort.Strings(*list)
	}
	return nil
}

// Subscription admission uses the same platform/key/App intersection as record reads.
func (s *Service) recordSubscribeScope(ctx context.Context, r RecordSubscribeRequest) (subscribeScope, error) {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return subscribeScope{}, problem("UNAUTHENTICATED", "current identity required")
	}
	if err := require(ctx, "uns.read"); err != nil {
		return subscribeScope{}, err
	}
	scope := subscribeScope{collections: append([]string(nil), r.CollectionIDs...), records: r.RecordIDs}
	if appIdentity(ctx) != "" && len(scope.collections) == 0 {
		_, app, err := appDefinition(ctx, appIdentity(ctx))
		if err != nil {
			return scope, err
		}
		for _, grant := range app.Collections {
			if contains(grant.Permissions, "record:read") && recordReadPermission(ctx, grant.CollectionID) == nil {
				scope.collections = append(scope.collections, grant.CollectionID)
			}
		}
		sort.Strings(scope.collections)
		if len(scope.collections) == 0 {
			return scope, problem("FORBIDDEN", "no readable App collections")
		}
	}
	for _, id := range scope.collections {
		if err := recordReadPermission(ctx, id); err != nil {
			return scope, err
		}
		if err := repo.RequireLocalCollectionRead(ctx, id); err != nil {
			return scope, classify(err)
		}
	}
	if len(scope.collections) == 0 {
		if err := recordReadPermission(ctx, ""); err != nil {
			return scope, err
		}
		if actor.APIKeyUsageType == repo.APIKeyUsageAppData {
			return scope, problem("FORBIDDEN", "App identity is required")
		}
		policy, err := repo.GlobalStorage(ctx)
		if err != nil {
			return scope, err
		}
		if policy.Mode == "center" {
			return scope, problem("UNSUPPORTED_OPERATION", "all-Collection subscriptions cannot combine remote center logs; select local Collections")
		}
	}
	raw, _ := json.Marshal(struct {
		User, Key            int64
		App, Auth            string
		Collections, Records []string
	}{actor.UserID, actor.APIKeyID, appIdentity(ctx), actor.AuthType, scope.collections, scope.records})
	hash := sha256.Sum256(raw)
	scope.binding = hex.EncodeToString(hash[:])
	return scope, nil
}

func subscribeReset(reason, message string) RecordSubscribeResponse {
	return RecordSubscribeResponse{Events: []RecordChangeEvent{}, Reset: &RecordSubscribeReset{Reason: reason, Message: message}}
}
func scopeLost(err error) bool {
	var f *Fault
	return errors.As(err, &f) && (f.Code == "FORBIDDEN" || f.Code == "NOT_FOUND" || f.Code == "UNAUTHENTICATED" || f.Code == "UNSUPPORTED_OPERATION")
}

// RecordSubscribe reads only finished transactions. Waiting holds no SQL connection or data admission slot.
func (s *Service) RecordSubscribe(ctx context.Context, r RecordSubscribeRequest) (RecordSubscribeResponse, error) {
	out := RecordSubscribeResponse{Events: []RecordChangeEvent{}}
	if err := normalizeRecordSubscribe(&r); err != nil {
		return out, err
	}
	if s.refresh == nil {
		return out, problem("TEMPORARILY_UNAVAILABLE", "current subscription authorization is unavailable")
	}
	if _, err := s.recordChangesAEAD(); err != nil {
		return out, err
	}
	var claims recordChangesCursor
	if r.Cursor != "" {
		var stale bool
		var err error
		claims, stale, err = s.openRecordChangesCursor(r.Cursor)
		if err != nil {
			return out, err
		}
		if stale {
			return subscribeReset("CURSOR_EXPIRED", "read current records and subscribe again"), nil
		}
	}
	release, ok := s.admitRecordWaiter()
	if !ok {
		return out, &Fault{Code: "TEMPORARILY_UNAVAILABLE", Reason: "SUBSCRIPTION_LIMIT", Message: "too many waiting record subscriptions", Retryable: true}
	}
	defer release()
	deadline := time.Now().Add(time.Duration(r.WaitMS) * time.Millisecond)
	if server, ok := ctx.Deadline(); ok && server.Add(-3*time.Second).Before(deadline) {
		deadline = server.Add(-3 * time.Second)
	}
	position := repo.RecordChangePosition{XID: claims.XID, Seq: claims.Seq}
	for {
		if err := ctx.Err(); err != nil {
			return out, pipelineRuntimeError(err)
		}
		actor, ok := contextx.SubjectFrom(ctx)
		if !ok {
			return out, problem("UNAUTHENTICATED", "current identity required")
		}
		current, err := s.refresh(ctx, actor)
		if err != nil {
			if r.Cursor != "" {
				return subscribeReset("SCOPE_CHANGED", "subscription authorization changed; read current records and subscribe again"), nil
			}
			return out, problem("FORBIDDEN", "current subscription authorization required")
		}
		currentCtx := contextx.WithSubject(ctx, current)
		scope, err := s.recordSubscribeScope(currentCtx, r)
		if err != nil {
			if r.Cursor != "" && scopeLost(err) {
				return subscribeReset("SCOPE_CHANGED", "subscription scope changed; read current records and subscribe again"), nil
			}
			return out, err
		}
		if r.Cursor != "" && claims.Binding != scope.binding {
			return subscribeReset("SCOPE_CHANGED", "subscription scope changed; read current records and subscribe again"), nil
		}
		signal := s.recordChangeSignal()
		var window repo.RecordChangesWindow
		var rows []repo.RecordChange
		var next repo.RecordChangePosition
		var more bool
		err = s.readTx(currentCtx, func(qctx context.Context, tx *gorm.DB) error {
			var err error
			window, err = repo.ReadRecordChangesWindow(qctx, tx)
			if err != nil {
				return err
			}
			if r.Cursor == "" || window.Database != claims.Database || position.Less(window.Purged) {
				return nil
			}
			rows, next, more, err = repo.ReadRecordChanges(qctx, tx, window, position, scope.collections, scope.records, r.Limit)
			return err
		})
		if err != nil {
			return out, pipelineRuntimeError(err)
		}
		if r.Cursor == "" {
			return s.subscribeResponse(out, scope, window.Head, window.Database)
		}
		if window.Database != claims.Database || position.Less(window.Purged) {
			return subscribeReset("CURSOR_EXPIRED", "cursor is outside the retained database history; read current records and subscribe again"), nil
		}
		for _, row := range rows {
			eventID, err := s.sealRecordChangesCursor(recordChangesCursor{Binding: scope.binding, XID: row.XID, Seq: row.Seq, Database: window.Database})
			if err != nil {
				return out, err
			}
			out.Events = append(out.Events, RecordChangeEvent{EventID: eventID, CollectionID: row.CollectionID, RecordID: row.RecordID, Revision: row.Revision, Change: row.Change, OccurredAt: row.CreatedAt})
		}
		position = next
		if len(out.Events) > 0 || !time.Now().Before(deadline) {
			return s.subscribeResponse(out, scope, position, window.Database)
		}
		if more {
			continue
		}
		timer := time.NewTimer(min(time.Until(deadline), recordSubscribeFallback))
		select {
		case <-ctx.Done():
			timer.Stop()
			return out, pipelineRuntimeError(ctx.Err())
		case <-signal:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (s *Service) subscribeResponse(out RecordSubscribeResponse, scope subscribeScope, position repo.RecordChangePosition, database string) (RecordSubscribeResponse, error) {
	token, err := s.sealRecordChangesCursor(recordChangesCursor{Binding: scope.binding, XID: position.XID, Seq: position.Seq, Database: database})
	if err != nil {
		return out, err
	}
	out.Cursor = &token
	return out, nil
}
func (s *Service) recordChangeSignal() <-chan struct{} {
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	if s.changeSignal == nil {
		s.changeSignal = make(chan struct{})
	}
	return s.changeSignal
}
func (s *Service) NotifyRecordChanges() {
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	if s.changeSignal != nil {
		close(s.changeSignal)
		s.changeSignal = nil
	}
}
func (s *Service) admitRecordWaiter() (func(), bool) {
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	if s.recordWaiters >= recordSubscribeMaxWaiters {
		return nil, false
	}
	s.recordWaiters++
	return func() { s.changeMu.Lock(); s.recordWaiters--; s.changeMu.Unlock() }, true
}
func (s *Service) purgeRecordChanges(ctx context.Context) error {
	_, err := repo.PurgeRecordChanges(ctx, repo.GetUnsConn(ctx), time.Now().Add(-24*time.Hour).UnixMilli(), 5000)
	return err
}
