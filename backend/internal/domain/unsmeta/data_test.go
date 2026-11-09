package unsmeta_test

// 原生 Record/Sample 写入与事件同事务；无变化和幂等样本不会重复发事件。
import (
	"context"
	"testing"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestNativeMetadataWritesEventsAndRollback(t *testing.T) {
	store, _, collectionID := programFixture(t)
	ctx := context.Background()
	s := unsmeta.New()
	row, err := s.SaveRecord(ctx, collectionID, "machine", 0, map[string]any{"value": 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, metadataEvents(t, store))
	unchanged, err := s.SaveRecord(ctx, collectionID, "machine", row.Revision, map[string]any{"value": 1})
	require.NoError(t, err)
	require.Equal(t, row.Revision, unchanged.Revision)
	require.EqualValues(t, 2, metadataEvents(t, store))
	_, err = s.SaveRecord(ctx, collectionID, "machine", 0, map[string]any{"value": 9})
	require.ErrorIs(t, err, repo.ErrConflict)
	input := []unsmeta.SampleInput{{ID: "sample-1", RecordID: "machine", Time: time.Now().UnixMilli(), Data: map[string]any{"value": 2}}}
	result, err := s.AppendSamples(ctx, collectionID, input)
	require.NoError(t, err)
	require.EqualValues(t, 1, result["inserted"])
	require.EqualValues(t, 4, metadataEvents(t, store))
	result, err = s.AppendSamples(ctx, collectionID, input)
	require.NoError(t, err)
	require.EqualValues(t, 1, result["replayed"])
	require.EqualValues(t, 4, metadataEvents(t, store))
	input[0].Data["value"] = 3
	_, err = s.AppendSamples(ctx, collectionID, input)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_native_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reject data write'; RETURN NEW; END $$; CREATE TRIGGER reject_native_event BEFORE INSERT ON public.uns_record FOR EACH ROW EXECUTE FUNCTION public.reject_native_event(); CREATE TRIGGER reject_native_sample BEFORE INSERT ON public.uns_sample FOR EACH ROW EXECUTE FUNCTION public.reject_native_event()`).Error)
	_, err = s.SaveRecord(ctx, collectionID, "new-record", 0, map[string]any{"value": 5})
	require.Error(t, err)
	_, err = repo.NewUnsMetaRepo(ctx).Record(ctx, collectionID, "new-record")
	require.ErrorIs(t, err, repo.ErrNotFound)
	input[0].ID = "sample-2"
	_, err = s.AppendSamples(ctx, collectionID, input)
	require.Error(t, err)
	var count int64
	require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.EqualValues(t, 4, metadataEvents(t, store))
}
