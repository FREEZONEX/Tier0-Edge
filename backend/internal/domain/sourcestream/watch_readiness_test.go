package sourcestream

// 测试就绪查询固定生效版本，不从当前草稿推断订阅已建立。
import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/repo"
	"backend/internal/streamexec"

	"github.com/stretchr/testify/require"
)

func TestWatchQueryUsesActiveSnapshotAndTrigger(t *testing.T) {
	definition := streamDefinition()
	raw, err := json.Marshal(definition)
	require.NoError(t, err)
	manifest := streamexec.ProgramManifest{
		Sources: []streamexec.ProgramSource{{ID: "7", Resources: []string{"mqtt:a/+", "mqtt:b/#"}}},
		Topics:  []streamexec.ProgramTopic{},
		Entries: []streamexec.ProgramEntry{{ID: "watch", Kind: "source-event", SourceID: "7", ResourceRefs: []string{"mqtt:a/+", "mqtt:b/#"}, QoS: 2}},
	}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	version := repo.StreamVersion{Document: string(raw), ProgramManifest: string(encoded), SourceRevision: 99,
		SourceManifest: `[{"alias":"7","sourceId":7,"configRevision":3,"resources":["mqtt:a/+","mqtt:b/#"]}]`}
	queries, err := ProgramV2WatchQueries(version)
	require.NoError(t, err)
	require.Equal(t, []WatchQuery{{SourceID: 7, ConfigRevision: 3, ResourceRefs: manifest.Entries[0].ResourceRefs, QoS: 2}}, queries,
		"readiness uses the frozen current-program Source snapshot, not mutable draft metadata")
	missingSnapshot := version
	missingSnapshot.SourceManifest = `[]`
	_, err = ProgramV2WatchQueries(missingSnapshot)
	require.Error(t, err)
	for _, entry := range []streamexec.ProgramEntry{
		{ID: "request", Kind: "request"}, {ID: "timer", Kind: "interval", IntervalMs: 1000},
	} {
		manifest.Entries = []streamexec.ProgramEntry{entry}
		encoded, err = json.Marshal(manifest)
		require.NoError(t, err)
		version.ProgramManifest = string(encoded)
		queries, err = ProgramV2WatchQueries(version)
		require.NoError(t, err)
		require.Empty(t, queries, "non-Source entries do not create protocol watches")
	}
	for _, raw := range []string{
		`{"model":"publish","sourceId":7,"nativeResourceRef":"mqtt:a/+","script":{"code":"return input;","timeoutMs":1000}}`,
		`{"name":"old","program":{"sources":[],"trigger":{"kind":"source"}},"script":"x"}`,
		`{`,
	} {
		_, _, err := watchQuery(repo.StreamVersion{Document: raw})
		require.Error(t, err, "legacy watch documents cannot enter a second execution model")
	}
}

func TestWatchObserverDoesNotReportCanceledAttemptReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	states := []bool{}
	ctx = WithWatchObserver(ctx, func(ready bool) { states = append(states, ready) })
	ReportWatchReady(ctx, true)
	cancel()
	ReportWatchReady(ctx, true)
	ReportWatchReady(ctx, false)
	require.Equal(t, []bool{true, false, false}, states)
	ReportWatchReady(context.Background(), true)
	ReportWatchReady(WithWatchObserver(context.Background(), nil), true)
}
