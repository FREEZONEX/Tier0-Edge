package unsv2

import (
	"backend/internal/contextx"
	"backend/internal/repo"
	"context"
	"encoding/json"
	"errors"
	assert "github.com/stretchr/testify/require"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func subscribeFixture(t *testing.T) (*repo.Store, *Service, context.Context) {
	store, s, ctx := batchReadFixture(t)
	s.SetRecordChangesCursorSecret("isolated-test-cursor-secret")
	assert.NoError(t, repo.ConfigureRecordChangeNotifier(store.UnsDB(), s.NotifyRecordChanges))
	return store, s, ctx
}
func subscribeHead(t *testing.T, s *Service, ctx context.Context, collections ...string) string {
	t.Helper()
	out, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: collections})
	assert.NoError(t, err)
	assert.Empty(t, out.Events)
	assert.Nil(t, out.Reset)
	assert.NotNil(t, out.Cursor)
	return *out.Cursor
}

func TestRecordSubscribeCommitWatermarkRollbackAndResume(t *testing.T) {
	store, s, ctx := subscribeFixture(t)
	cursor := subscribeHead(t, s, ctx, "data")
	slow := store.UnsDB().Begin()
	assert.NoError(t, slow.Error)
	t.Cleanup(func() { slow.Rollback() })
	assert.NoError(t, slow.Exec(`INSERT INTO public.uns_record(collection_id,id,data,revision,created_at,updated_at) VALUES('data','slow','{"value":3}',1,1,1)`).Error)
	_, err := s.metadata.SaveRecord(ctx, "data", "fast", 0, map[string]any{"value": 4})
	assert.NoError(t, err)
	blocked, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: cursor})
	assert.NoError(t, err)
	assert.Empty(t, blocked.Events, "do not advance beyond an uncommitted transaction")
	assert.NoError(t, slow.Commit().Error)
	out, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: *blocked.Cursor, Limit: 1})
	assert.NoError(t, err)
	assert.Len(t, out.Events, 1)
	assert.Equal(t, "slow", out.Events[0].RecordID)
	resumed, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: out.Events[0].EventID})
	assert.NoError(t, err)
	assert.Len(t, resumed.Events, 1)
	assert.Equal(t, "fast", resumed.Events[0].RecordID)
	// The transaction trigger retains no change when a write rolls back.
	tx := store.UnsDB().Begin()
	assert.NoError(t, tx.Exec(`UPDATE public.uns_record SET data='{"value":99}',revision=revision+1 WHERE collection_id='data' AND id='fast'`).Error)
	assert.NoError(t, tx.Rollback().Error)
	empty, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: *resumed.Cursor})
	assert.NoError(t, err)
	assert.Empty(t, empty.Events)
	assert.NoError(t, store.UnsDB().Exec(`UPDATE public.uns_record SET deleted_at=now(),revision=revision+1 WHERE collection_id='data' AND id='fast'`).Error)
	deleted, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: *empty.Cursor})
	assert.NoError(t, err)
	assert.Len(t, deleted.Events, 1)
	assert.Equal(t, "delete", deleted.Events[0].Change)
	assert.EqualValues(t, 2, deleted.Events[0].Revision)
	encoded, err := json.Marshal(deleted)
	assert.NoError(t, err)
	assert.NotContains(t, string(encoded), "value")
}

func TestRecordSubscribeCursorScopeRetentionAndDatabase(t *testing.T) {
	store, s, ctx := subscribeFixture(t)
	cursor := subscribeHead(t, s, ctx, "data")
	changed, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"other"}, Cursor: cursor})
	assert.NoError(t, err)
	assert.Equal(t, "SCOPE_CHANGED", changed.Reset.Reason)
	tampered := []byte(cursor)
	if tampered[20] == 'A' {
		tampered[20] = 'B'
	} else {
		tampered[20] = 'A'
	}
	changed, err = s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: string(tampered)})
	assert.NoError(t, err)
	assert.Equal(t, "CURSOR_EXPIRED", changed.Reset.Reason)
	claims, _, err := s.openRecordChangesCursor(cursor)
	assert.NoError(t, err)
	claims.Database = "different-database"
	different, err := s.sealRecordChangesCursor(claims)
	assert.NoError(t, err)
	changed, err = s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: different})
	assert.NoError(t, err)
	assert.Equal(t, "CURSOR_EXPIRED", changed.Reset.Reason)
	_, err = s.metadata.SaveRecord(ctx, "data", "purged-event", 0, map[string]any{"value": 5})
	assert.NoError(t, err)
	assert.NoError(t, store.UnsDB().Exec("UPDATE public.uns_record_change SET created_at=?", time.Now().Add(-25*time.Hour).UnixMilli()).Error)
	assert.NoError(t, s.purgeRecordChanges(ctx))
	changed, err = s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: cursor})
	assert.NoError(t, err)
	assert.Equal(t, "CURSOR_EXPIRED", changed.Reset.Reason)
	for _, request := range []RecordSubscribeRequest{{WaitMS: 15001}, {Limit: 101}, {RecordIDs: []string{"first"}}, {CollectionIDs: []string{"data", "data"}}, {Cursor: "invalid"}} {
		_, err = s.RecordSubscribe(ctx, request)
		assert.Error(t, err)
	}
}

