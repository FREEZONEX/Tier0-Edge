// 验证目录复制事务、数据引用、名称冲突、无效目标和旧版本拒绝。
package unsmeta_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/domain/common"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestCopyNamespace(t *testing.T) {
	testkit.NewTestDB(t)
	s := unsmeta.New()
	ctx := context.Background()
	folder, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: "Line"})
	require.NoError(t, err)
	child, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: "Machine", ParentID: folder.ID})
	require.NoError(t, err)
	collection, err := s.Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Data", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	topic, err := s.Save(ctx, "", "topic", 0, unsmeta.Document{Name: "Temperature", ParentID: child.ID, CollectionID: collection.ID})
	require.NoError(t, err)
	copy, err := s.Copy(ctx, folder.ID, folder.Revision, "", "", nil)
	require.NoError(t, err)
	require.NotEqual(t, folder.ID, copy.ID)
	var doc unsmeta.Document
	require.NoError(t, json.Unmarshal(copy.Data, &doc))
	require.Equal(t, "Line (2)", doc.Name)
	rows, count, err := repo.NewUnsMetaRepo(ctx).List(ctx, "namespace", false, 1, 500)
	require.NoError(t, err)
	require.EqualValues(t, 7, count)
	copiedFolderID := ""
	for _, row := range rows {
		var d unsmeta.Document
		require.NoError(t, json.Unmarshal(row.Data, &d))
		if d.ParentID == copy.ID {
			copiedFolderID = row.ID
		}
	}
	require.NotEmpty(t, copiedFolderID)
	found := false
	for _, row := range rows {
		var d unsmeta.Document
		require.NoError(t, json.Unmarshal(row.Data, &d))
		if d.ParentID == copiedFolderID {
			found = true
			require.NotEqual(t, topic.ID, row.ID)
			require.Equal(t, collection.ID, d.CollectionID)
		}
	}
	require.True(t, found)
	description := "Changed before paste"
	changed, err := s.Copy(ctx, topic.ID, topic.Revision, "", "New name", &description)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(changed.Data, &doc))
	require.Equal(t, "New name", doc.Name)
	require.Equal(t, description, doc.Description)
	require.Empty(t, doc.ParentID)
	for _, input := range []struct {
		id           string
		revision     int64
		parent, name string
	}{
		{"/invalid", 1, "", ""}, {folder.ID, 0, "", ""}, {folder.ID, 1, "/invalid", ""},
		{folder.ID, 99, "", ""}, {collection.ID, 1, "", ""}, {folder.ID, 1, child.ID, ""},
		{folder.ID, 1, topic.ID, ""}, {folder.ID, 1, "", "Line"},
		{folder.ID, 1, "", "bad/name"}, {folder.ID, 1, "missing", ""}, {"missing", 1, "", ""},
	} {
		_, err := s.Copy(ctx, input.id, input.revision, input.parent, input.name, nil)
		require.Error(t, err)
	}
	_, after, err := repo.NewUnsMetaRepo(ctx).List(ctx, "namespace", false, 1, 500)
	require.NoError(t, err)
	require.EqualValues(t, 8, after, "failed operations must not leave partial folders")
	long, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: strings.Repeat("字", common.NameMaxLen)})
	require.NoError(t, err)
	duplicated, err := s.Copy(ctx, long.ID, long.Revision, "", "", nil)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(duplicated.Data, &doc))
	require.Len(t, []rune(doc.Name), common.NameMaxLen)
	require.True(t, strings.HasSuffix(doc.Name, " (2)"))
	_, err = s.State(ctx, topic.ID, topic.Revision, false)
	require.NoError(t, err)
	_, err = s.Copy(ctx, topic.ID, topic.Revision, "", "", nil)
	require.Error(t, err)
}

func TestCopyNamespaceCaseInsensitiveNames(t *testing.T) {
	for _, name := range []string{"Line", "Équipement"} {
		t.Run(name, func(t *testing.T) {
			testkit.NewTestDB(t)
			s := unsmeta.New()
			ctx := context.Background()
			source, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: name})
			require.NoError(t, err)
			target, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: "Destination"})
			require.NoError(t, err)
			collection, err := s.Save(ctx, "", "collection", 0, unsmeta.Document{Name: name + " (3)", RecordSchema: &unsmeta.Schema{Type: "object"}})
			require.NoError(t, err)
			// Folder and Topic share the same directory uniqueness constraint.
			_, err = s.Save(ctx, "", "topic", 0, unsmeta.Document{Name: strings.ToLower(name), ParentID: target.ID, CollectionID: collection.ID})
			require.NoError(t, err)
			_, err = s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: strings.ToUpper(name) + " (2)", ParentID: target.ID})
			require.NoError(t, err)
			archived, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: name + " (3)", ParentID: target.ID})
			require.NoError(t, err)
			_, err = s.State(ctx, archived.ID, archived.Revision, false)
			require.NoError(t, err)

			_, before, err := repo.NewUnsMetaRepo(ctx).List(ctx, "namespace", false, 1, 500)
			require.NoError(t, err)
			_, err = s.Copy(ctx, source.ID, source.Revision, target.ID, strings.ToUpper(name), nil)
			require.ErrorIs(t, err, repo.ErrConflict)
			_, after, err := repo.NewUnsMetaRepo(ctx).List(ctx, "namespace", false, 1, 500)
			require.NoError(t, err)
			require.Equal(t, before, after)

			copied, err := s.Copy(ctx, source.ID, source.Revision, target.ID, "", nil)
			require.NoError(t, err)
			var doc unsmeta.Document
			require.NoError(t, json.Unmarshal(copied.Data, &doc))
			require.Equal(t, name+" (3)", doc.Name)
			require.Equal(t, target.ID, doc.ParentID)
			// Root copies share names with Collections.
			_, err = s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: strings.ToUpper(name) + " (2)"})
			require.NoError(t, err)
			copied, err = s.Copy(ctx, source.ID, source.Revision, "", "", nil)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(copied.Data, &doc))
			require.Equal(t, name+" (4)", doc.Name)
		})
	}
}
