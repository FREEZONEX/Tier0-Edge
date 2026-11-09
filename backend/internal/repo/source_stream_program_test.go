package repo_test

// 验证零/多 Source 的真实 PG 持久化、不可变依赖快照和草稿/生效关系并集。
import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProgramPersistence_ZeroAndMultipleSources(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewSourceStreamRepo(store)
	zero := `{"name":"zero","sourceId":0,"model":"request-reply","program":{"sources":[],"trigger":{"kind":"manual"}}}`
	stream, err := r.SaveProgram(ctx, repo.Stream{Name: "zero", Model: "request-reply", Document: zero}, 0, nil)
	require.NoError(t, err)
	var isNull bool
	require.NoError(t, store.UnsDB().Raw("SELECT source_id IS NULL FROM public.program_stream_projection WHERE id=?", stream.ID).Scan(&isNull).Error)
	require.True(t, isNull, "no placeholder Source")
	stream, err = r.ActivateProgram(ctx, stream.ID, stream.Revision, 0, zero, nil, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	version, err := r.Version(ctx, stream.ID, stream.ActiveVersion)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, version.SourceManifest)
	_, err = r.ProgramVersionSources(ctx, version)
	require.NoError(t, err)
	run := repo.StreamRun{ID: "zero-run", StreamID: stream.ID, VersionID: version.ID, Model: "request-reply", Status: "processing", Summary: `{}`}
	created, err := r.AdmitProgramRun(ctx, run)
	require.NoError(t, err)
	require.True(t, created)
	created, err = r.AdmitProgramRun(ctx, run)
	require.NoError(t, err)
	require.False(t, created)
	asyncDoc := `{"sourceId":0,"model":"publish","program":{"sources":[],"trigger":{"kind":"interval","intervalMs":1000}}}`
	async, err := r.SaveProgram(ctx, repo.Stream{Name: "zero async", Model: "publish", Document: asyncDoc}, 0, nil)
	require.NoError(t, err)
	async, err = r.ActivateProgram(ctx, async.ID, async.Revision, 0, asyncDoc, nil, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	run.ID, run.Model, run.StreamID, run.VersionID = "zero-task", "publish", async.ID, async.ActiveVersion
	task, err := r.PutProgramTask(ctx, repo.StreamTask{ID: run.ID, StreamID: async.ID, VersionID: async.ActiveVersion, Model: "publish", Output: `{}`, ReceivedAt: 1}, run)
	require.NoError(t, err)
	require.Zero(t, task.SourceID)
	task, err = r.Task(ctx, task.ID)
	require.NoError(t, err)
	require.Zero(t, task.SourceID)
	loaded, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	require.Zero(t, loaded.SourceID)
	_, err = r.SaveProgram(ctx, repo.Stream{Name: "zero", Model: "request-reply", Document: zero}, 0, nil)
	require.Error(t, err, "NULL owner must not bypass name uniqueness")

	a, err := r.CreateSource(ctx, repo.Source{Name: "opc", Protocol: "opcua", Document: `{}`})
	require.NoError(t, err)
	b, err := r.CreateSource(ctx, repo.Source{Name: "modbus", Protocol: "modbus-tcp", Document: `{}`})
	require.NoError(t, err)
	for _, source := range []repo.Source{a, b} {
		_, err := r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, current *repo.Source) error {
			current.DesiredState, current.Health = "enabled", "healthy"
			return nil
		})
		require.NoError(t, err)
	}
	refs := []repo.StreamSourceSnapshot{{Alias: "opc", SourceID: a.ID, ConfigRevision: 1, ReadResources: []string{"opcua:temp"}}, {Alias: "modbus", SourceID: b.ID, ConfigRevision: 1, ReadResources: []string{"modbus:hr:1"}}}
	doc := func(sources []repo.StreamSourceSnapshot) string {
		raw, err := json.Marshal(map[string]any{"sourceId": 0, "model": "publish", "program": map[string]any{"sources": sources, "trigger": map[string]any{"kind": "interval", "intervalMs": 5000}}})
		require.NoError(t, err)
		return string(raw)
	}
	stream, err = r.SaveProgram(ctx, repo.Stream{Name: "multi", Model: "publish", Document: doc(refs)}, 0, []int64{101, 102})
	require.NoError(t, err)
	stream, err = r.ActivateProgram(ctx, stream.ID, stream.Revision, 0, stream.Document, []int64{101, 102}, refs)
	require.NoError(t, err)
	version, err = r.Version(ctx, stream.ID, stream.ActiveVersion)
	require.NoError(t, err)
	snapshots, err := version.Sources()
	require.NoError(t, err)
	require.Equal(t, refs, snapshots)
	stream.Document = doc(refs[:1])
	stream, err = r.SaveProgram(ctx, stream, stream.Revision, []int64{101})
	require.NoError(t, err)
	var index []repo.StreamSource
	require.NoError(t, store.CommonDB().Where("stream_id=?", stream.ID).Find(&index).Error)
	require.Len(t, index, 2, "saved draft cannot hide active dependencies")
	dependencies, dependencyTopics, err := r.SourceDependencies(ctx, b.ID)
	require.NoError(t, err)
	require.Len(t, dependencies, 1)
	require.Zero(t, dependencies[0].SourceID, "referenced Sources are dependencies, not ownership")
	require.Equal(t, "publish", dependencies[0].Model, "legacy discriminator must not become script")
	require.NotEmpty(t, dependencyTopics)
	listed, total, err := r.ListStreams(ctx, repo.StreamFilter{SourceID: b.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, listed, 1)
	require.Zero(t, listed[0].SourceID)
	_, err = r.ActivateProgram(ctx, stream.ID, stream.Revision, version.ID, version.Document, []int64{101, 102}, refs[:1])
	require.Error(t, err, "rollback cannot change frozen grants")
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=?", b.ID).Update("config_revision", 2).Error)
	_, err = r.ProgramVersionSources(ctx, version)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = r.ActivateProgram(ctx, stream.ID, stream.Revision, version.ID, version.Document, nil, refs)
	require.ErrorIs(t, err, repo.ErrConflict, "connection edit invalidates old snapshot")
}

func TestProgramSnapshots_InvalidManifests(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `broken`, `[{"alias":"x","sourceId":1,"configRevision":0}]`, `[{"alias":"x","sourceId":1,"configRevision":1},{"alias":"x","sourceId":2,"configRevision":1}]`} {
		_, err := (repo.StreamVersion{SourceManifest: raw}).Sources()
		require.Error(t, err)
	}
}
