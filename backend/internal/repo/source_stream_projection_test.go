package repo_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSourceStreamProjectionLocksUnderlyingMetadata(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(context.Background(), repo.Source{Name: "locked-source", Protocol: "mqtt", Document: `{}`})
	require.NoError(t, err)
	tx := store.UnsDB().Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	var projected repo.Source
	require.NoError(t, tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", source.ID).Take(&projected).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	called := false
	_, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error { called = true; item.Name = "after-lock"; return nil })
	require.Error(t, err, "view FOR UPDATE must lock the actual source metadata row")
	require.False(t, called)
	require.NoError(t, tx.Rollback().Error)
	updated, err := r.ChangeSource(context.Background(), source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error { item.Name = "after-lock"; return nil })
	require.NoError(t, err)
	require.Equal(t, source.ConfigRevision, updated.ConfigRevision)
	require.Equal(t, "after-lock", updated.Name)
	credential := repo.SourceCredential{ID: "locked-credential", SourceID: source.ID, Ciphertext: []byte{0, 255, 42}, UpdatedAt: time.Now().UnixMilli()}
	require.NoError(t, r.SaveCredential(context.Background(), credential))
	var privateProjection repo.SourceCredential
	require.NoError(t, store.UnsDB().Where("id=? AND source_id=?", credential.ID, source.ID).Take(&privateProjection).Error)
	require.Equal(t, credential.Ciphertext, privateProjection.Ciphertext)
	other, err := r.CreateSource(context.Background(), repo.Source{Name: "other-source", Protocol: "mqtt", Document: `{}`})
	require.NoError(t, err)
	rebound := credential
	rebound.SourceID = other.ID
	require.ErrorIs(t, r.SaveCredential(context.Background(), rebound), repo.ErrConflict, "credential identity cannot be rebound to another source")
	errors := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		go func(id string) {
			errors <- r.SaveCredential(context.Background(), repo.SourceCredential{ID: id, SourceID: source.ID, Ciphertext: []byte(id), UpdatedAt: 1})
		}(id)
	}
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		loaded, err := r.Credential(context.Background(), source.ID, id)
		require.NoError(t, err)
		require.Equal(t, []byte(id), loaded.Ciphertext, "concurrent rotations cannot replace the entire credentials map")
	}
}

func TestSourceStreamRunsTopicFilterCoversScriptAndProgram(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	script, err := r.CreateStream(ctx, repo.Stream{Name: "old-script", Model: "script", Document: `{"name":"old-script","model":"script","sources":[],"topicIds":[543]}`}, nil)
	require.NoError(t, err)
	manifest := v2RequestManifest()
	manifest.Topics = []streamexec.ProgramTopic{{TopicID: "543"}}
	program, err := r.SaveProgramV2(ctx, repo.Stream{Name: "new-program", Model: "program", Document: `{"script":"x"}`}, 0, manifest)
	require.NoError(t, err)
	other, err := r.SaveProgramV2(ctx, repo.Stream{Name: "other-program", Model: "program", Document: `{"script":"x"}`}, 0, v2RequestManifest())
	require.NoError(t, err)
	for _, item := range []repo.Stream{script, program, other} {
		require.NoError(t, r.CreateRun(ctx, repo.StreamRun{ID: "topic-run-" + strconv.FormatInt(item.ID, 10), StreamID: item.ID, Model: item.Model, Status: "succeeded", Summary: `{}`, CreatedAt: 1}))
	}
	runs, total, err := r.Runs(ctx, repo.StreamRunFilter{TopicID: 543})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, runs, 2)
	for _, run := range runs {
		require.NotEqual(t, other.ID, run.StreamID)
	}
}
