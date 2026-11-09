package unsmeta_test

import (
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCloudNamespaceCollectionLifecycle(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	s := unsmeta.New()
	root, err := s.Save(ctx, "root", "folder", 0, unsmeta.Document{Name: "Workshop"})
	require.NoError(t, err)
	schema := &unsmeta.Schema{Type: "object"}
	created, err := s.CreateCollection(ctx, root.ID, unsmeta.Document{Name: "Machines", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	require.NotContains(t, created, "topic")
	c := created["collection"].(repo.UnsMeta)
	var doc unsmeta.Document
	require.NoError(t, json.Unmarshal(c.Data, &doc))
	require.Equal(t, root.ID, doc.ParentID)
	_, err = s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: "machines", ParentID: root.ID})
	require.ErrorIs(t, err, repo.ErrDuplicate)
	_, err = s.Save(ctx, "", "collection", 0, unsmeta.Document{Name: "Machines", ParentID: c.ID, RecordSchema: schema})
	require.Error(t, err)
	older, err := s.Save(ctx, "older", "collection", 0, unsmeta.Document{Name: "Older", ParentID: root.ID, RecordSchema: schema})
	require.NoError(t, err)
	olderView, err := s.Save(ctx, "older-view", "topic", 0, unsmeta.Document{Name: "Older reference", ParentID: root.ID, ViewMode: "records", CollectionID: older.ID})
	require.NoError(t, err)
	older, err = s.State(ctx, older.ID, older.Revision, false, "collection")
	require.NoError(t, err)
	_, err = s.SaveRecord(ctx, c.ID, "machine", 0, map[string]any{"value": 42})
	require.NoError(t, err)
	_, err = s.AppendSamples(ctx, c.ID, []unsmeta.SampleInput{{ID: "sample", RecordID: "machine", Time: time.Now().UnixMilli(), Data: map[string]any{"value": 43}}})
	require.NoError(t, err)
	view, err := s.Save(ctx, "view", "topic", 0, unsmeta.Document{Name: "Outside", ViewMode: "records", CollectionID: c.ID})
	require.NoError(t, err)
	root, err = s.State(ctx, root.ID, root.Revision, false, "directory")
	require.NoError(t, err)
	recycled, err := repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, true, "collection")
	require.NoError(t, err)
	require.NotZero(t, recycled.DeletedAt)
	require.NoError(t, json.Unmarshal(recycled.Data, &doc))
	require.Equal(t, root.ID, doc.DirectoryTrashID)
	rows, err := s.Records(ctx, c.ID, "", 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows["total"])
	_, err = s.SaveRecord(ctx, c.ID, "blocked", 0, map[string]any{})
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = s.AppendSamples(ctx, c.ID, []unsmeta.SampleInput{{ID: "blocked", RecordID: "machine", Time: time.Now().UnixMilli(), Data: map[string]any{}}})
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, view.ID, false, "topic")
	require.NoError(t, err)
	_, err = s.State(ctx, c.ID, recycled.Revision, true, "collection")
	require.ErrorIs(t, err, unsmeta.ErrTrashDirectory)
	root, err = s.State(ctx, root.ID, root.Revision, true, "directory")
	require.NoError(t, err)
	current, err := repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, false, "collection")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(current.Data, new(unsmeta.Document)))
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, older.ID, false, "collection")
	require.ErrorIs(t, err, repo.ErrNotFound, "earlier independent recycle stays deleted")
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, olderView.ID, false, "topic")
	require.NoError(t, err, "existing Reference restores while its independently recycled Collection stays readable")
	_, err = s.Save(ctx, "new-reference", "topic", 0, unsmeta.Document{Name: "New reference", ViewMode: "records", CollectionID: older.ID})
	require.ErrorIs(t, err, repo.ErrNotFound, "new Reference still requires an active Collection")
	_, err = s.SaveRecord(ctx, c.ID, "allowed", 0, map[string]any{})
	require.NoError(t, err)
	// A retained name conflict makes the entire subtree restore fail atomically.
	root, err = s.State(ctx, root.ID, root.Revision, false, "directory")
	require.NoError(t, err)
	_, err = s.Save(ctx, "replacement", "folder", 0, unsmeta.Document{Name: "Workshop"})
	require.NoError(t, err)
	_, err = s.State(ctx, root.ID, root.Revision, true, "directory")
	require.ErrorIs(t, err, repo.ErrDuplicate)
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, false, "collection")
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestCloudDirectoryCopyReferencesCollections(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	s := unsmeta.New()
	root, err := s.Save(ctx, "root", "folder", 0, unsmeta.Document{Name: "Root"})
	require.NoError(t, err)
	c, err := s.Save(ctx, "collection", "collection", 0, unsmeta.Document{Name: "Data", ParentID: root.ID, RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	clone, err := s.Copy(ctx, root.ID, root.Revision, "", "", nil, "directory")
	require.NoError(t, err)
	rows, _, err := repo.NewUnsMetaRepo(ctx).List(ctx, "namespace", false, 1, 50)
	require.NoError(t, err)
	found := false
	for _, row := range rows {
		var d unsmeta.Document
		require.NoError(t, json.Unmarshal(row.Data, &d))
		if d.ParentID == clone.ID {
			found = true
			require.Equal(t, "topic", row.Kind)
			require.Equal(t, c.ID, d.CollectionID)
		}
	}
	require.True(t, found)
}

func TestRecycledCollectionKeepsCenterReadDestination(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	s := unsmeta.New()
	c, err := s.Save(ctx, "center-data", "collection", 0, unsmeta.Document{Name: "Center", RecordSchema: &unsmeta.Schema{Type: "object"}})
	require.NoError(t, err)
	require.NoError(t, store.UnsDB().Create(&repo.GlobalStoragePolicy{ID: "edge", Mode: "center", CenterID: "center", Revision: 1}).Error)
	require.NoError(t, store.UnsDB().Create(&repo.StorageCollectionMapping{CollectionID: c.ID, CenterID: "center", TargetCollectionID: "remote-data", SourceRevision: c.Revision}).Error)
	routed := false
	require.NoError(t, repo.ConfigureCollectionStorageReader(store.UnsDB(), func(_ context.Context, p repo.CollectionStoragePolicy, operation string, _ json.RawMessage) (json.RawMessage, error) {
		routed = true
		require.Equal(t, "remote-data", p.TargetCollectionID)
		require.Equal(t, "records", operation)
		return json.RawMessage(`{"list":[],"total":0,"page":1,"size":20}`), nil
	}))
	c, err = s.State(ctx, c.ID, c.Revision, false, "collection")
	require.NoError(t, err)
	_, err = s.Records(ctx, c.ID, "", 1, 20)
	require.NoError(t, err)
	require.True(t, routed)
	require.ErrorIs(t, repo.RequireLocalCollectionRead(ctx, c.ID), repo.ErrStorageOperation)
	require.ErrorIs(t, repo.RequireLocalCollectionPurge(ctx, c.ID), repo.ErrStorageOperation)
}
