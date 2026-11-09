package sourcestream

// PG验证固定版本多入口与候选边界；就绪函数只是观察契约，协议确认另由真实Broker验收。
import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/domain/dataingest"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestProgramV2PlansAllEntriesAndCandidateReadiness(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.source", "uns.read")
	programV2SourceFixture(t, store, 8101)
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8101").Update("name", "First scope fixture").Error)
	programV2SourceFixture(t, store, 8102)
	s := New(nil, programV2RealExecutor{}, "")
	s.SetAsyncReady(func() bool { return true })
	ready := map[int64]bool{}
	s.SetWatchState(func(query WatchQuery) string {
		if ready[query.SourceID] && query.ConfigRevision == 1 && len(query.ResourceRefs) == 1 {
			return "ready"
		}
		return "starting"
	})
	def := programV2Definition(`module.exports=api=>{
 const a=api.sources.use({id:"8101",resources:["opcua:ns=2;s=temp"]});
 const b=api.sources.use({id:"8102",resources:["opcua:ns=2;s=temp"]});
 api.stream.onSource({id:"a",source:a,resourceRefs:["opcua:ns=2;s=temp"]},ctx=>ctx.input);
 api.stream.onSource({id:"b",source:b,resourceRefs:["opcua:ns=2;s=temp"]},ctx=>ctx.input);
 api.stream.every({id:"tick",intervalMs:1000},()=>1);
 api.stream.onRequest({id:"request"},ctx=>ctx.input);
};`)
	created, err := s.saveProgramV2(ctx, 0, 0, def, programV2Raw(t, def))
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	item, err := r.Stream(ctx, created["id"].(int64))
	require.NoError(t, err)
	_, err = s.deployProgramV2(ctx, item, item.Revision, 0, def, item.Document)
	require.NoError(t, err)
	active, err := s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, active, "candidate never enters business dispatch")
	watch, err := s.WatchPlans(ctx)
	require.NoError(t, err)
	require.Len(t, watch, 4)
	for _, plan := range watch {
		require.True(t, plan.Candidate)
		require.False(t, plan.MatchesSourceInput(8101, "opcua:ns=2;s=temp"))
	}
	ready[8101] = true
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	active, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Empty(t, active, "one ready Source cannot activate a multi-Source version")
	ready[8102] = true
	require.NoError(t, s.ActivateReadyProgramV2(ctx))
	active, err = s.Plans(ctx)
	require.NoError(t, err)
	require.Len(t, active, 4)
	stored, err := r.Stream(ctx, item.ID)
	require.NoError(t, err)
	require.Zero(t, stored.PendingVersion)
	version, err := r.Version(ctx, item.ID, stored.ActiveVersion)
	require.NoError(t, err)
	require.Positive(t, version.ActivatedAt)
	for _, plan := range active {
		require.False(t, plan.Candidate)
		_, err := s.loadPlan(context.Background(), plan.ExecutionRef)
		require.NoError(t, err)
		wrong := plan.ExecutionRef
		wrong.EntryID = "missing"
		_, err = s.loadPlan(ctx, wrong)
		require.Error(t, err)
		if plan.Entry.Kind == "source-event" {
			require.True(t, plan.MatchesSourceInput(plan.SourceID, "opcua:ns=2;s=temp"))
			wrong = plan.ExecutionRef
			wrong.SourceRevision++
			_, err = s.loadPlan(ctx, wrong)
			require.Error(t, err)
		} else {
			require.Zero(t, plan.SourceID)
			require.False(t, plan.MatchesSourceInput(8101, "opcua:ns=2;s=temp"))
		}
	}
	require.NoError(t, store.CommonDB().Model(&repo.Source{}).Where("id=8102").Update("config_revision", 2).Error)
	_, err = s.loadPlan(ctx, active[0].ExecutionRef)
	require.ErrorIs(t, err, errInactivePlan, "changed snapshot invalidates the whole frozen program")
}

