package repo_test

// 验证清单独立持久化、候选版本就绪后激活、草稿竞态与回滚边界。
import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func v2RequestManifest() streamexec.ProgramManifest {
	return streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "request", Kind: "request"}}}
}

func TestProgramV2ImplicitRequestOwnership(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	draft := v2RequestManifest()
	doc := `{"script":"ownership fixture"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "implicit ownership", Model: "program", Document: doc}, 0, draft)
	require.NoError(t, err)
	wrong, err := streamexec.WithImplicitRequests(draft, stream.ID+1, streamexec.DefaultProgramTimeoutMs)
	require.NoError(t, err)
	_, _, err = r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, wrong, []repo.StreamSourceSnapshot{})
	require.ErrorIs(t, err, repo.ErrInvalidArgument, "another Stream cannot reuse a generated request identity")
	correct, err := streamexec.WithImplicitRequests(draft, stream.ID, streamexec.DefaultProgramTimeoutMs)
	require.NoError(t, err)
	_, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, correct, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	_, err = version.ProgramV2Manifest()
	require.NoError(t, err)
	version.StreamID++
	_, err = version.ProgramV2Manifest()
	require.Error(t, err, "stored ownership is rechecked when reading the frozen release")
	version.StreamID--
	version.Document = `{"script":"ownership fixture","timeoutMs":2000}`
	_, err = version.ProgramV2Manifest()
	require.Error(t, err, "frozen contract timeout must match the owning release")
}

func TestProgramV2ExecutionPlanFrozenIdentity(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session"})
	manifest := v2RequestManifest()
	doc := `{"script":"publication fixture","extensions":{"exact":9007199254740993,"scaled":1e3,"zero":-0.00}}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "publication authority", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	_, _, err = r.StageProgramV2WithRequestPolicies(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{}, []repo.ProgramUNSBinding{},
		func(streamexec.ProgramManifest, []repo.Source, []repo.ProgramSourceContract, []repo.ProgramUNSBinding) ([]repo.ProgramRequestPolicy, error) {
			return nil, repo.ErrInvalidArgument
		})
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	unchanged, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	require.Equal(t, stream.Revision, unchanged.Revision)
	require.Zero(t, unchanged.PendingVersion)
	var candidateCount int64
	require.NoError(t, store.UnsDB().Model(&repo.StreamVersion{}).Where("stream_id=?", stream.ID).Count(&candidateCount).Error)
	require.Zero(t, candidateCount, "target compilation failure must roll back the entire publication")
	stream, candidate, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	plan, err := candidate.ProgramExecutionPlan()
	require.NoError(t, err)
	require.Equal(t, repo.ProgramAuthorityRef{Kind: "user", ID: 1}, plan.Authority)
	require.Len(t, plan.Digest, 64)
	stored, err := r.Version(ctx, stream.ID, candidate.ID)
	require.NoError(t, err)
	storedPlan, err := stored.ProgramExecutionPlan()
	require.NoError(t, err, "JSONB may change whitespace, object order and exponent notation")
	require.Equal(t, plan, storedPlan)
	// The readiness worker must preserve the original publisher, not replace it.
	stream, err = r.ActivateStagedProgramV2(context.Background(), stream.ID, stream.Revision, stored.ID)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*repo.StreamVersion){
		"version identity": func(v *repo.StreamVersion) { v.ID++ },
		"stream identity":  func(v *repo.StreamVersion) { v.StreamID++ },
		"script": func(v *repo.StreamVersion) {
			v.Document = strings.Replace(v.Document, "publication fixture", "other script", 1)
		},
		"exact number": func(v *repo.StreamVersion) {
			v.Document = strings.Replace(v.Document, "9007199254740993", "9007199254740992", 1)
		},
		"source revision": func(v *repo.StreamVersion) { v.SourceRevision++ },
		"source manifest": func(v *repo.StreamVersion) {
			v.SourceManifest = `[{"sourceId":1,"configRevision":2,"alias":"1","resources":[]}]`
		},
		"entry": func(v *repo.StreamVersion) {
			v.ProgramManifest = strings.Replace(v.ProgramManifest, `"request"`, `"changed"`, 1)
		},
		"authority": func(v *repo.StreamVersion) {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(v.ProgramManifest), &fields))
			changed := plan
			changed.Authority.ID++
			fields["_executionPlan"], err = json.Marshal(changed)
			require.NoError(t, err)
			raw, marshalErr := json.Marshal(fields)
			require.NoError(t, marshalErr)
			v.ProgramManifest = string(raw)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := stored
			mutate(&changed)
			_, err := changed.ProgramV2Manifest()
			require.ErrorIs(t, err, repo.ErrInvalidArgument, "every execution reader must reject altered publication contents")
		})
	}
	keyCtx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "apiKey", APIKeyID: 88})
	stream, rollback, err := r.StageProgramV2(keyCtx, stream.ID, stream.Revision, stored.ID, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	rollbackPlan, err := rollback.ProgramExecutionPlan()
	require.NoError(t, err)
	require.Equal(t, plan, rollbackPlan, "rollback never replaces the historical publisher with its caller")
	stream, next, err := r.StageProgramV2(keyCtx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	keyPlan, err := next.ProgramExecutionPlan()
	require.NoError(t, err)
	require.Equal(t, repo.ProgramAuthorityRef{Kind: "api_key", ID: 88}, keyPlan.Authority, "a Key must not become its owner")
	require.NotEqual(t, plan.Digest, keyPlan.Digest)
	for _, subject := range []contextx.Subject{
		{}, // Historical repository callers have no authenticated authority.
		{UserID: 1, AuthType: "mcpOAuth", MCPRestricted: true, MCPGrantID: "grant"},
		{AuthType: "fleetTask", FleetTask: &contextx.FleetTaskSubject{NodeID: "remote", GrantID: "grant"}},
		{UserID: 1, AuthType: "apiKey", APIKeyID: 89, APIKeyUsageType: repo.APIKeyUsageAppData, APIKeyAppID: "app"},
	} {
		caller := contextx.WithSubject(context.Background(), subject)
		var legacy repo.StreamVersion
		stream, legacy, err = r.StageProgramV2(caller, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
		require.NoError(t, err)
		_, err = legacy.ProgramExecutionPlan()
		require.ErrorIs(t, err, repo.ErrNotFound, "unrelated identities never acquire a local App delegation snapshot")
	}
	legacyID := stream.PendingVersion
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, legacyID)
	require.NoError(t, err)
	_, legacyRollback, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, legacyID, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	_, err = legacyRollback.ProgramExecutionPlan()
	require.ErrorIs(t, err, repo.ErrNotFound, "a local rollback cannot backfill authority into old releases")
	require.Error(t, store.UnsDB().Model(&repo.StreamVersion{}).Where("id=?", stored.ID).Update("program_manifest", next.ProgramManifest).Error,
		"the existing version projection must reject replacing publication provenance")
}

func TestProgramV2StagedActivationAndRollback(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	manifest := v2RequestManifest()
	doc := `{"script":"first"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "standalone", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	require.Equal(t, "draft", stream.Status)
	require.Zero(t, stream.SourceID)
	stored, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	require.NotEqual(t, "{}", stored.DraftManifest)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	require.Zero(t, stream.ActiveVersion)
	require.Equal(t, version.ID, stream.PendingVersion)
	require.Equal(t, "draft", stream.Status)
	_, _, err = r.StageProgramV2(ctx, stream.ID, stream.Revision, version.ID, doc, manifest, []repo.StreamSourceSnapshot{})
	require.ErrorIs(t, err, repo.ErrConflict) // never-activated candidate is not rollback history
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	require.Equal(t, "enabled", stream.Status)
	require.Equal(t, version.ID, stream.ActiveVersion)
	require.Zero(t, stream.PendingVersion)
	secondDoc := `{"script":"second"}`
	stream.Document = secondDoc
	stream, err = r.SaveProgramV2(ctx, stream, stream.Revision, manifest)
	require.NoError(t, err)
	require.Equal(t, version.ID, stream.ActiveVersion)
	stream, candidate, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, secondDoc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	require.Equal(t, version.ID, stream.ActiveVersion)
	stagedRevision := stream.Revision
	stream.Document = `{"script":"third"}`
	stream, err = r.SaveProgramV2(ctx, stream, stream.Revision, manifest)
	require.NoError(t, err)
	require.Zero(t, stream.PendingVersion)
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stagedRevision, candidate.ID)
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, rollback, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, version.ID, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	require.Equal(t, version.ID, rollback.ID)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, rollback.ID)
	require.NoError(t, err)
	require.Contains(t, stream.Document, "third") // rollback does not discard newer draft
	stream, pending, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, stream.Document, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	stagedRevision = stream.Revision
	stream, err = r.DisableStream(ctx, stream.ID, stream.Revision, true)
	require.NoError(t, err)
	require.Equal(t, "disabled", stream.Status)
	require.Zero(t, stream.PendingVersion)
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stagedRevision, pending.ID)
	require.Error(t, err, "a delayed deployment cannot reactivate a deleted Stream")
}

func TestProgramV2SourceManifestAndConfigRace(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	source, err := r.CreateSource(ctx, repo.Source{Name: "device", Protocol: "mqtt", Document: `{}`})
	require.NoError(t, err)
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState = "enabled"
		item.Health = "healthy"
		return nil
	})
	require.NoError(t, err)
	manifest := v2RequestManifest()
	id := strconv.FormatInt(source.ID, 10)
	manifest.Sources = []streamexec.ProgramSource{{ID: id, Resources: []string{"command"}}}
	doc := `{"script":"device"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "with-device", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	snapshot := []repo.StreamSourceSnapshot{{Alias: id, SourceID: source.ID, ConfigRevision: source.ConfigRevision, Resources: []string{"command"}}}
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, snapshot)
	require.NoError(t, err)
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error { item.ConfigRevision++; return nil })
	require.NoError(t, err)
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.ErrorIs(t, err, repo.ErrConflict)
	stored, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	require.Zero(t, stored.ActiveVersion)
	require.Equal(t, version.ID, stored.PendingVersion)
	_, _, err = r.StageProgramV2(ctx, stream.ID, stream.Revision, version.ID, doc, manifest, snapshot)
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, err = r.DisableStream(ctx, stream.ID, stream.Revision, false)
	require.NoError(t, err)
	require.Zero(t, stream.PendingVersion)
}

