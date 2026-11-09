package repo_test

// 验证真实 PostgreSQL 中的版本隔离、乐观锁、Source 一对多和历史保留。
import (
	"context"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSourceStreamPersistence(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(ctx, repo.Source{Name: "隔离连接", Protocol: "mqtt", Document: `{"endpoint":"mqtt://broker:1883","extensions":{"vendor":42}}`})
	require.NoError(t, err)
	require.EqualValues(t, 1, source.Revision)
	require.Equal(t, "unverified", source.Health)
	require.Equal(t, "enabled", source.DesiredState)
	_, err = r.ChangeSource(ctx, source.ID, 9, func(*gorm.DB, *repo.Source) error { t.Fatal("stale callback executed"); return nil })
	require.ErrorIs(t, err, repo.ErrConflict)
	source, err = r.ChangeSource(ctx, source.ID, 1, func(tx *gorm.DB, s *repo.Source) error { s.DesiredState = "enabled"; return nil })
	require.NoError(t, err)
	require.ErrorIs(t, r.ObserveSource(ctx, source.ID, 1, "healthy", "", nil), repo.ErrConflict)
	resources := `[{"id":"temperature","models":["publish"]}]`
	require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", &resources))
	sources, total, err := r.ListSources(ctx, repo.SourceFilter{Protocol: "mqtt", Health: "healthy", Search: "隔离"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, sources, 1)
	_, err = r.Source(ctx, -1)
	require.ErrorIs(t, err, repo.ErrNotFound)
	require.True(t, repo.IsSourceStreamMissing(err))

	stream, err := r.CreateStream(ctx, repo.Stream{SourceID: source.ID, Name: "温度", Model: "publish", Document: `{"version":1}`}, []int64{101, 102})
	require.NoError(t, err)
	other, err := r.CreateStream(ctx, repo.Stream{SourceID: source.ID, Name: "压力", Model: "subscribe", Document: `{"version":1}`}, []int64{103})
	require.NoError(t, err)
	streams, total, err := r.ListStreams(ctx, repo.StreamFilter{SourceID: source.ID})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, streams, 2)
	_, _, err = r.ListStreams(ctx, repo.StreamFilter{SourceID: source.ID, TopicID: 101, Model: "publish", Status: "draft", Search: "温"})
	require.NoError(t, err)
	stream, err = r.ActivateStream(ctx, stream.ID, stream.Revision, source.ConfigRevision, 0, stream.Document, []int64{101, 102})
	require.NoError(t, err)
	firstVersion := stream.ActiveVersion
	require.Positive(t, firstVersion)
	require.Equal(t, "enabled", stream.Status)
	_, err = r.DisableStream(ctx, stream.ID, stream.Revision+1, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = r.SaveStream(ctx, stream, 1, []int64{101})
	require.ErrorIs(t, err, repo.ErrConflict)
	stream.Document = `{"version":2}`
	stream, err = r.SaveStream(ctx, stream, stream.Revision, []int64{101, 102})
	require.NoError(t, err)
	version, err := r.Version(ctx, stream.ID, firstVersion)
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1}`, version.Document)
	require.Equal(t, firstVersion, stream.ActiveVersion, "saving a draft must preserve active snapshot")
	_, err = r.ActivateStream(ctx, stream.ID, stream.Revision, source.ConfigRevision+1, 0, stream.Document, []int64{101})
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, err = r.ActivateStream(ctx, stream.ID, stream.Revision, source.ConfigRevision, 0, stream.Document, []int64{101, 102})
	require.NoError(t, err)
	require.NotEqual(t, firstVersion, stream.ActiveVersion)
	stream, err = r.ActivateStream(ctx, stream.ID, stream.Revision, source.ConfigRevision, firstVersion, version.Document, []int64{101, 102})
	require.NoError(t, err)
	require.Equal(t, firstVersion, stream.ActiveVersion)
	versions, total, err := r.Versions(ctx, stream.ID, 1, 2000)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, versions, 2)
	_, err = r.Version(ctx, other.ID, firstVersion)
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = r.DisableStream(ctx, stream.ID, 1, false)
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, err = r.DisableStream(ctx, stream.ID, stream.Revision, true)
	require.NoError(t, err)
	require.Positive(t, stream.DeletedAt)
	_, err = r.Stream(ctx, stream.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = r.Version(ctx, stream.ID, firstVersion)
	require.NoError(t, err, "soft delete keeps immutable versions")

	credential := repo.SourceCredential{ID: uuid.NewString(), SourceID: source.ID, Ciphertext: []byte("ciphertext"), UpdatedAt: time.Now().UnixMilli()}
	require.NoError(t, r.SaveCredential(ctx, credential))
	credential.Ciphertext = []byte("rotated")
	require.NoError(t, r.SaveCredential(ctx, credential))
	loaded, err := r.Credential(ctx, source.ID, credential.ID)
	require.NoError(t, err)
	require.Equal(t, credential.Ciphertext, loaded.Ciphertext)
	_, err = r.Credential(ctx, source.ID+1, credential.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	run := repo.StreamRun{ID: uuid.NewString(), SourceID: source.ID, StreamID: stream.ID, VersionID: firstVersion, Model: "publish", Status: "succeeded", Summary: `{"targets":[101,102]}`, CreatedAt: time.Now().UnixMilli()}
	require.NoError(t, r.CreateRun(ctx, run))
	loadedRun, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.JSONEq(t, run.Summary, loadedRun.Summary)
	runs, total, err := r.Runs(ctx, repo.StreamRunFilter{SourceID: source.ID, StreamID: stream.ID, TopicID: 101, Model: "publish", Status: "succeeded"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, runs, 1)
	run.ID = uuid.NewString()
	run.Summary = "invalid"
	require.ErrorIs(t, r.CreateRun(ctx, run), repo.ErrInvalidArgument)
}
