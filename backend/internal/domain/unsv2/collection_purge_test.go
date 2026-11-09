package unsv2

import (
	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"errors"
	assert "github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNamespacePurgeDurableLifecycle(t *testing.T) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	actor := contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.delete": {}}}
	ctx := contextx.WithSubject(context.Background(), actor)
	m := unsmeta.New()
	s := New(m, func(context.Context, contextx.Subject) (contextx.Subject, error) { return actor, nil })
	root, err := m.Save(ctx, "root", "folder", 0, unsmeta.Document{Name: "Root"})
	assert.NoError(t, err)
	c, err := m.Save(ctx, "collection", "collection", 0, unsmeta.Document{Name: "Data", ParentID: root.ID, RecordSchema: &unsmeta.Schema{Type: "object"}, SampleSchema: &unsmeta.Schema{Type: "object"}})
	assert.NoError(t, err)
	external, err := m.Save(ctx, "external", "collection", 0, unsmeta.Document{Name: "External", RecordSchema: &unsmeta.Schema{Type: "object"}})
	assert.NoError(t, err)
	_, err = m.Save(ctx, "reference", "topic", 0, unsmeta.Document{Name: "Reference", ParentID: root.ID, ViewMode: "records", CollectionID: external.ID})
	assert.NoError(t, err)
	_, err = m.SaveRecord(ctx, c.ID, "machine", 0, map[string]any{"value": 1})
	assert.NoError(t, err)
	_, err = m.AppendSamples(ctx, c.ID, []unsmeta.SampleInput{{ID: "sample", RecordID: "machine", Time: time.Now().UnixMilli(), Data: map[string]any{"value": 2}}})
	assert.NoError(t, err)
	// Reject active targets before accepting a job or fencing any subtree member.
	request := collectionPurgeRequest{Kind: "directory", IDs: []string{root.ID}, IdempotencyKey: "purge-root"}
	raw, _ := json.Marshal(request)
	_, err = s.Execute(ctx, "metadataPurge", raw)
	assert.Error(t, err)
	root, err = m.State(ctx, root.ID, root.Revision, false, "directory")
	assert.NoError(t, err)
	request.DryRun = true
	raw, _ = json.Marshal(request)
	preview, err := s.Execute(ctx, "metadataPurge", raw)
	assert.NoError(t, err)
	assert.Equal(t, "validated", preview.(Outcome).Status)
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, true, "collection")
	assert.NoError(t, err)
	assert.NotContains(t, string(row.Data), "purgeJobId")
	request.DryRun = false
	raw, _ = json.Marshal(request)
	value, err := s.Execute(ctx, "metadataPurge", raw)
	assert.NoError(t, err)
	accepted := value.(Outcome)
	assert.Equal(t, "accepted", accepted.Status)
	assert.NotEmpty(t, accepted.JobID)
	_, err = m.State(ctx, root.ID, root.Revision+1, true, "directory")
	assert.ErrorIs(t, err, unsmeta.ErrPurgeChanged)
	replay, err := s.Execute(ctx, "metadataPurge", raw)
	assert.NoError(t, err)
	assert.Equal(t, accepted.JobID, replay.(Outcome).JobID)
	assert.NoError(t, s.workJob(ctx, accepted.JobID))
	task, err := repo.NewUnsMetaRepo(ctx).Get(ctx, accepted.JobID, true, "task")
	assert.NoError(t, err)
	var job jobData
	assert.NoError(t, json.Unmarshal(task.Data, &job))
	assert.Equal(t, "succeeded", job.Status)
	assert.Equal(t, "committed", job.Result.Status)
	assert.Equal(t, 3, job.Result.Summary.Succeeded)
	for _, target := range []struct{ kind, id string }{{"directory", root.ID}, {"collection", c.ID}, {"topic", "reference"}} {
		_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, target.id, true, target.kind)
		assert.ErrorIs(t, err, repo.ErrNotFound)
	}
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, external.ID, false, "collection")
	assert.NoError(t, err, "deleting a reference must not delete its target")
	var count int64
	assert.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("collection_id=?", c.ID).Count(&count).Error)
	assert.Zero(t, count)
	assert.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Where("collection_id=?", c.ID).Count(&count).Error)
	assert.Zero(t, count)
	// Reusing an ID after completion must not expose the new generation to a replay.
	_, err = m.Save(ctx, c.ID, "collection", 0, unsmeta.Document{Name: "Replacement", RecordSchema: &unsmeta.Schema{Type: "object"}})
	assert.NoError(t, err)
	_, err = s.Execute(ctx, "metadataPurge", raw)
	assert.NoError(t, err)
	assert.NoError(t, s.workJob(ctx, accepted.JobID))
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, false, "collection")
	assert.NoError(t, err)
}

func TestNamespacePurgeRevokedAuthorizationAndResume(t *testing.T) {
	nativeEdgeTestProduct(t)
	testkit.NewTestDB(t)
	actor := contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.delete": {}}}
	ctx := contextx.WithSubject(context.Background(), actor)
	m := unsmeta.New()
	authorized := true
	s := New(m, func(context.Context, contextx.Subject) (contextx.Subject, error) {
		if !authorized {
			return actor, errors.New("revoked")
		}
		return actor, nil
	})
	c, err := m.Save(ctx, "collection", "collection", 0, unsmeta.Document{Name: "Data", RecordSchema: &unsmeta.Schema{Type: "object"}})
	assert.NoError(t, err)
	c, err = m.State(ctx, c.ID, c.Revision, false, "collection")
	assert.NoError(t, err)
	raw := []byte(`{"kind":"collection","ids":["collection"],"idempotencyKey":"resume"}`)
	result, err := s.Execute(ctx, "metadataPurge", raw)
	assert.NoError(t, err)
	id := result.(Outcome).JobID
	authorized = false
	assert.NoError(t, s.workJob(ctx, id))
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, true, "collection")
	assert.NoError(t, err)
	assert.NotEmpty(t, row.Data)
	task, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, true, "task")
	assert.NoError(t, err)
	var job jobData
	assert.NoError(t, json.Unmarshal(task.Data, &job))
	assert.Equal(t, "failed", job.Status)
	authorized = true
	_, err = s.Execute(ctx, "metadataPurge", raw)
	assert.NoError(t, err)
	assert.NoError(t, s.workJob(ctx, id))
	_, err = repo.NewUnsMetaRepo(ctx).Get(ctx, c.ID, true, "collection")
	assert.ErrorIs(t, err, repo.ErrNotFound)
}
