package unsmeta_test

// 真实 PG 验证采集映射的事件、重放、所有权及数据写入失败的整体回滚。
import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMappedMetadataEventsOwnershipAndReplay(t *testing.T) {
	store, topicID, collectionID := programFixture(t)
	ctx := context.Background()
	stamp := time.Now().UnixMilli()
	mapping := unsmeta.Mapping{TopicID: topicID, CollectionID: collectionID, RecordID: "machine", SampleEnabled: true}
	input := map[string]any{"value": 3}
	require.NoError(t, unsmeta.CommitMapped(ctx, "mapped-1", mapping, input, stamp))
	require.EqualValues(t, 2, metadataEvents(t, store))
	require.NoError(t, unsmeta.CommitMapped(ctx, "mapped-1", mapping, input, stamp))
	require.EqualValues(t, 2, metadataEvents(t, store))
	var samples int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&samples).Error)
	require.EqualValues(t, 1, samples)
	input["value"] = 4
	require.ErrorIs(t, unsmeta.CommitMapped(ctx, "mapped-1", mapping, input, stamp), repo.ErrConflict)
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_mapped_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reject record write'; RETURN NEW; END $$; CREATE TRIGGER reject_mapped_event BEFORE INSERT ON public.uns_record FOR EACH ROW EXECUTE FUNCTION public.reject_mapped_event()`).Error)
	mapping.RecordID = "failed"
	require.Error(t, unsmeta.CommitMapped(ctx, "mapped-failed", mapping, input, stamp))
	_, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "failed")
	require.ErrorIs(t, err, repo.ErrNotFound)
	require.EqualValues(t, 2, metadataEvents(t, store))
	require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_mapped_event ON public.uns_record; DROP FUNCTION public.reject_mapped_event()`).Error)
	sourceID := "8888"
	require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{ID: sourceID, Kind: "source", Revision: 1, Data: json.RawMessage(`{"name":"owner"}`)}).Error)
	require.NoError(t, store.UnsDB().Create(&repo.UnsRecord{SourceID: &sourceID, CollectionID: collectionID, ID: "owned", Revision: 1, Data: json.RawMessage(`{"value":1}`)}).Error)
	mapping.RecordID = "owned"
	require.ErrorIs(t, unsmeta.CommitMapped(ctx, "mapped-owned", mapping, input, stamp), unsmeta.ErrReferenced)
	require.EqualValues(t, 2, metadataEvents(t, store))
}

func TestMappedPageSampleGateCoversAllNestedTimes(t *testing.T) {
	store, topicID, collectionID := programFixture(t)
	ctx := context.Background()
	stamp := time.Now().UnixMilli()
	mapping := unsmeta.Mapping{TopicID: topicID, CollectionID: collectionID, RecordIDPath: "id",
		RecordPath: "reading", SampleEnabled: true, SamplePath: "reading", TimePath: "time"}
	value := func(id string, at int64) any {
		return map[string]any{"id": id, "time": at, "reading": map[string]any{"value": 3}}
	}
	require.NoError(t, unsmeta.CommitMapped(ctx, "warm-chunk", mapping, value("warm", stamp), stamp))
	inputs := []any{value("existing", stamp), value("future", stamp+8*24*60*60*1000)}
	require.NoError(t, repo.MetadataEventTransaction(ctx, store.UnsDB(), func(tx *gorm.DB) error {
		if err := unsmeta.PrepareMappedSamples(ctx, tx, mapping, inputs, stamp); err != nil {
			return err
		}
		var locked bool
		require.NoError(t, tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid()
		 AND relation='public.uns_sample'::regclass AND mode='ShareUpdateExclusiveLock' AND granted)`).Scan(&locked).Error)
		require.True(t, locked, "the later input timestamp, not just receivedAt, requires a chunk gate before any page output")
		pageCtx := repo.WithUnsTransaction(ctx, tx)
		for i, input := range inputs {
			if err := unsmeta.CommitMapped(pageCtx, fmt.Sprintf("page-%d", i), mapping, input, stamp); err != nil {
				return err
			}
		}
		return nil
	}))
	var records, samples int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("collection_id=?", collectionID).Count(&records).Error)
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", collectionID).Count(&samples).Error)
	require.EqualValues(t, 3, records)
	require.EqualValues(t, 3, samples)
	require.NoError(t, repo.MetadataEventTransaction(ctx, store.UnsDB(), func(tx *gorm.DB) error {
		require.ErrorIs(t, unsmeta.PrepareMappedSamples(ctx, tx, mapping,
			[]any{value("not-written", stamp), value("bad-time", 0)}, stamp), unsmeta.ErrData)
		return nil
	}))
	_, err := repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "not-written")
	require.ErrorIs(t, err, repo.ErrNotFound)
}
