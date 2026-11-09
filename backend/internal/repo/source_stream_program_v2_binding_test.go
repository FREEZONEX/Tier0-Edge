package repo_test

import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestProgramV2CanonicalBindingSnapshotAndRetarget(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session"})
	u := unsmeta.New()
	created, err := u.CreateCollection(ctx, "", unsmeta.Document{Name: "Bound", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	topic, err := u.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Bound view", CollectionID: collectionID})
	require.NoError(t, err)
	topicID := topic.ID
	manifest := v2RequestManifest()
	manifest.Topics = []streamexec.ProgramTopic{{TopicID: topicID, Access: "write"}, {CollectionID: collectionID, Access: "read"}}
	bindings, err := repo.FreezeProgramV2Bindings(ctx, manifest)
	require.NoError(t, err, "an explicit Topic is a valid records view")
	require.Equal(t, collectionID, bindings[0].CollectionID)
	r := repo.NewSourceStreamRepo(store)
	doc := `{"script":"canonical"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "canonical", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{}, bindings)
	require.NoError(t, err)
	rawManifest, err := version.ProgramV2Manifest()
	require.NoError(t, err)
	require.Equal(t, manifest, rawManifest, "trusted bindings must not change runtime registration contract")
	storedBindings, err := version.ProgramV2Bindings()
	require.NoError(t, err)
	require.Equal(t, bindings, storedBindings)
	_, err = version.ProgramExecutionPlan()
	require.NoError(t, err, "the publication digest includes the exact UNS binding snapshot")
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	_, err = u.Save(ctx, topicID, "topic", 1, unsmeta.Document{Name: "Renamed", CollectionID: collectionID})
	require.NoError(t, err)
	require.NoError(t, repo.ValidateProgramV2Bindings(ctx, manifest, bindings), "name is not data authority")
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal([]byte(version.ProgramManifest), &snapshot))
	for _, invalid := range []any{[]any{}, []any{map[string]any{"unknown": true}}, append(bindings, bindings[0])} {
		snapshot["_unsBindings"] = invalid
		raw, marshalErr := json.Marshal(snapshot)
		require.NoError(t, marshalErr)
		forged := version
		forged.ProgramManifest = string(raw)
		_, err = forged.ProgramV2Bindings()
		require.Error(t, err)
	}
	other, err := u.Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Other", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	_, err = u.Save(ctx, topicID, "topic", 2, unsmeta.Document{Name: "Renamed", CollectionID: other.ID})
	require.NoError(t, err)
	require.ErrorIs(t, repo.ValidateProgramV2Bindings(ctx, manifest, bindings), repo.ErrConflict)
	fresh, err := repo.FreezeProgramV2Bindings(ctx, manifest)
	require.NoError(t, err)
	_, _, err = r.StageProgramV2(ctx, stream.ID, stream.Revision, version.ID, doc, manifest, []repo.StreamSourceSnapshot{}, fresh)
	require.ErrorIs(t, err, repo.ErrConflict, "rollback cannot silently bind an old version to new data")
	manifest.Topics = []streamexec.ProgramTopic{{CollectionID: topicID, Access: "read"}}
	_, err = repo.FreezeProgramV2Bindings(ctx, manifest)
	require.Error(t, err, "Topic UUID cannot masquerade as Collection")
}