func TestProgramV2PlansRejectMalformedSnapshotsAndMQTTMatching(t *testing.T) {
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{{ID: "8101", Resources: []string{"mqtt:factory/+/telemetry"}}},
		Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "mqtt", Kind: "source-event", SourceID: "8101", ResourceRefs: []string{"mqtt:factory/+/telemetry"}, QoS: 1}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	snapshots, err := json.Marshal([]repo.StreamSourceSnapshot{{Alias: "8101", SourceID: 8101, ConfigRevision: 1, Resources: []string{"mqtt:factory/+/telemetry"}}})
	require.NoError(t, err)
	def := programV2Definition(`module.exports=api=>{const s=api.sources.use({id:"8101",resources:["mqtt:factory/+/telemetry"]});api.stream.onSource({id:"mqtt",source:s,resourceRefs:["mqtt:factory/+/telemetry"],qos:1},ctx=>ctx.input);};`)
	item := repo.StreamExecution{Stream: repo.Stream{ID: 101}, Version: repo.StreamVersion{ID: 102, StreamID: 101, ActivatedAt: 1, Document: programV2Raw(t, def), ProgramManifest: string(encoded), SourceManifest: string(snapshots)}}
	plans, err := programV2Plans(item, false)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.True(t, plans[0].MatchesSourceInput(8101, "mqtt:factory/press/telemetry"))
	require.False(t, plans[0].MatchesSourceInput(8101, "mqtt:factory/press/other"))
	require.False(t, plans[0].MatchesSourceInput(8102, "mqtt:factory/press/telemetry"))
	item.Version.ActivatedAt = 0
	_, err = programV2Plans(item, false)
	require.ErrorIs(t, err, errInactivePlan)
	_, err = programV2Plans(item, true)
	require.NoError(t, err)
	item.Version.SourceManifest = `[]`
	_, err = programV2Plans(item, true)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	legacyDownlink := ExecutionPlan{ExecutionRef: ExecutionRef{SourceID: 8101}, Definition: StreamDefinition{Model: Subscribe, NativeResourceRefs: []string{"mqtt:factory/press/telemetry"}}}
	require.False(t, legacyDownlink.MatchesSourceInput(8101, "mqtt:factory/press/telemetry"))
}

// 同一 UNS 事件冻结每个入口的独立身份；真实执行器和持久结果不会在重投时重新执行。
func TestProgramV2PlansMatchEveryUNSEntryAndPreserveLineage(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.read", "uns.write")
	topic, err := repo.NewUnsRepo(store).CreateUnsNode(ctx, repo.UnsNode{Name: "Program input", Type: 2, TopicType: 3,
		Schema: json.RawMessage(`{"fields":[{"name":"value","type":"double"}]}`)}, nil, nil)
	require.NoError(t, err)
	sink, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer sink.Close()
	s := New(nil, programV2RealExecutor{}, "")
	require.NoError(t, s.StartProgramExecutions(ctx))
	t.Cleanup(s.Close)
	s.SetAsyncReady(func() bool { return true })
	code := fmt.Sprintf(`module.exports=api=>{
 const topic=api.uns.use({topicId:%q});
 api.stream.onTopic({id:"first",topics:[topic]},ctx=>({value:ctx.input.value+1}));
 api.stream.onTopic({id:"second",topics:[topic]},ctx=>({value:ctx.input.value+2}));
};`, strconv.FormatInt(topic.ID, 10))
	def := programV2Definition(code)
	created, err := s.saveProgramV2(ctx, 0, 0, def, programV2Raw(t, def))
	require.NoError(t, err)
	r := repo.NewSourceStreamRepo(store)
	stream, err := r.Stream(ctx, created["id"].(int64))
	require.NoError(t, err)
	_, err = s.deployProgramV2(ctx, stream, stream.Revision, 0, def, stream.Document)
	require.NoError(t, err)
	stream, err = r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	event := dataingest.StreamEvent{MessageID: "multi-entry-event", TopicID: topic.ID, ReceivedAt: time.Now(), Data: map[string]any{"value": 7}}
	require.NoError(t, s.MatchProgramEvent(ctx, event, sink))
	for i, entry := range []string{"first", "second"} {
		id := repo.ProgramRunIdentity(event.MessageID, stream.ID, stream.ActiveVersion, entry)
		run, err := r.Run(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "succeeded", run.Status)
		require.Equal(t, entry, run.EntryID)
		task, err := r.Task(ctx, id)
		require.NoError(t, err)
		var result streamexec.ProgramResult
		require.NoError(t, json.Unmarshal([]byte(task.Output), &result))
		require.JSONEq(t, fmt.Sprintf(`{"value":%d}`, 8+i), string(result.Data))
	}
	require.NoError(t, s.MatchProgramEvent(ctx, event, sink))
	var count int64
	require.NoError(t, store.CommonDB().Model(&repo.StreamRun{}).Where("stream_id=?", stream.ID).Count(&count).Error)
	require.EqualValues(t, 2, count)
	event.MessageID = "feedback-event"
	event.Lineage = []int64{stream.ID}
	require.NoError(t, s.MatchProgramEvent(ctx, event, sink))
	require.NoError(t, store.CommonDB().Model(&repo.StreamRun{}).Where("stream_id=?", stream.ID).Count(&count).Error)
	require.EqualValues(t, 2, count, "feedback does not create another execution")
}
