package unsmeta_test

// PostgreSQL 验证 UUID Topic/Collection 读取、字段视图、冻结绑定和资源限制。
import (
	"context"
	"strings"
	"testing"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestProgramCanonicalReadBindingAndLimits(t *testing.T) {
	_, topicID, collectionID := programFixture(t)
	ctx := context.Background()
	s := unsmeta.New()
	_, err := s.SaveRecord(ctx, collectionID, "machine", 0, map[string]any{"value": 2})
	require.NoError(t, err)
	frozen, err := repo.FreezeProgramV2Bindings(ctx, streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{{TopicID: topicID, Access: "read"}}})
	require.NoError(t, err)
	view, err := unsmeta.ReadProgramTopic(ctx, topicID, frozen[0].TopicBindingHash)
	require.NoError(t, err)
	require.Equal(t, topicID, view["topicId"])
	require.Equal(t, collectionID, view["data"].(map[string]any)["collectionId"])
	resolved, err := unsmeta.ProgramCollectionForTopic(ctx, topicID)
	require.NoError(t, err)
	require.Equal(t, collectionID, resolved)
	page, err := unsmeta.ReadProgramCollection(ctx, collectionID, unsmeta.ProgramReadOptions{RecordID: "machine", Limit: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, page["total"])
	for _, options := range []unsmeta.ProgramReadOptions{{Limit: 101}, {Limit: -1}, {Offset: 10001}, {Offset: -1}, {RecordID: "bad/id"}} {
		_, err := unsmeta.ReadProgramCollection(ctx, collectionID, options)
		require.ErrorIs(t, err, unsmeta.ErrInvalid)
	}
	field, err := s.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Field Read", ViewMode: "fields", Fields: []unsmeta.FieldBinding{{Name: "temperature", Kind: "record", CollectionID: collectionID, RecordID: "machine", FieldPath: "value"}}})
	require.NoError(t, err)
	fieldView, err := unsmeta.ReadProgramTopic(ctx, field.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, fieldView["fields"].([]map[string]any)[0]["value"])
	_, err = unsmeta.ProgramCollectionForTopic(ctx, field.ID)
	require.ErrorIs(t, err, unsmeta.ErrInvalid)
	_, err = s.Save(ctx, topicID, "topic", 1, unsmeta.Document{Name: "Renamed", ViewMode: "records", CollectionID: collectionID})
	require.NoError(t, err)
	_, err = unsmeta.ReadProgramTopic(ctx, topicID, frozen[0].TopicBindingHash)
	require.NoError(t, err, "display rename cannot invalidate binding")
	other, err := s.Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Other", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	_, err = s.Save(ctx, topicID, "topic", 2, unsmeta.Document{Name: "Renamed", ViewMode: "records", CollectionID: other.ID})
	require.NoError(t, err)
	_, err = unsmeta.ReadProgramTopic(ctx, topicID, frozen[0].TopicBindingHash)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = unsmeta.ReadProgramTopic(ctx, field.ID, "short")
	require.ErrorIs(t, err, unsmeta.ErrInvalid)
	_, err = unsmeta.ReadProgramCollection(ctx, "bad/id", unsmeta.ProgramReadOptions{})
	require.ErrorIs(t, err, unsmeta.ErrInvalid)
	for _, id := range []string{"large-a", "large-b"} {
		_, err := s.SaveRecord(ctx, collectionID, id, 0, map[string]any{"value": 1, "payload": strings.Repeat("x", 150000)})
		require.NoError(t, err)
	}
	page, err = unsmeta.ReadProgramCollection(ctx, collectionID, unsmeta.ProgramReadOptions{Limit: 1})
	require.NoError(t, err, "size preflight must consider only the selected page")
	require.Len(t, page["records"], 1)
	page, err = unsmeta.ReadProgramCollection(ctx, collectionID, unsmeta.ProgramReadOptions{Limit: 1, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, "machine", page["records"].([]repo.UnsRecord)[0].ID)
	_, err = unsmeta.ReadProgramCollection(ctx, collectionID, unsmeta.ProgramReadOptions{})
	require.ErrorIs(t, err, unsmeta.ErrData, "aggregate read must stay inside executor JSON limit")
	_, err = s.AppendSamples(ctx, collectionID, []unsmeta.SampleInput{{ID: "large-sample", RecordID: "machine", Time: time.Now().UnixMilli(), Data: map[string]any{"value": 3, "payload": strings.Repeat("x", 150000)}}})
	require.NoError(t, err)
	for _, kind := range []string{"record", "sample"} {
		recordID := "large-a"
		if kind == "sample" {
			recordID = "machine"
		}
		largeFields, err := s.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Large " + kind, ViewMode: "fields", Fields: []unsmeta.FieldBinding{
			{Name: "a", Kind: kind, CollectionID: collectionID, RecordID: recordID, FieldPath: "value"},
			{Name: "b", Kind: kind, CollectionID: collectionID, RecordID: recordID, FieldPath: "value"},
		}})
		require.NoError(t, err)
		_, err = unsmeta.ReadProgramTopic(ctx, largeFields.ID)
		require.ErrorIs(t, err, unsmeta.ErrData, "fields must budget decoded bodies even when scalar output would be small")
	}
}
