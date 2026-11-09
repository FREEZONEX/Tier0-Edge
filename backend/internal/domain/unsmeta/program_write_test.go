package unsmeta_test

// PostgreSQL 验证完整脚本的三表输出、游标及事件原子提交；不调用外部设备。
import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

var metadataNotifications sync.Map // test-only post-commit transport, keyed by Store

func programFixture(t *testing.T) (*repo.Store, string, string) {
	store, _ := testkit.NewTestDB(t)
	counter := &atomic.Int64{}
	metadataNotifications.Store(store, counter)
	t.Cleanup(func() { metadataNotifications.Delete(store) })
	require.NoError(t, repo.ConfigureResourceEventPublisher(store.UnsDB(), func(context.Context, string, string, []byte) error { counter.Add(1); return nil }))
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	result, err := unsmeta.New().CreateCollection(context.Background(), "", unsmeta.Document{Name: "Program Data", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	collection := result["collection"].(repo.UnsMeta)
	topic, err := unsmeta.New().Save(context.Background(), "", "topic", 0, unsmeta.Document{Name: "Program Data view", ViewMode: "records", CollectionID: collection.ID})
	require.NoError(t, err)
	return store, topic.ID, collection.ID
}

// 预览与真实提交必须一致：部分更新、null/删除、重复 ID 的批次顺序、
// schema/revision/身份冲突及整批回滚；多个 Stream 不能丢失彼此的不同字段。
func TestProgramWriteModesAndPreview(t *testing.T) {
	store, _, collectionID := programFixture(t)
	ctx := context.Background()
	stamp := time.Now().UnixMilli()
	write := func(mode, id string, data map[string]any) unsmeta.ProgramWrite {
		return unsmeta.ProgramWrite{CollectionID: collectionID, Value: unsmeta.IngestValue{RecordID: id, Record: data}, ProgramWriteOptions: unsmeta.ProgramWriteOptions{Mode: mode}}
	}
	commit := func(id string, writes ...unsmeta.ProgramWrite) (bool, error) {
		return unsmeta.CommitProgram(ctx, unsmeta.ProgramCommit{RunID: id, StreamID: 10, VersionID: 1, EntryID: "run", ReceivedAt: stamp, Writes: writes})
	}
	_, err := commit("create", write("create", "customer", map[string]any{"value": 1, "name": "Alpha", "owner": "Sales"}))
	require.NoError(t, err)
	patch := write("upsert", "customer", map[string]any{"name": "Beta", "nullable": nil})
	previews, err := unsmeta.PreviewProgramWrites(ctx, []unsmeta.ProgramWrite{patch, patch, write("create", "customer", map[string]any{"value": 2}), write("update", "absent", map[string]any{"name": "No"})}, stamp)
	require.NoError(t, err)
	require.Equal(t, "updated", previews[0].Status)
	require.EqualValues(t, 1, previews[0].Revision)
	require.Len(t, previews[0].Changes, 2)
	require.Equal(t, "nullable", previews[0].Changes[1].Field)
	require.False(t, previews[0].Changes[1].BeforeExists)
	require.True(t, previews[0].Changes[1].AfterExists)
	require.Nil(t, previews[0].Changes[1].After)
	require.Equal(t, "unchanged", previews[1].Status)
	require.EqualValues(t, 2, previews[1].Revision)
	require.Equal(t, "conflict", previews[2].Error)
	require.Equal(t, "notFound", previews[3].Error)
	row, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "customer")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Revision, "preview does not persist")
	require.JSONEq(t, `{"value":1,"name":"Alpha","owner":"Sales"}`, string(row.Data))
	_, err = commit("patch", patch)
	require.NoError(t, err)
	_, err = commit("patch-again", patch)
	require.NoError(t, err)
	row, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "customer")
	require.NoError(t, err)
	require.EqualValues(t, 2, row.Revision)
	require.JSONEq(t, `{"value":1,"name":"Beta","owner":"Sales","nullable":null}`, string(row.Data))
	unset := write("update", "customer", map[string]any{})
	unset.Unset = []string{"nullable"}
	_, err = commit("unset", unset)
	require.NoError(t, err)
	unset.Unset = []string{"value"}
	_, err = commit("remove-required", unset)
	require.ErrorIs(t, err, unsmeta.ErrData)
	badRevision := int64(1)
	patch.ExpectedRevision = &badRevision
	_, err = commit("stale", patch)
	require.ErrorIs(t, err, repo.ErrConflict)
	patch.ExpectedRevision = nil
	_, err = commit("rollback", write("create", "new", map[string]any{"value": 5}), write("update", "absent", map[string]any{"value": 2}))
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "new")
	require.ErrorIs(t, err, repo.ErrNotFound, "an earlier output must roll back")
	_, err = commit("replace", write("replace", "customer", map[string]any{"value": 9}))
	require.NoError(t, err)
	row, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "customer")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":9}`, string(row.Data))
	_, err = commit("invalid-mode", write("typo", "customer", map[string]any{"value": 10}))
	require.ErrorIs(t, err, unsmeta.ErrData)
	conflict := write("update", "customer", map[string]any{"value": 10})
	conflict.Unset = []string{"value"}
	_, err = commit("set-and-unset", conflict)
	require.ErrorIs(t, err, unsmeta.ErrData)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Count(&count).Error)
	require.EqualValues(t, 1, count)

	// Concurrent commits share the real row/advisory locks, not a read-merge in an App.
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, key := range []string{"crm", "billing"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, err := commit("concurrent-"+key, write("update", "customer", map[string]any{key: true}))
			errors <- err
		}(key)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	row, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "customer")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":9,"crm":true,"billing":true}`, string(row.Data))
}