func TestProgramV2SourceCatalogFrozenAcrossPublication(t *testing.T) {
	for _, tc := range []struct{ protocol, endpoint, ref, catalog, from, to string }{
		{"modbus-tcp", "tcp://localhost:502", "modbus:value", `[{"id":"modbus:value","name":"Value","models":["publish","subscribe"],"metadata":{"unitId":7,"address":0,"quantity":2,"writable":true,"metadataVerifiedAt":1000}}]`, `"address":0`, `"address":1`},
		{"mqtt", "mqtt://localhost:1883", "mqtt:factory/value", `[{"id":"mqtt:factory/value","name":"Value","models":["publish","subscribe"],"metadata":{"topic":"factory/value","observed":false}}]`, `"topic":"factory/value"`, `"topic":"factory/other"`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			r := repo.NewSourceStreamRepo(store)
			ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session"})
			source, err := r.CreateSource(ctx, repo.Source{Name: "Device", Protocol: tc.protocol,
				Document: fmt.Sprintf(`{"name":"Device","description":"Display","protocol":%q,"endpoint":%q}`, tc.protocol, tc.endpoint)})
			require.NoError(t, err)
			catalog := tc.catalog
			require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", &catalog))
			id := strconv.FormatInt(source.ID, 10)
			manifest := v2RequestManifest()
			manifest.Sources = []streamexec.ProgramSource{{ID: id, Resources: []string{tc.ref}}}
			doc := `{"script":"frozen resource fixture"}`
			stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "frozen catalog", Model: "program", Document: doc}, 0, manifest)
			require.NoError(t, err)
			snapshots := []repo.StreamSourceSnapshot{{Alias: id, SourceID: source.ID, ConfigRevision: source.ConfigRevision, Resources: []string{tc.ref}}}
			stream, active, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, snapshots)
			require.NoError(t, err)
			stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, active.ID)
			require.NoError(t, err)
			active, err = r.Version(ctx, stream.ID, active.ID)
			require.NoError(t, err)
			plan, err := active.ProgramExecutionPlan()
			require.NoError(t, err)
			require.Len(t, plan.Sources, 1)
			require.Len(t, plan.Sources[0].Resources, 1)
			run := repo.StreamRun{ID: "frozen-catalog", StreamID: stream.ID, VersionID: active.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`}
			created, err := r.AdmitProgramV2Run(ctx, run, true)
			require.NoError(t, err)
			require.True(t, created)
			task := repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: active.ID, EntryID: "request", Model: "program", Output: `{"outputs":[]}`}
			stream, pending, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, snapshots)
			require.NoError(t, err)
			retargeted := strings.Replace(catalog, tc.from, tc.to, 1)
			require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", &retargeted))
			current, err := r.Source(ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, source.ConfigRevision, current.ConfigRevision, "discovery does not advance connection revision")
			require.ErrorIs(t, active.ValidateProgramExecutionSource(current), repo.ErrConflict)
			newRun := run
			newRun.ID = "changed-catalog"
			created, err = r.AdmitProgramV2Run(ctx, newRun, true)
			require.ErrorIs(t, err, repo.ErrConflict)
			require.False(t, created)
			_, err = r.PutProgramV2Task(ctx, task, run)
			require.ErrorIs(t, err, repo.ErrConflict, "even scripts without Source host calls must not commit outputs after retargeting")
			_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, pending.ID)
			require.ErrorIs(t, err, repo.ErrConflict)
			stored, err := r.Stream(ctx, stream.ID)
			require.NoError(t, err)
			require.Equal(t, active.ID, stored.ActiveVersion)
			require.Equal(t, pending.ID, stored.PendingVersion)
			// Restore physical identity while changing only display/discovery fields.
			display := strings.Replace(strings.Replace(catalog, `"Value"`, `"Renamed"`, 1), `"metadataVerifiedAt":1000`, `"metadataVerifiedAt":2000`, 1)
			display = strings.Replace(display, `"observed":false`, `"observed":true`, 1)
			require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", &display))
			current, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
				item.Name = "Renamed"
				item.Document = strings.Replace(strings.Replace(item.Document, "Device", "Renamed", 1), "Display", "New description", 1)
				return nil
			})
			require.NoError(t, err)
			require.NoError(t, active.ValidateProgramExecutionSource(current))
			created, err = r.AdmitProgramV2Run(ctx, newRun, true)
			require.NoError(t, err)
			require.True(t, created)
			_, err = r.PutProgramV2Task(ctx, task, run)
			require.NoError(t, err)
			_, err = r.ActivateStagedProgramV2(context.Background(), stream.ID, stream.Revision, pending.ID)
			require.NoError(t, err)
			frozen, err := r.Version(ctx, stream.ID, active.ID)
			require.NoError(t, err)
			after, err := frozen.ProgramExecutionPlan()
			require.NoError(t, err)
			require.Equal(t, plan, after, "current observations never rewrite the old release")
		})
	}
}

func TestProgramV2RejectsBadManifest(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	manifest := v2RequestManifest()
	manifest.Sources = []streamexec.ProgramSource{{ID: "1.2", Resources: []string{}}}
	_, err := r.SaveProgramV2(context.Background(), repo.Stream{Name: "invalid", Model: "program", Document: `{"script":"x"}`}, 0, manifest)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	manifest = v2RequestManifest()
	manifest.Entries = append(manifest.Entries, manifest.Entries[0])
	_, err = r.SaveProgramV2(context.Background(), repo.Stream{Name: "invalid", Model: "program", Document: `{"script":"x"}`}, 0, manifest)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
}

func TestProgramV2DependencyUnionAndCandidateCancellation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	manifest := v2RequestManifest()
	snapshots := []repo.StreamSourceSnapshot{}
	ids := []int64{}
	for _, name := range []string{"first", "second"} {
		source, err := r.CreateSource(ctx, repo.Source{Name: name, Protocol: "mqtt", Document: `{}`})
		require.NoError(t, err)
		source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
			item.DesiredState = "enabled"
			item.Health = "healthy"
			return nil
		})
		require.NoError(t, err)
		id := strconv.FormatInt(source.ID, 10)
		ids = append(ids, source.ID)
		manifest.Sources = append(manifest.Sources, streamexec.ProgramSource{ID: id, Resources: []string{"value"}})
		snapshots = append(snapshots, repo.StreamSourceSnapshot{Alias: id, SourceID: source.ID, ConfigRevision: source.ConfigRevision, Resources: []string{"value"}})
	}
	doc := `{"script":"two"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "dependencies", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, old, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, snapshots)
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, old.ID)
	require.NoError(t, err)
	empty := v2RequestManifest()
	stream.Document = `{"script":"zero"}`
	stream, err = r.SaveProgramV2(ctx, stream, stream.Revision, empty)
	require.NoError(t, err)
	for _, id := range ids {
		dependencies, _, err := r.SourceDependencies(ctx, id)
		require.NoError(t, err)
		require.Len(t, dependencies, 1)
	}
	stream, zero, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, stream.Document, empty, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, zero.ID)
	require.NoError(t, err)
	for _, id := range ids {
		dependencies, _, err := r.SourceDependencies(ctx, id)
		require.NoError(t, err)
		require.Empty(t, dependencies)
	}
	stream, _, err = r.StageProgramV2(ctx, stream.ID, stream.Revision, old.ID, doc, manifest, snapshots)
	require.NoError(t, err)
	for _, id := range ids {
		dependencies, _, err := r.SourceDependencies(ctx, id)
		require.NoError(t, err)
		require.Len(t, dependencies, 1)
	}
	stream, err = r.DisableStream(ctx, stream.ID, stream.Revision, false)
	require.NoError(t, err)
	require.Zero(t, stream.PendingVersion)
	for _, id := range ids {
		dependencies, _, err := r.SourceDependencies(ctx, id)
		require.NoError(t, err)
		require.Empty(t, dependencies)
	}
}
