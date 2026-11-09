package unsmeta_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

// Source 未建立映射时可以序列化 null；引用检查不得因此阻止无关节点归档，
// 也不能遗漏真实的草稿或生效版本引用。只使用每测试独立的 PostgreSQL。
func TestNamespaceArchiveNullableSourceContainers(t *testing.T) {
	cases := []struct {
		name       string
		streams    func(string) any
		referenced bool
	}{
		{"null", func(string) any { return nil }, false},
		{"empty", func(string) any { return map[string]any{} }, false},
		{"null-versions", func(string) any {
			return map[string]any{"draft": map[string]any{"versions": nil}}
		}, false},
		{"draft-reference", func(id string) any {
			return map[string]any{"draft": map[string]any{"metadataTarget": map[string]any{"topicId": id}, "versions": nil}}
		}, true},
		{"active-reference", func(id string) any {
			return map[string]any{"active": map[string]any{"activeVersion": "v1", "versions": map[string]any{
				"v1": map[string]any{"document": map[string]any{"metadataTarget": map[string]any{"topicId": id}}},
			}}}
		}, true},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			ctx := context.Background()
			u := unsmeta.New()
			created, err := u.CreateCollection(ctx, "", unsmeta.Document{Name: "nullable-" + tc.name, RecordSchema: &unsmeta.Schema{Type: "object"}})
			require.NoError(t, err)
			topic, err := u.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Reference view", ViewMode: "records", CollectionID: created["collection"].(repo.UnsMeta).ID})
			require.NoError(t, err)
			raw, err := json.Marshal(map[string]any{"schemaVersion": 1, "name": "Source " + tc.name, "adapter": "http-api", "config": map[string]any{"connection": map[string]any{}, "streams": tc.streams(topic.ID)}})
			require.NoError(t, err)
			require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{ID: fmt.Sprint(91000 + index), Kind: "source", Revision: 1, Data: raw}).Error)
			_, err = u.State(ctx, topic.ID, topic.Revision, false, "topic")
			require.NoError(t, err, "external references remain while the object is recycled")
			row, readErr := repo.NewUnsMetaRepo(ctx).Get(ctx, topic.ID, true, "topic")
			require.NoError(t, readErr)
			require.NotZero(t, row.DeletedAt)

		})
	}
}

func TestProgramRecycleRetainsCurrentReferences(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	u := unsmeta.New()
	collection, err := u.Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Referenced", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	topic, err := u.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Reference view", ViewMode: "records", CollectionID: collection.ID})
	require.NoError(t, err)
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{{TopicID: topic.ID, Access: "write"}}, Entries: []streamexec.ProgramEntry{{ID: "run", Kind: "request"}}}
	r := repo.NewSourceStreamRepo(store)
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "Source-less reference", Model: "program", Document: `{"script":"canonical"}`}, 0, manifest)
	require.NoError(t, err)
	_, err = u.State(ctx, collection.ID, collection.Revision, false, "collection")
	require.NoError(t, err)
	var retained repo.Stream
	require.NoError(t, store.UnsDB().Where("id=?", stream.ID).Take(&retained).Error)
	require.JSONEq(t, stream.DraftManifest, retained.DraftManifest)
	_, err = u.SaveRecord(ctx, collection.ID, "blocked", 0, map[string]any{})
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = u.State(ctx, topic.ID, topic.Revision, false, "topic")
	require.NoError(t, err)
}
