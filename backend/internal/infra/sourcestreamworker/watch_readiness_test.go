package sourcestreamworker

// 测试真实运行观察的范围匹配、代际隔离、停止与失败；不修改部署配置。
import (
	"context"
	"sync"
	"testing"

	"backend/internal/domain/sourcestream"
	"github.com/stretchr/testify/require"
)

func TestWatchReadinessScopeAndAttemptIsolation(t *testing.T) {
	var registry watchReadiness
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	query := sourcestream.WatchQuery{SourceID: 10, ConfigRevision: 3, ResourceRefs: []string{"mqtt:a/+"}, QoS: 1}
	require.Equal(t, "starting", registry.state(query))
	spec := sourceWatchSpec{Revision: 3, Refs: []string{"mqtt:a/+", "mqtt:b/#"}, QoS: 1}
	observe, finish := registry.begin(ctx, 10, spec)
	require.Equal(t, "starting", registry.state(query))
	observe(true)
	require.Equal(t, "ready", registry.state(query))
	other := query
	other.ResourceRefs = []string{"mqtt:b/#"}
	require.Equal(t, "ready", registry.state(other), "two Streams share the confirmed union scope")
	other.ResourceRefs = []string{"mqtt:c/#"}
	require.Equal(t, "starting", registry.state(other))
	other = query
	other.ConfigRevision++
	require.Equal(t, "starting", registry.state(other))
	other = query
	other.QoS++
	require.Equal(t, "starting", registry.state(other))
	other = query
	other.SourceID++
	require.Equal(t, "starting", registry.state(other))
	observe(false)
	require.Equal(t, "starting", registry.state(query), "disconnect immediately invalidates readiness")
	finish("source-watch-failed")
	observe(true)
	require.Equal(t, "source-watch-failed", registry.state(query), "late callbacks cannot revive a completed attempt")
	next, complete := registry.begin(ctx, 10, spec)
	observe(true)
	finish("source-watch-failed")
	require.Equal(t, "starting", registry.state(query), "old attempt cannot change replacement state")
	next(true)
	require.Equal(t, "ready", registry.state(query))
	cancel()
	require.Equal(t, "starting", registry.state(query), "ownership loss hides readiness even before cleanup")
	complete("source-watch-failed")
	require.Empty(t, registry.entries)
}

func TestWatchReadinessConcurrentObservation(t *testing.T) {
	var registry watchReadiness
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	query := sourcestream.WatchQuery{SourceID: 1, ConfigRevision: 1, ResourceRefs: []string{"mqtt:a"}}
	observe, finish := registry.begin(ctx, 1, sourceWatchSpec{Revision: 1, Refs: query.ResourceRefs})
	var jobs sync.WaitGroup
	for i := 0; i < 20; i++ {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			for j := 0; j < 100; j++ {
				observe(j%2 == 0)
				_ = registry.state(query)
			}
		}()
	}
	jobs.Wait()
	finish("source-watch-failed")
	require.Equal(t, "source-watch-failed", registry.state(query))
}

func TestWatchReadinessIndependentCollectionScopes(t *testing.T) {
	var registry watchReadiness
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, finishFirst := registry.beginScope(ctx, 7, "collection:one", sourceWatchSpec{Revision: 2, Refs: []string{"opcua:one"}, QoS: 1})
	second, finishSecond := registry.beginScope(ctx, 7, "collection:two", sourceWatchSpec{Revision: 2, Refs: []string{"opcua:two"}, QoS: 1})
	query := sourcestream.WatchQuery{SourceID: 7, ConfigRevision: 2, ResourceRefs: []string{"opcua:one", "opcua:two"}, QoS: 1}
	first(true)
	require.Equal(t, "starting", registry.state(query))
	second(true)
	require.Equal(t, "ready", registry.state(query))
	finishFirst("source-watch-failed")
	require.Equal(t, "source-watch-failed", registry.state(query))
	query.ResourceRefs = []string{"opcua:two"}
	require.Equal(t, "ready", registry.state(query), "one failed scope does not stop another")
	replacement, _ := registry.beginScope(ctx, 7, "collection:one", sourceWatchSpec{Revision: 2, Refs: []string{"opcua:one"}, QoS: 1})
	first(true)
	finishFirst("source-watch-failed")
	replacement(true)
	query.ResourceRefs = []string{"opcua:one", "opcua:two"}
	require.Equal(t, "ready", registry.state(query), "old callback cannot replace new owner")
	cancel()
	finishSecond("source-watch-failed")
	require.Equal(t, "starting", registry.state(query))
}

func TestWatchReadinessSeparatesAcquisitionParameters(t *testing.T) {
	var registry watchReadiness
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	query := sourcestream.WatchQuery{SourceID: 7, ConfigRevision: 2, ResourceRefs: []string{"modbus:temperature"}, AcquisitionKey: `{"pollIntervalMs":1000}`}
	fast, _ := registry.beginScope(ctx, 7, "fast", sourceWatchSpec{Revision: 2, Refs: query.ResourceRefs, AcquisitionKey: query.AcquisitionKey})
	fast(true)
	require.Equal(t, "ready", registry.state(query))
	query.AcquisitionKey = `{"pollIntervalMs":5000}`
	require.Equal(t, "starting", registry.state(query))
	slow, finish := registry.beginScope(ctx, 7, "slow", sourceWatchSpec{Revision: 2, Refs: query.ResourceRefs, AcquisitionKey: query.AcquisitionKey})
	slow(true)
	require.Equal(t, "ready", registry.state(query))
	finish("source-watch-failed")
	require.Equal(t, "source-watch-failed", registry.state(query))
	query.AcquisitionKey = `{"pollIntervalMs":1000}`
	require.Equal(t, "ready", registry.state(query))
}
