package repo_test

// 验证可读及纯数字 Meta ID 能冻结为可信绑定，不与旧数字 Topic 混淆。
import (
	"context"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestProgramV2NamedMetadataBindings(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	u := unsmeta.New()
	const collectionID = "plant0-equipment-state"
	for _, id := range []string{collectionID, "80"} {
		_, err := u.Save(ctx, id, "collection", 0, unsmeta.Document{Name: "Collection " + id, RecordSchema: &unsmeta.Schema{Type: "object"}})
		require.NoError(t, err)
	}
	for _, id := range []string{"plant0-equipment-state-topic", "79"} {
		_, err := u.Save(ctx, id, "topic", 0, unsmeta.Document{Name: "Topic " + id, CollectionID: collectionID, ViewMode: "records"})
		require.NoError(t, err)
	}
	manifest := v2RequestManifest()
	manifest.Topics = []streamexec.ProgramTopic{
		{TopicID: "plant0-equipment-state-topic", Access: "read-write"},
		{TopicID: "79", Space: "metadata", Access: "read-write"},
		{CollectionID: collectionID, Access: "read"},
		{CollectionID: "80", Access: "read"},
		{TopicID: "79"},
	}
	bindings, err := repo.FreezeProgramV2Bindings(ctx, manifest)
	require.NoError(t, err)
	require.Len(t, bindings, 4, "legacy numeric reference is not a Meta binding")
	require.Equal(t, "metadata", bindings[1].Reference.Space)
	require.Equal(t, collectionID, bindings[1].CollectionID)
	r := repo.NewSourceStreamRepo(store)
	document := `{"script":"identity binding persistence"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "identity", Model: "program", Document: document}, 0, manifest)
	require.NoError(t, err)
	_, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, document, manifest, []repo.StreamSourceSnapshot{}, bindings)
	require.NoError(t, err)
	stored, err := version.ProgramV2Bindings()
	require.NoError(t, err)
	require.Equal(t, bindings, stored)
	require.NoError(t, repo.ValidateProgramV2Bindings(ctx, manifest, stored))
}
