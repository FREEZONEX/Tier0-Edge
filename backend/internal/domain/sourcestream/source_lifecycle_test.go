package sourcestream_test

// Metadata and legacy state do not alter connection identity; configuration changes and deletion fence execution.
import (
	"backend/internal/testkit/scripttest"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type physicalInventoryFixture struct{ protocolFixture }

func (*physicalInventoryFixture) Devices(context.Context, string) domain.PhysicalInventory {
	return domain.PhysicalInventory{Devices: []domain.PhysicalDevice{{Kind: "usb-hid", Address: "hid:test-device"}}}
}

func TestCloudSourceDeviceBindingsUseObservedHealth(t *testing.T) {
	_, _ = testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewSourceStreamRepo(ctx)
	document, err := json.Marshal(domain.SourceDefinition{Name: "Device binding", Protocol: domain.USBHID, Endpoint: "hid:test-device"})
	require.NoError(t, err)
	source, err := r.CreateSource(ctx, repo.Source{Name: "Device binding", Protocol: string(domain.USBHID), Document: string(document)})
	require.NoError(t, err)
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, row *repo.Source) error {
		row.DesiredState = "disabled"
		row.Health = "healthy"
		return nil
	})
	require.NoError(t, err)
	s := domain.New(&physicalInventoryFixture{}, nil, "")
	defer s.Close()
	inventory, err := s.Devices(ctx, "usb-hid")
	require.NoError(t, err)
	require.True(t, inventory.BindingsComplete)
	require.Len(t, inventory.Devices, 1)
	require.Equal(t, "connected", inventory.Devices[0].Presence)
	require.Equal(t, []domain.DeviceSource{{ID: strconv.FormatInt(source.ID, 10), Name: source.Name, Health: "healthy"}}, inventory.Devices[0].Sources)
	raw, err := json.Marshal(inventory)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "desiredState", "legacy storage state must not reappear in the public inventory")
	_, err = s.DeleteSource(ctx, source.ID, source.Revision, "")
	require.NoError(t, err)
	after, err := s.Devices(ctx, "usb-hid")
	require.NoError(t, err)
	require.Len(t, after.Devices, 1, "removing a binding does not disconnect physical hardware")
	require.Empty(t, after.Devices[0].Sources)
	require.NotEqual(t, inventory.Revision, after.Revision)
}

func TestCloudSourceMutationIgnoresHistoricalDependencies(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	s := domain.New(&protocolFixture{}, nil, "")
	def := domain.SourceDefinition{Name: "Cloud impact", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "impact"}}
	view, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	_, err = s.UpdateSource(ctx, id, 1, def, "stale")
	require.ErrorIs(t, err, repo.ErrConflict, "a supplied token is checked even without references")
	view, err = s.UpdateSource(ctx, id, 1, def, "")
	require.NoError(t, err, "unreferenced Source permits direct update")
	streamID := int64(900001)
	stream, _ := json.Marshal(map[string]any{"name": "retained", "model": "program", "revision": 2, "status": "disabled", "document": map[string]any{}, "activeVersion": 900003, "deletedAt": 1})
	require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "stream", ID: strconv.FormatInt(streamID, 10), Data: stream, Revision: 2, DeletedAt: 1}).Error)
	for _, versionID := range []int64{900002, 900003} {
		refs := []map[string]any{}
		if versionID == 900002 {
			refs = append(refs, map[string]any{"sourceId": id, "configRevision": 1, "alias": "old"})
		}
		plan, _ := json.Marshal(map[string]any{"streamId": streamID, "sourceManifest": refs, "programManifest": map[string]any{}, "document": map[string]any{}})
		require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "source_plan", ID: strconv.FormatInt(versionID, 10), Data: plan, Revision: 1}).Error)
	}
	impact, err := s.Impact(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 0, impact["count"], "archived Streams and historical releases do not block Source changes")
	def.MQTT.ClientID = "changed"
	view, err = s.UpdateSource(ctx, id, view["revision"].(int64), def, "")
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(ctx)
	for i, versionID := range []int64{900002, 900003} {
		release, err := r.ReleaseNumber(ctx, streamID, versionID)
		require.NoError(t, err)
		require.EqualValues(t, i+1, release)
	}
	// Historical releases remain queryable even when their parent is gone.
	require.NoError(t, store.UnsDB().Where("kind='stream' AND id=?", strconv.FormatInt(streamID, 10)).Delete(&repo.UnsMeta{}).Error)
	impact, err = s.Impact(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 0, impact["count"])
	_, err = s.DeleteSource(ctx, id, view["revision"].(int64), "")
	require.NoError(t, err)
}