func metadataEvents(t *testing.T, store *repo.Store) int64 {
	t.Helper()
	var count int64
	require.NoError(t, store.UnsDB().Table("public.uns_metadata_event_outbox").Count(&count).Error)
	require.Zero(t, count, "no persistent event Outbox")
	value, ok := metadataNotifications.Load(store)
	require.True(t, ok)
	return value.(*atomic.Int64).Load()
}

func TestProgramCanonicalBatchAtomicCommit(t *testing.T) {
	store, topicID, collectionID := programFixture(t)
	ctx := context.Background()
	stamp := time.Now().UnixMilli()
	batch := unsmeta.ProgramCommit{RunID: "canonical-run", StreamID: 10, VersionID: 1, EntryID: "poll", ReceivedAt: stamp,
		Writes: []unsmeta.ProgramWrite{
			{TopicID: topicID, CollectionID: collectionID, Value: unsmeta.IngestValue{RecordID: "temperature", Record: map[string]any{"value": 25}, Sample: map[string]any{"value": 25}}},
			{CollectionID: collectionID, Value: unsmeta.IngestValue{RecordID: "pressure", Record: map[string]any{"value": 3}, Sample: map[string]any{"value": 3}}},
		},
		Checkpoint: &repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: "canonical-run", Value: json.RawMessage(`{"page":1}`)},
	}
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_program_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reject sample write'; RETURN NEW; END $$; CREATE TRIGGER reject_program_event BEFORE INSERT ON public.uns_sample FOR EACH ROW EXECUTE FUNCTION public.reject_program_event()`).Error)
	committed, err := unsmeta.CommitProgram(ctx, batch)
	require.Error(t, err)
	require.False(t, committed)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&count).Error)
	require.Zero(t, count)
	require.Zero(t, metadataEvents(t, store))
	cursor, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.Zero(t, cursor.Revision)
	require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_program_event ON public.uns_sample; DROP FUNCTION public.reject_program_event()`).Error)
	batch.Checkpoint.ExpectedRevision = 99
	_, err = unsmeta.CommitProgram(ctx, batch)
	require.ErrorIs(t, err, repo.ErrConflict)
	batch.Checkpoint.ExpectedRevision = 0
	batch.Writes[1].Value.Sample["value"] = "bad schema"
	_, err = unsmeta.CommitProgram(ctx, batch)
	require.ErrorIs(t, err, unsmeta.ErrData)
	require.Zero(t, metadataEvents(t, store))
	batch.Writes[1].Value.Sample["value"] = 3
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.True(t, committed)
	require.Empty(t, batch.Checkpoint.Digest, "transaction digest must not mutate caller-owned output")
	require.Equal(t, 25, batch.Writes[0].Value.Record["value"], "normalization must not mutate caller maps")
	require.Zero(t, batch.Writes[0].Value.Time)
	require.EqualValues(t, 4, metadataEvents(t, store), "each output has a Collection event and a Topic view event")
	cursor, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 1, cursor.Revision)
	require.JSONEq(t, `{"page":1}`, string(cursor.Value))
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	var samples []repo.UnsSample
	require.NoError(t, store.UnsDB().Find(&samples).Error)
	for _, sample := range samples {
		require.Nil(t, sample.SourceID)
		require.Equal(t, "10", sample.StreamID)
	}
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.False(t, committed)
	require.EqualValues(t, 4, metadataEvents(t, store))
	other, err := unsmeta.New().Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Retarget", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	_, err = unsmeta.New().Save(ctx, topicID, "topic", 1, unsmeta.Document{Name: "Program Data view", CollectionID: other.ID, ViewMode: "records"})
	require.NoError(t, err)
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err, "committed replay must not resolve the new binding")
	require.False(t, committed)
	batch.RunID, batch.Checkpoint.RunID = "retargeted-run", "retargeted-run"
	batch.Checkpoint.ExpectedRevision = 1
	_, err = unsmeta.CommitProgram(ctx, batch)
	require.ErrorIs(t, err, repo.ErrConflict)
	cursor, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 1, cursor.Revision)
}

// 非数值坏质量是原始观察，不是零值；完整 Record 替换不能残留上次良值。
func TestProgramNonGoodObservationReplacesMeasurementWithoutLosingHistory(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	schema := &unsmeta.Schema{Type: "object", Required: []string{"native", "numericCalculationEligible"},
		Properties: map[string]unsmeta.Schema{"value": {Type: "number"}, "native": {Type: "object"}, "numericCalculationEligible": {Type: "boolean"}}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Quality observations", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	stamp := time.Now().UnixMilli()
	good := map[string]any{"value": 42, "native": map[string]any{"Value": 42, "Good": true}, "numericCalculationEligible": true}
	bad := map[string]any{"native": map[string]any{"Value": map[string]any{"Name": "No Data", "Value": 248, "IsSystem": true}, "Good": false}, "numericCalculationEligible": false}
	batch := unsmeta.ProgramCommit{RunID: "quality-good", StreamID: 20, VersionID: 1, EntryID: "collect", ReceivedAt: stamp,
		Writes: []unsmeta.ProgramWrite{{CollectionID: collectionID, Value: unsmeta.IngestValue{RecordID: "point", Record: good, Sample: good, Time: stamp - 1, Quality: "good"}}}}
	committed, err := unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.True(t, committed)
	batch.RunID = "quality-bad"
	batch.Writes[0].Value = unsmeta.IngestValue{RecordID: "point", Record: bad, Sample: bad, Time: stamp, Quality: "bad"}
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.True(t, committed)
	var metaCount int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsMeta{}).Where("kind = 'collection' AND id = ?", collectionID).Count(&metaCount).Error)
	require.EqualValues(t, 1, metaCount)
	record, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "point")
	require.NoError(t, err)
	expected, err := json.Marshal(bad)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(record.Data))
	require.NotContains(t, string(record.Data), `"value":42`, "last good value must not leak into the new quality observation")
	var samples []repo.UnsSample
	require.NoError(t, store.UnsDB().Where("collection_id = ? AND record_id = ?", collectionID, "point").Order("time asc").Find(&samples).Error)
	require.Len(t, samples, 2)
	require.Equal(t, "good", samples[0].Quality)
	require.Equal(t, "bad", samples[1].Quality)
	require.JSONEq(t, string(expected), string(samples[1].Data))
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.False(t, committed)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id = ?", collectionID).Count(&count).Error)
	require.EqualValues(t, 2, count, "replay must not append duplicate quality history")
}

func TestProgramCanonicalOwnershipAndFilteredCursor(t *testing.T) {
	store, _, collectionID := programFixture(t)
	ctx := context.Background()
	sourceID := "12345"
	require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{ID: sourceID, Kind: "source", Data: json.RawMessage(`{"name":"owned"}`), Revision: 1}).Error)
	require.NoError(t, store.UnsDB().Create(&repo.UnsRecord{SourceID: &sourceID, CollectionID: collectionID, ID: "managed", Data: json.RawMessage(`{"value":1}`), Revision: 1}).Error)
	batch := unsmeta.ProgramCommit{RunID: "owned-record", StreamID: 10, VersionID: 1, EntryID: "poll", ReceivedAt: time.Now().UnixMilli(),
		Writes: []unsmeta.ProgramWrite{{CollectionID: collectionID, Value: unsmeta.IngestValue{RecordID: "managed", Record: map[string]any{"value": 9}}}}}
	_, err := unsmeta.CommitProgram(ctx, batch)
	require.ErrorIs(t, err, unsmeta.ErrReferenced)
	require.Zero(t, metadataEvents(t, store))
	batch.RunID, batch.Writes = "filtered-page", nil
	batch.Checkpoint = &repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: batch.RunID, Value: json.RawMessage(`{"page":2}`)}
	created, err := unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.True(t, created)
	cursor, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 1, cursor.Revision)
	require.Zero(t, metadataEvents(t, store))
	batch.Checkpoint.Value = json.RawMessage(`{"page":3}`)
	_, err = unsmeta.CommitProgram(ctx, batch)
	require.ErrorIs(t, err, repo.ErrConflict)
}

func TestProgramWriteRevisionJSON(t *testing.T) {
	for _, invalid := range []string{"null", "-1", "1.5", `"1"`, "true", "[]", "{}", "9007199254740992", "9223372036854775808"} {
		t.Run(invalid, func(t *testing.T) {
			var write unsmeta.ProgramWrite
			err := json.Unmarshal([]byte(`{"collectionId":"c","value":{"recordId":"r","record":{"value":1}},"expectedRevision":`+invalid+`}`), &write)
			require.Error(t, err)
		})
	}
	for _, valid := range []string{"", `,"expectedRevision":0`, `,"expectedRevision":1`, `,"expectedRevision":9007199254740991`} {
		var write unsmeta.ProgramWrite
		require.NoError(t, json.Unmarshal([]byte(`{"collectionId":"c","value":{"recordId":"r","record":{"value":1}}`+valid+`}`), &write))
		if valid == "" {
			require.Nil(t, write.ExpectedRevision)
		} else {
			require.NotNil(t, write.ExpectedRevision)
		}
	}
	negative := int64(-1)
	_, err := unsmeta.PreviewProgramWrite(context.Background(), unsmeta.ProgramWrite{ExpectedRevision: &negative}, time.Now().UnixMilli())
	require.ErrorIs(t, err, unsmeta.ErrInvalid)
	unsafe := unsmeta.MaxProgramExpectedRevision + 1
	_, err = unsmeta.PreviewProgramWrite(context.Background(), unsmeta.ProgramWrite{ExpectedRevision: &unsafe}, time.Now().UnixMilli())
	require.ErrorIs(t, err, unsmeta.ErrInvalid)
}

func TestProgramCanonicalRevisionAtomicityAndReplay(t *testing.T) {
	store, _, collectionID := programFixture(t)
	ctx := context.Background()
	zero, one, two := int64(0), int64(1), int64(2)
	batch := unsmeta.ProgramCommit{RunID: "revision-create", StreamID: 10, VersionID: 1, EntryID: "poll", ReceivedAt: time.Now().UnixMilli(),
		Writes:     []unsmeta.ProgramWrite{{CollectionID: collectionID, ExpectedRevision: &zero, Value: unsmeta.IngestValue{RecordID: "r", Record: map[string]any{"value": 1}, Sample: map[string]any{"value": 1}}}},
		Checkpoint: &repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: "revision-create", Value: json.RawMessage(`{"page":1}`)},
	}
	committed, err := unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.True(t, committed)
	unchanged := batch
	unchanged.RunID, unchanged.Checkpoint = "revision-unchanged", nil
	unchanged.Writes = []unsmeta.ProgramWrite{{CollectionID: collectionID, ExpectedRevision: &one, Value: unsmeta.IngestValue{RecordID: "r", Record: map[string]any{"value": 1}}}}
	committed, err = unsmeta.CommitProgram(ctx, unchanged)
	require.NoError(t, err)
	require.True(t, committed)
	row, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Revision)
	// 即使异常历史行保存了零版本，create-only 也必须拒绝覆盖它。
	// 仅在本测试独立库暂时移除正版本约束，模拟迁移遗留的异常行。
	require.NoError(t, store.UnsDB().Exec("ALTER TABLE public.uns_record DROP CONSTRAINT uns_record_revision_check").Error)
	require.NoError(t, store.UnsDB().Create(&repo.UnsRecord{CollectionID: collectionID, ID: "legacy-zero", Revision: 0, Data: json.RawMessage(`{"value":7}`)}).Error)
	zeroExisting := batch
	zeroExisting.RunID, zeroExisting.Checkpoint = "revision-legacy-zero", nil
	zeroExisting.Writes = []unsmeta.ProgramWrite{{CollectionID: collectionID, ExpectedRevision: &zero, Value: unsmeta.IngestValue{RecordID: "legacy-zero", Record: map[string]any{"value": 8}, Sample: map[string]any{"value": 8}}}}
	committed, err = unsmeta.CommitProgram(ctx, zeroExisting)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.False(t, committed)
	row, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "legacy-zero")
	require.NoError(t, err)
	require.Zero(t, row.Revision)
	require.JSONEq(t, `{"value":7}`, string(row.Data))
	require.NoError(t, store.UnsDB().Unscoped().Delete(&row).Error)
	require.NoError(t, store.UnsDB().Exec("ALTER TABLE public.uns_record ADD CONSTRAINT uns_record_revision_check CHECK (revision > 0)").Error)
	// CAS is checked before identical-data optimization and before sample append.
	batch.RunID, batch.Checkpoint.RunID = "revision-zero-existing", "revision-zero-existing"
	batch.Checkpoint.ExpectedRevision = 1
	_, err = unsmeta.CommitProgram(ctx, batch)
	require.ErrorIs(t, err, repo.ErrConflict)
	batch.RunID, batch.Checkpoint.RunID = "revision-exact", "revision-exact"
	batch.Writes[0].ExpectedRevision = &one
	batch.Writes[0].Value.Record["value"] = 2
	batch.Checkpoint.Value = json.RawMessage(`{"page":2}`)
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.True(t, committed)
	// Another writer changes the record; committed replay must bypass today's CAS.
	_, err = unsmeta.New().SaveRecord(ctx, collectionID, "r", 2, map[string]any{"value": 3, "note": "operator"})
	require.NoError(t, err)
	committed, err = unsmeta.CommitProgram(ctx, batch)
	require.NoError(t, err)
	require.False(t, committed)
	beforeEvents := metadataEvents(t, store)
	for index, failure := range []string{"stale-equal", "schema", "missing"} {
		t.Run(failure, func(t *testing.T) {
			candidate := batch
			candidate.RunID = fmt.Sprintf("revision-fail-%d", index)
			candidate.Checkpoint = &repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: candidate.RunID, ExpectedRevision: 2, Value: json.RawMessage(`{"page":99}`)}
			candidate.Writes = []unsmeta.ProgramWrite{
				{CollectionID: collectionID, ExpectedRevision: &zero, Value: unsmeta.IngestValue{RecordID: "rolled-back", Record: map[string]any{"value": 8}, Sample: map[string]any{"value": 8}}},
				{CollectionID: collectionID, ExpectedRevision: &two, Value: unsmeta.IngestValue{RecordID: "r", Record: map[string]any{"value": 3, "note": "operator"}, Sample: map[string]any{"value": 9}}},
			}
			if failure == "schema" {
				candidate.Writes[1].Value.Record["value"] = "invalid"
			} else if failure == "missing" {
				candidate.Writes[1].Value.RecordID = "absent"
			}
			committed, err := unsmeta.CommitProgram(ctx, candidate)
			require.Error(t, err)
			require.False(t, committed)
			if failure != "schema" {
				require.ErrorIs(t, err, repo.ErrConflict)
			} else {
				require.ErrorIs(t, err, unsmeta.ErrData)
			}
			_, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "rolled-back")
			require.ErrorIs(t, err, repo.ErrNotFound)
			row, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
			require.NoError(t, err)
			require.EqualValues(t, 3, row.Revision)
			require.JSONEq(t, `{"value":3,"note":"operator"}`, string(row.Data))
			var samples int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&samples).Error)
			require.EqualValues(t, 2, samples)
			require.Equal(t, beforeEvents, metadataEvents(t, store))
			var receipts int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='task' AND id=?", "programMetadataCommit:"+candidate.RunID).Count(&receipts).Error)
			require.Zero(t, receipts)
			cursor, err := repo.NewSourceStreamRepo(ctx).ProgramCheckpoint(ctx, 10, "poll")
			require.NoError(t, err)
			require.EqualValues(t, 2, cursor.Revision)
			require.JSONEq(t, `{"page":2}`, string(cursor.Value))
		})
	}
	// Omitted revision still unconditionally updates an existing record.
	legacy := unsmeta.ProgramCommit{RunID: "revision-legacy", StreamID: 10, VersionID: 1, EntryID: "poll", ReceivedAt: batch.ReceivedAt,
		Writes: []unsmeta.ProgramWrite{{CollectionID: collectionID, Value: unsmeta.IngestValue{RecordID: "r", Record: map[string]any{"value": 4}}}}}
	committed, err = unsmeta.CommitProgram(ctx, legacy)
	require.NoError(t, err)
	require.True(t, committed)
}

func TestProgramCanonicalConcurrentRevisionWriters(t *testing.T) {
	store, _, collectionID := programFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := unsmeta.New().SaveRecord(ctx, collectionID, "r", 0, map[string]any{"value": 0})
	require.NoError(t, err)
	beforeEvents := metadataEvents(t, store)
	one := int64(1)
	start := make(chan struct{})
	errors := make(chan error, 2)
	for i := 1; i <= 2; i++ {
		go func(value int) {
			<-start
			_, err := unsmeta.CommitProgram(ctx, unsmeta.ProgramCommit{RunID: fmt.Sprintf("concurrent-cas-%d", value), StreamID: 10, VersionID: 1, EntryID: "poll", ReceivedAt: time.Now().UnixMilli(),
				Writes: []unsmeta.ProgramWrite{{CollectionID: collectionID, ExpectedRevision: &one, Value: unsmeta.IngestValue{RecordID: "r", Record: map[string]any{"value": value}, Sample: map[string]any{"value": value}}}}})
			errors <- err
		}(i)
	}
	close(start)
	first, second := <-errors, <-errors
	if first == nil {
		require.ErrorIs(t, second, repo.ErrConflict)
	} else {
		require.ErrorIs(t, first, repo.ErrConflict)
		require.NoError(t, second)
	}
	row, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "r")
	require.NoError(t, err)
	require.EqualValues(t, 2, row.Revision)
	var samples int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&samples).Error)
	require.EqualValues(t, 1, samples)
	require.EqualValues(t, beforeEvents+2, metadataEvents(t, store))
}