func TestRecordSubscribeWakeRevocationAndCancellation(t *testing.T) {
	_, s, ctx := subscribeFixture(t)
	cursor := subscribeHead(t, s, ctx, "data")
	type response struct {
		out RecordSubscribeResponse
		err error
	}
	done := make(chan response, 1)
	go func() {
		out, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: cursor, WaitMS: 10000})
		done <- response{out, err}
	}()
	assert.Eventually(t, func() bool { s.changeMu.Lock(); defer s.changeMu.Unlock(); return s.recordWaiters == 1 }, time.Second, 5*time.Millisecond)
	_, err := s.metadata.SaveRecord(ctx, "data", "wake", 0, map[string]any{"value": 7})
	assert.NoError(t, err)
	select {
	case result := <-done:
		assert.NoError(t, result.err)
		assert.Len(t, result.out.Events, 1)
		cursor = *result.out.Cursor
	case <-time.After(2 * time.Second):
		t.Fatal("after-commit wake did not deliver")
	}
	var revoked atomic.Bool
	s.refresh = func(_ context.Context, actor contextx.Subject) (contextx.Subject, error) {
		if revoked.Load() {
			return actor, errors.New("revoked")
		}
		return actor, nil
	}
	go func() {
		out, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: cursor, WaitMS: 10000})
		done <- response{out, err}
	}()
	assert.Eventually(t, func() bool { s.changeMu.Lock(); defer s.changeMu.Unlock(); return s.recordWaiters == 1 }, time.Second, 5*time.Millisecond)
	revoked.Store(true)
	s.NotifyRecordChanges()
	select {
	case result := <-done:
		assert.NoError(t, result.err)
		assert.Equal(t, "SCOPE_CHANGED", result.out.Reset.Reason)
		assert.Empty(t, result.out.Events)
	case <-time.After(2 * time.Second):
		t.Fatal("authorization was not refreshed")
	}
	revoked.Store(false)
	cancelCtx, cancel := context.WithCancel(ctx)
	go func() {
		out, err := s.RecordSubscribe(cancelCtx, RecordSubscribeRequest{CollectionIDs: []string{"data"}, Cursor: cursor, WaitMS: 10000})
		done <- response{out, err}
	}()
	assert.Eventually(t, func() bool { s.changeMu.Lock(); defer s.changeMu.Unlock(); return s.recordWaiters == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	select {
	case result := <-done:
		assert.Error(t, result.err)
	case <-time.After(time.Second):
		t.Fatal("canceled waiter remained active")
	}
	s.changeMu.Lock()
	assert.Zero(t, s.recordWaiters)
	s.changeMu.Unlock()
}

func TestRecordSubscribeAppGrantIntersection(t *testing.T) {
	store, s, admin := subscribeFixture(t)
	app := AppData{Header: Header{SchemaVersion: 1, Name: "App"}, Collections: []Grant{{Alias: "data", CollectionID: "data", Permissions: []string{"record:read"}}}}
	raw, _ := json.Marshal(app)
	assert.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: "app", Revision: 1, Data: raw}).Error)
	actor, _ := contextx.SubjectFrom(admin)
	actor.APIKeyAppID = "app"
	actor.APIKeyUsageType = repo.APIKeyUsageAppData
	ctx := contextx.WithSubject(context.Background(), actor)
	cursor := subscribeHead(t, s, ctx)
	denied, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{CollectionIDs: []string{"private"}})
	assert.Error(t, err)
	assert.Empty(t, denied.Events)
	_, err = s.metadata.SaveRecord(admin, "data", "visible", 0, map[string]any{"value": 8})
	assert.NoError(t, err)
	out, err := s.RecordSubscribe(ctx, RecordSubscribeRequest{Cursor: cursor})
	assert.NoError(t, err)
	assert.Len(t, out.Events, 1)
	assert.NoError(t, store.UnsDB().Exec(`UPDATE public.uns_meta SET data=jsonb_set(data,'{collections}','[]') WHERE kind='app' AND id='app'`).Error)
	out, err = s.RecordSubscribe(ctx, RecordSubscribeRequest{Cursor: *out.Cursor})
	assert.NoError(t, err)
	assert.Equal(t, "SCOPE_CHANGED", out.Reset.Reason)
	assert.NotContains(t, strings.Join([]string{out.Reset.Message}, ""), "visible")
}

func TestRecordSubscribeRequiresCurrentAuthorization(t *testing.T) {
	_, s, ctx := subscribeFixture(t)
	s.refresh = nil
	var out RecordSubscribeResponse
	var err error
	assert.NotPanics(t, func() { out, err = s.RecordSubscribe(ctx, RecordSubscribeRequest{}) })
	var fault *Fault
	assert.ErrorAs(t, err, &fault)
	assert.Equal(t, "TEMPORARILY_UNAVAILABLE", fault.Code)
	assert.Empty(t, out.Events)
	assert.Nil(t, out.Cursor)
}