func TestSourceMetadataAndStatePreserveExecutableVersions(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programContext()
	s := domain.New(&protocolFixture{resources: []domain.NativeResource{{ID: "mqtt:data/value", Models: []domain.AccessModel{domain.Publish}}}}, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer s.Close()
	require.NoError(t, s.StartProgramExecutions(ctx))
	s.SetAsyncReady(func() bool { return true })
	s.SetWatchState(func(domain.WatchQuery) string { return "ready" })
	r := repo.NewSourceStreamRepo(ctx)
	def := domain.SourceDefinition{Name: "生命周期连接", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "lifecycle"}}
	source, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	source, err = testkit.SetLegacySourceState(ctx, s, id, 1, "enabled")
	require.NoError(t, err)
	require.EqualValues(t, 1, source["configRevision"])
	_, err = s.Probe(ctx, id, nil, 500)
	require.NoError(t, err)
	_, err = s.Discover(ctx, id, 2, "")
	require.NoError(t, err)
	topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "生命周期目标", Type: 2, TopicType: 2}, nil, nil)
	require.NoError(t, err)
	document := domain.StreamDefinition{Name: "同步处理", Model: domain.ProgramAccess, Script: domain.ScriptDefinition{Code: fmt.Sprintf(`module.exports=api=>{
 api.sources.use({id:"%d",resources:["mqtt:data/value"]});
 api.stream.onRequest({id:"echo"},ctx=>ctx.input);
};`, id), TimeoutMs: 1000}}
	stream, err := s.SaveStream(ctx, 0, 0, document)
	require.NoError(t, err)
	streamID := stream["id"].(int64)
	stream, err = s.Deploy(ctx, streamID, 1, currentConfigRevision(t, ctx, s, id), 0)
	require.NoError(t, err)
	versionID := stream["activeVersion"].(int64)
	document.Name = "采集处理"
	document.Script.Code = fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:"%d",resources:["mqtt:data/value"]}); const output=api.uns.use({topicId:"%d"});
 api.stream.onMessage({id:"telemetry",source,resourceRefs:["mqtt:data/value"],qos:1},async ctx=>{await output.write(ctx.input);});
};`, id, topic.ID)
	publish, err := s.SaveStream(ctx, 0, 0, document)
	require.NoError(t, err)
	_, err = s.Deploy(ctx, publish["id"].(int64), 1, currentConfigRevision(t, ctx, s, id), 0)
	require.NoError(t, err)
	require.NoError(t, s.ActivateReadyProgramV2(ctx))

	changeState := func(state string) {
		t.Helper()
		revision := currentSourceRevision(t, ctx, s, id)
		action := "enable"
		if state == "disabled" {
			action = "disable"
		}
		_, err = s.SetSourceState(ctx, id, revision, state, "")
		require.Error(t, err, "referenced Source requires a state-specific impact proof")
		impact, e := s.InspectSourceImpact(ctx, id, revision, action, nil, 0, 100, "")
		require.NoError(t, e)
		wrongAction := "enable"
		if action == "enable" {
			wrongAction = "disable"
		}
		wrong, e := s.InspectSourceImpact(ctx, id, revision, wrongAction, nil, 0, 100, "")
		require.NoError(t, e)
		_, err = s.SetSourceState(ctx, id, revision, state, wrong["impactRevision"].(string))
		require.Error(t, err, "enable and disable proofs are not interchangeable")
		source, err = s.SetSourceState(ctx, id, revision, state, impact["impactRevision"].(string))
		require.NoError(t, err)
	}
	def.Name, def.Description = "已改名连接", "只修改展示信息"
	impact, err := s.Impact(ctx, id)
	require.NoError(t, err)
	previousRevision := currentSourceRevision(t, ctx, s, id)
	source, err = s.UpdateSource(ctx, id, previousRevision, def, "")
	require.NoError(t, err)
	require.EqualValues(t, previousRevision+1, source["revision"])
	require.EqualValues(t, 1, source["configRevision"])
	require.Equal(t, "healthy", source["health"])
	require.Len(t, source["resources"], 1)
	_, err = s.Invoke(ctx, streamID, versionID, "renamed", map[string]any{"value": 1}, 1000)
	require.NoError(t, err)
	plans, err := s.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	changeState("disabled")
	_, err = s.Invoke(ctx, streamID, versionID, "disabled", nil, 1000)
	require.Error(t, err, "disabled Source must reject even an existing frozen execution")
	plans, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, plans)
	view, err := s.Stream(ctx, streamID)
	require.NoError(t, err)
	require.Equal(t, "source-disabled", view["executionState"])
	changeState("enabled")
	require.EqualValues(t, 1, source["configRevision"])
	_, err = s.Invoke(ctx, streamID, versionID, "resumed", map[string]any{"value": 2}, 1000)
	require.NoError(t, err)
	plans, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	// An already admitted task remains valid across metadata edits, not across
	// connection changes. Verify the repository gate used by RAW/DELIVERY.
	pub := plans[1]
	if pub.Entry.Kind != "source-event" {
		pub = plans[0]
	}
	run := repo.StreamRun{ID: "metadata-task", SourceID: id, StreamID: pub.StreamID, VersionID: pub.VersionID, EntryID: pub.EntryID, Model: "program", Status: "processing", Summary: "{}"}
	_, err = r.AdmitProgramV2Run(ctx, run, false)
	require.NoError(t, err)
	task := repo.StreamTask{ID: run.ID, SourceID: id, StreamID: pub.StreamID, VersionID: pub.VersionID, EntryID: pub.EntryID, SourceRevision: pub.SourceRevision, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: 1}
	_, err = r.PutProgramV2Task(ctx, task, run)
	require.NoError(t, err)

	def.MQTT.ClientID = "changed-connection"
	impact, err = s.Impact(ctx, id)
	require.NoError(t, err)
	source, err = s.UpdateSource(ctx, id, currentSourceRevision(t, ctx, s, id), def, impact["impactRevision"].(string))
	require.NoError(t, err)
	require.EqualValues(t, 2, source["configRevision"])
	require.Equal(t, "unverified", source["health"])
	require.Empty(t, source["resources"])
	_, err = r.PutProgramV2Task(ctx, task, run)
	require.ErrorIs(t, err, repo.ErrConflict, "a connection change fences even a previously admitted task")
	view, err = s.Stream(ctx, streamID)
	require.NoError(t, err)
	require.Equal(t, "enabled", view["status"])
	require.Equal(t, "needs-redeploy", view["executionState"])
	_, err = s.Invoke(ctx, streamID, versionID, "stale", nil, 1000)
	require.ErrorIs(t, err, repo.ErrConflict)
	plans, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, plans)
	_, err = s.Probe(ctx, id, nil, 500)
	require.NoError(t, err)
	_, err = s.Discover(ctx, id, source["revision"].(int64), "")
	require.NoError(t, err)
	_, err = s.Deploy(ctx, streamID, stream["revision"].(int64), currentConfigRevision(t, ctx, s, id), versionID)
	require.ErrorIs(t, err, repo.ErrConflict, "rollback must not silently rebind an immutable version to a different connection")
	stream, err = s.Deploy(ctx, streamID, stream["revision"].(int64), currentConfigRevision(t, ctx, s, id), 0)
	require.NoError(t, err)
	_, err = s.Invoke(ctx, streamID, stream["activeVersion"].(int64), "new-plan", map[string]any{"value": 3}, 1000)
	require.NoError(t, err)
	impact, err = s.Impact(ctx, id)
	require.NoError(t, err)
	source, err = s.SaveCredential(ctx, id, currentSourceRevision(t, ctx, s, id), domain.Credential{Kind: "basic", Username: "test", Secret: "test-only"}, impact["impactRevision"].(string))
	require.NoError(t, err)
	require.EqualValues(t, 3, source["configRevision"])
	view, err = s.Stream(ctx, streamID)
	require.NoError(t, err)
	require.Equal(t, "needs-redeploy", view["executionState"])
}

func TestHistoricalProgramManifestDoesNotBreakManagementList(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	s := domain.New(nil, nil, "")
	valid := json.RawMessage(`{"sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`)
	legacy := json.RawMessage(`{"sdkVersion":"tier0-stream/v2","sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`)
	for i, mode := range []string{"draft", "active", "pending", "current"} {
		id := strconv.Itoa(9100 + i)
		draft := valid
		if mode == "draft" {
			draft = legacy
		}
		data := map[string]any{"name": mode, "model": "program", "revision": 1, "status": "disabled", "document": map[string]any{"name": mode, "script": "original bytes"}, "draftManifest": draft}
		if mode == "active" || mode == "pending" {
			data[mode+"Version"] = 9200 + i
			plan, err := json.Marshal(map[string]any{"streamId": 9100 + i, "sourceManifest": []any{}, "programManifest": legacy, "document": map[string]any{}})
			require.NoError(t, err)
			require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "source_plan", ID: strconv.Itoa(9200 + i), Data: plan, Revision: 1}).Error)
		}
		raw, err := json.Marshal(data)
		require.NoError(t, err)
		require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "stream", ID: id, Data: raw, Revision: 1}).Error)
	}
	page, err := s.Streams(ctx, repo.StreamFilter{Page: 1, Size: 50})
	require.NoError(t, err)
	require.EqualValues(t, 4, page["total"])
	for _, row := range page["list"].([]map[string]any) {
		if row["name"] == "current" {
			require.Equal(t, "disabled", row["executionState"])
		} else {
			require.Equal(t, "needs-redeploy", row["executionState"])
		}
	}
	// A missing deployment is a storage error, not permission to hide a broken query.
	require.NoError(t, store.UnsDB().Exec("DELETE FROM public.uns_meta WHERE kind='source_plan' AND id='9201'").Error)
	_, err = s.Stream(ctx, 9101)
	require.Error(t, err)
}

func TestDeleteEnabledSourceFencesConnectionAndRetainsHistory(t *testing.T) {
	_, _ = testkit.NewTestDB(t)
	ctx := context.Background()
	s := domain.New(&protocolFixture{}, nil, "")
	defer s.Close()
	def := domain.SourceDefinition{Name: "Delete running connection", Protocol: domain.MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "delete-running"}}
	source, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	id := source["id"].(int64)
	source, err = testkit.SetLegacySourceState(ctx, s, id, source["revision"].(int64), "enabled")
	require.NoError(t, err)
	impact, err := s.Impact(ctx, id)
	require.NoError(t, err)
	_, err = s.DeleteSource(ctx, id, source["revision"].(int64)+1, impact["impactRevision"].(string))
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = s.DeleteSource(ctx, id, source["revision"].(int64), "stale")
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = s.DeleteSource(ctx, id, source["revision"].(int64), impact["impactRevision"].(string))
	require.NoError(t, err)
	_, err = repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = s.Probe(ctx, id, nil, 500)
	require.Error(t, err, "deleted connection cannot be probed or reopened")
}
