package dataingest

// service_test.go
// 测试目标：Service 生命周期、载荷发布/消费、最新值与历史查询入口及纯函数助手。
// 正常路径：New 默认参数与 writer 装配；Start/Close 启停；Consume 入队与最新值缓存；
//   LastPayload 从 sink 回读；EnsurePhysicalForNodes 建视图；flushLoop 停止时落库；
//   publishPayloadRecords 发事件流；OpenAPI 写入带时间戳分支。
// 异常情况：非法 sink DSN；未初始化 MQTT client / broker 未连接发布失败；队列满报错；
//   $ 前缀与未知 topic 跳过；nil receiver 全部安全；非 type2 节点跳过。
// 未覆盖：真实 MQTT broker 连接（connectMQTT/newMQTTClient/subscribeMQTTTopics*），
//   需要外部 broker，仅验证 publishOverride 注入路径。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/infra/eventstream"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMQTTSubscribeResult(t *testing.T) {
	require.NoError(t, validateMQTTSubscribeResult("Metric/#", map[string]byte{"Metric/#": 1}))
	require.ErrorContains(t, validateMQTTSubscribeResult("Metric/#", map[string]byte{}), "missing SUBACK")
	require.ErrorContains(t, validateMQTTSubscribeResult("Metric/#", map[string]byte{"Metric/#": 0x87}), "rejected")
}

type qualityObserverSpy struct {
	observed  []QualityObservation
	dropped   []QualityObservation
	persisted []QualityObservation
	effective map[int64]int64
}

func (s *qualityObserverSpy) Observe(_ context.Context, observations []QualityObservation) {
	s.observed = append(s.observed, observations...)
}

func (s *qualityObserverSpy) RecordDrop(_ context.Context, _, _ string, observations []QualityObservation) {
	s.dropped = append(s.dropped, observations...)
}

func (s *qualityObserverSpy) RecordPersisted(_ context.Context, observations []QualityObservation) {
	s.persisted = append(s.persisted, observations...)
}

func (s *qualityObserverSpy) EffectiveQuality(unsID int64, sourceQuality int64) int64 {
	if quality, ok := s.effective[unsID]; ok && quality > sourceQuality {
		return quality
	}
	return sourceQuality
}

func TestService_WithEffectiveQualityBehavior(t *testing.T) {
	observer := &qualityObserverSpy{effective: map[int64]int64{707: 2}}
	s := &Service{qualityObserver: observer}

	payload := s.withEffectiveQuality(707, map[string]any{"quality": qualityGood})
	assert.Equal(t, qualityUncertain, payload["quality"])

	payload = s.withEffectiveQuality(707, map[string]any{"quality": qualityBad})
	assert.Equal(t, qualityBad, payload["quality"], "platform detection must not improve source quality")

	delete(observer.effective, 707)
	payload = s.withEffectiveQuality(707, map[string]any{"quality": qualityGood})
	assert.Equal(t, qualityGood, payload["quality"], "recovery falls back to source quality")

	withoutProvider := (&Service{}).withEffectiveQuality(707, map[string]any{"quality": qualityUncertain})
	assert.Equal(t, qualityUncertain, withoutProvider["quality"])
}

func TestNew_DefaultsAndWriterBehavior(t *testing.T) {
	ctx := context.Background()
	// 空 sink URL：writer 为 nil，默认参数生效
	s, err := New(ctx, config.DataIngestConf{}, "")
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, 20000, s.cfg.QueueSize)
	assert.Equal(t, 5000, s.cfg.BatchSize)
	assert.Equal(t, 1000, s.cfg.FlushIntervalMs)
	assert.Nil(t, s.writer)
	require.NotNil(t, s.resolver)
	s.Close()

	// 非法 sink DSN → 报错
	_, err = New(ctx, config.DataIngestConf{}, "://bad")
	require.Error(t, err)
}

func TestNew_WithSinkDBBehavior(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{Enabled: true, QueueSize: 8, BatchSize: 2}, dsn)
	require.NoError(t, err)
	require.NotNil(t, s.writer)
	assert.Equal(t, 2, s.writer.batchSize)
	assert.Equal(t, 8, s.cfg.QueueSize)
	s.Close()
}

func TestService_NilReceiverBehavior(t *testing.T) {
	var s *Service
	ctx := context.Background()
	require.NoError(t, s.Start(ctx))
	s.Close()
	assert.Equal(t, Stats{}, s.Stats())
	assert.Nil(t, s.Resolver())
	require.NoError(t, s.EnsurePhysicalForNodes(ctx, []repo.UnsNode{{ID: 1}}))
	payload, err := s.LastPayload(ctx, repo.UnsNode{ID: 1, Type: 2})
	require.NoError(t, err)
	assert.Nil(t, payload)
	require.NoError(t, s.ClearLatest(ctx, []repo.UnsNode{{ID: 1}}))
	require.NoError(t, s.Consume(ctx, "a/b", []byte(`{}`)))
	require.NoError(t, s.WriteOpenAPIValue(ctx, repo.UnsNode{ID: 1, Type: 2}, "x", 0, 0, false))
	resp, err := s.History(ctx, repo.UnsNode{ID: 1, Type: 2}, 0, 0, 0)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	require.NoError(t, s.EnsurePhysicalForNode(ctx, repo.UnsNode{ID: 1}))
}

func TestService_StartCloseBehavior(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, config.DataIngestConf{Enabled: false}, "")
	require.NoError(t, err)
	require.NoError(t, s.Start(ctx))
	// 重复 Start 幂等
	require.NoError(t, s.Start(ctx))
	assert.True(t, s.started.Load())
	// Stats 计数初始为 0
	stats := s.Stats()
	assert.Zero(t, stats.Written)
	assert.Zero(t, stats.Dropped)
	s.Close()
	assert.False(t, s.started.Load())
	// 重复 Close 安全
	s.Close()
}

func TestService_StartEnabledNoMQTTFails(t *testing.T) {
	// Enabled=true 但 broker 不可达：Start 应返回错误并复位 started
	_, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{Enabled: true, MqttBrokers: "tcp://127.0.0.1:1"}, dsn)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = s.Start(ctx)
	require.Error(t, err)
	assert.False(t, s.started.Load())
	s.Close()
}

func TestSplitList_Behavior(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitList("a,b"))
	assert.Equal(t, []string{"a", "b"}, splitList(" a ; b "))
	assert.Equal(t, []string{"a"}, splitList("a"))
	assert.Empty(t, splitList(""))
	assert.Empty(t, splitList(" , ; "))
}

func TestSplitBrokersTopics_Behavior(t *testing.T) {
	assert.Equal(t, []string{"tcp://x:1", "tcp://y:2"}, splitBrokers("tcp://x:1, tcp://y:2"))
	assert.Equal(t, []string{"a/#", "b/+"}, splitTopics("a/#;b/+"))
}

func TestNonEmpty_Behavior(t *testing.T) {
	assert.Equal(t, "fb", nonEmpty("", "fb"))
	assert.Equal(t, "  x  ", nonEmpty("  x  ", "fb")) // 仅空串回退
	assert.Equal(t, "v", nonEmpty("v", "fb"))
}

func TestHistoryFields_Behavior(t *testing.T) {
	fields := historyFields([]Field{{Name: "a", Type: "double"}, {Name: "b", Type: "string"}})
	require.Len(t, fields, 2)
	assert.Equal(t, map[string]any{"name": "a", "type": "double"}, fields[0])
}

func TestSinkTimeColumn_Behavior(t *testing.T) {
	assert.Equal(t, metricTimeColumn, sinkTimeColumn(&Definition{}))
}

func TestPayloadFromSinkRow_Behavior(t *testing.T) {
	// metric：按字段名取值
	row := map[string]any{"temp": 25.5, "_quality": int64(1), "_timestamp": time.Now()}
	payload := payloadFromSinkRow(row, []Field{{Name: "temp", Type: "double"}}, metricTopicType)
	assert.Equal(t, map[string]any{"temp": 25.5}, payload)
	// 事件：解析 _json
	eventRow := map[string]any{eventJSONColumn: []byte(`{"a":1}`)}
	payload = payloadFromSinkRow(eventRow, nil, 1)
	assert.Equal(t, map[string]any{"a": json.Number("1")}, payload)
}

func TestQualityFromSinkRow_Behavior(t *testing.T) {
	assert.Equal(t, qualityUncertain, qualityFromSinkRow(map[string]any{metricQualityColumn: int64(2)}, metricTopicType))
	assert.Equal(t, qualityBad, qualityFromSinkRow(map[string]any{eventJSONColumn: []byte(`{"quality":"3"}`)}, 1))
	assert.Equal(t, qualityGood, qualityFromSinkRow(map[string]any{eventJSONColumn: []byte(`{}`)}, 1))
}

func TestQualityFromEventJSON_Behavior(t *testing.T) {
	assert.Equal(t, qualityUncertain, qualityFromEventJSON([]byte(`{"_quality":2}`)))
	assert.Equal(t, qualityBad, qualityFromEventJSON(`{"quality":3}`))
	assert.Equal(t, qualityGood, qualityFromEventJSON([]byte(`{"x":1}`)))
	assert.Equal(t, qualityGood, qualityFromEventJSON(nil))
}

func TestEventPayloadFromSinkJSON_Behavior(t *testing.T) {
	assert.Equal(t, map[string]any{}, eventPayloadFromSinkJSON(nil))
	assert.Equal(t, map[string]any{"a": json.Number("1")}, eventPayloadFromSinkJSON([]byte(`{"a":1}`)))
	assert.Equal(t, map[string]any{"a": json.Number("1")}, eventPayloadFromSinkJSON(`{"a":1}`))
	assert.Equal(t, map[string]any{"a": json.Number("1")}, eventPayloadFromSinkJSON(json.RawMessage(`{"a":1}`)))
	// map 直接深拷贝
	src := map[string]any{"a": 1}
	got := eventPayloadFromSinkJSON(src)
	src["b"] = 2
	assert.Equal(t, map[string]any{"a": 1}, got)
	// 标量 → 空 map
	assert.Equal(t, map[string]any{}, eventPayloadFromSinkJSON(42))
}

func TestHistoryWindow_Behavior(t *testing.T) {
	now := time.Now().UTC()
	startAt, endAt := historyWindow(0, 0)
	assert.WithinDuration(t, now, endAt, time.Second)
	assert.Equal(t, endAt.Add(-defaultHistoryWindow), startAt)

	end := now.Add(-time.Hour).UnixMilli()
	startAt, endAt = historyWindow(0, end)
	assert.Equal(t, time.UnixMilli(end), endAt)
	assert.Equal(t, endAt.Add(-defaultHistoryWindow), startAt)

	// start > end → 回退默认窗口
	startAt, endAt = historyWindow(now.Add(time.Hour).UnixMilli(), end)
	assert.Equal(t, endAt.Add(-defaultHistoryWindow), startAt)
}

func TestHistoryLimit_Behavior(t *testing.T) {
	assert.Equal(t, defaultHistoryLimit, historyLimit(0))
	assert.Equal(t, defaultHistoryLimit, historyLimit(-1))
	assert.Equal(t, maxHistoryLimit, historyLimit(maxHistoryLimit+100))
	assert.Equal(t, 500, historyLimit(500))
}

func TestValueTimeFromAny_Behavior(t *testing.T) {
	fixed := time.UnixMilli(1732223334512).UTC()
	assert.Equal(t, fixed, valueTimeFromAny(fixed))
	assert.Equal(t, fixed, valueTimeFromAny("1732223334512"))
	assert.Equal(t, fixed, valueTimeFromAny("2024-11-21T21:08:54.512Z"))
	// 非法值回退当前时间
	got := valueTimeFromAny(42)
	assert.WithinDuration(t, time.Now(), got, time.Second)
}

func TestLivePayloadFromRecord_MetricBehavior(t *testing.T) {
	record := Record{
		UnsID:     1,
		TopicType: metricTopicType,
		Time:      time.UnixMilli(1000),
		Fields:    []Field{{Name: "temp", Type: "double"}, {Name: "_timestamp", Type: "datetime"}},
		Values:    map[string]string{"temp": "25.5"},
	}
	payload := livePayloadFromRecord(record)
	require.NotNil(t, payload)
	data := payload["data"].(map[string]any)
	assert.Equal(t, 25.5, data["temp"])
	assert.Equal(t, int64(1000), payload["updateTime"])
	assert.Equal(t, []string{"temp"}, payload["fields"])
	assert.Equal(t, map[string]int64{"temp": 1000}, payload["dt"])
	// 无有效字段 → nil
	assert.Nil(t, livePayloadFromRecord(Record{TopicType: metricTopicType}))
	// 非法数值回退原始字符串
	bad := livePayloadFromRecord(Record{
		TopicType: metricTopicType,
		Time:      time.UnixMilli(1),
		Fields:    []Field{{Name: "v", Type: "double"}},
		Values:    map[string]string{"v": "not-a-number"},
	})
	assert.Equal(t, "not-a-number", bad["data"].(map[string]any)["v"])
}

func TestLiveEventPayloadFromRecord_Behavior(t *testing.T) {
	record := Record{
		UnsID:        2,
		TopicType:    1,
		Time:         time.UnixMilli(2000),
		EventPayload: map[string]any{"a": 1, "b": "x"},
	}
	payload := liveEventPayloadFromRecord(record)
	require.NotNil(t, payload)
	assert.Equal(t, map[string]any{"a": 1, "b": "x"}, payload["data"])
	assert.Equal(t, []string{"a", "b"}, payload["fields"])
	assert.Equal(t, int64(2000), payload["updateTime"])
	assert.Nil(t, liveEventPayloadFromRecord(Record{TopicType: 1}))
}

func TestPublishPayloadRecords_Behavior(t *testing.T) {
	ch, cancel := eventstream.Subscribe(func(event eventstream.Event) bool {
		return event.Type == "uns.payload"
	})
	defer cancel()
	records := []Record{
		{
			UnsID:     1,
			Alias:     "metric1",
			Namespace: "Metric/line1",
			TopicType: metricTopicType,
			Time:      time.UnixMilli(1000),
			Fields:    []Field{{Name: "temp", Type: "double"}},
			Values:    map[string]string{"temp": "25.5"},
		},
		{UnsID: 0, TopicType: 1}, // 无数据 → 不发布
	}
	publishPayloadRecords(records)
	select {
	case event := <-ch:
		assert.Equal(t, "uns.payload", event.Type)
		assert.Contains(t, event.Keys, "id:1")
		assert.Contains(t, event.Keys, "alias:metric1")
		assert.Contains(t, event.Keys, "namespace:Metric/line1")
		data := event.Data["data"].(map[string]any)
		assert.Equal(t, 25.5, data["temp"])
	case <-time.After(time.Second):
		t.Fatal("expected uns.payload event")
	}
}

func TestWriteOpenAPIValue_WithTimestampBehavior(t *testing.T) {
	s := &Service{}
	var gotPayload []byte
	s.publishOverride = func(topic string, qos byte, retain bool, payload []byte) error {
		gotPayload = payload
		return nil
	}
	node := repo.UnsNode{ID: 1, Namespace: "Metric/temp", Type: 2, TopicType: 3}

	// map 值缺 timeStamp → 补入
	require.NoError(t, s.WriteOpenAPIValue(context.Background(), node, map[string]any{"v": 1}, 12345, 0, false))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(gotPayload, &decoded))
	assert.Equal(t, json.Number("12345"), decoded["timeStamp"])

	// 标量值 + 时间戳 → {"value":…,"timeStamp":…}
	require.NoError(t, s.WriteOpenAPIValue(context.Background(), node, "hello", 67890, 0, false))
	decoded = map[string]any{}
	require.NoError(t, json.Unmarshal(gotPayload, &decoded))
	assert.Equal(t, "hello", decoded["value"])
	assert.Equal(t, json.Number("67890"), decoded["timeStamp"])
}

func TestWriteOpenAPIValue_NonType2NodeNoop(t *testing.T) {
	s := &Service{}
	require.NoError(t, s.WriteOpenAPIValue(context.Background(), repo.UnsNode{ID: 1, Type: 1}, "x", 0, 0, false))
}

func TestPublishOpenAPIWrite_Behavior(t *testing.T) {
	s := &Service{}
	// 空 topic
	err := s.publishOpenAPIWrite("//", 0, false, []byte("x"))
	require.Error(t, err)
	assert.Equal(t, "mqtt topic is empty", err.Error())
	// 无 client 无 override
	err = s.publishOpenAPIWrite("a/b", 0, false, []byte("x"))
	require.Error(t, err)
	assert.Equal(t, "mqtt client is not initialized", err.Error())
	// override 优先
	s.publishOverride = func(topic string, qos byte, retain bool, payload []byte) error {
		return errors.New("override error")
	}
	err = s.publishOpenAPIWrite("a/b", 0, false, []byte("x"))
	require.Error(t, err)
	assert.Equal(t, "override error", err.Error())
}

func TestConsume_UnknownAndSystemTopics(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	// 未知 topic → 静默忽略
	require.NoError(t, s.Consume(ctx, "not/exist/ns", []byte(`{"v":1}`)))
	// $ 前缀系统 topic → 忽略
	require.NoError(t, s.Consume(ctx, "$SYS/broker/load", []byte(`{}`)))
	// 非法 payload 错误透传
	require.NoError(t, s.Consume(ctx, "$SYS/broker", []byte(`{bad`)))
}

func TestConsume_QueueAndLatestCacheBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	cacheClient := testkit.NewTestCache(t)
	s, err := New(context.Background(), config.DataIngestConf{QueueSize: 4, BatchSize: 100, FlushIntervalMs: 1000}, dsn, cacheClient)
	require.NoError(t, err)
	defer s.Close()
	node := repo.UnsNode{
		ID:            701,
		Namespace:     "Test/ns/consume1",
		Alias:         "consume1",
		Name:          "consume1",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)

	ctx := context.Background()
	require.NoError(t, s.Consume(ctx, "Test/ns/consume1", []byte(`{"temp":25.5}`)))
	require.NoError(t, s.Consume(ctx, "Test/ns/consume1", []byte(`{"temp":26.5}`)))
	assert.Equal(t, 2, len(s.queue))

	// 最新值缓存已写入
	payload, ok, err := s.latest.Get(ctx, 701)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 26.5, payload["data"].(map[string]any)["temp"])

	// 队列满 → 报错并计入 dropped（queue 容量 4，已入 2，再塞 2 后满）
	require.NoError(t, s.Consume(ctx, "Test/ns/consume1", []byte(`{"temp":27.5}`)))
	require.NoError(t, s.Consume(ctx, "Test/ns/consume1", []byte(`{"temp":28.5}`)))
	err = s.Consume(ctx, "Test/ns/consume1", []byte(`{"temp":29.5}`))
	require.Error(t, err)
	assert.Equal(t, "dataingest queue full", err.Error())
	assert.Equal(t, int64(1), s.dropped.Load())
	assert.Equal(t, 4, s.Stats().Queued)
}

func TestConsume_SaveToDBDisabledNoQueue(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	// EnableHistory 默认 2 → SaveToDB=false
	node := repo.UnsNode{
		ID:        702,
		Namespace: "Test/ns/consume2",
		Name:      "consume2",
		Type:      2,
		TopicType: metricTopicType,
		Schema:    json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)
	require.NoError(t, s.Consume(context.Background(), "Test/ns/consume2", []byte(`{"temp":1}`)))
	assert.Empty(t, s.queue)
}

func TestService_ConsumeFlushWritesToSink(t *testing.T) {
	// flushLoop 停止路径：Close 时把队列中记录落库
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{Enabled: false, QueueSize: 16, BatchSize: 100, FlushIntervalMs: 60000}, dsn)
	require.NoError(t, err)
	node := repo.UnsNode{
		ID:               703,
		Namespace:        "Test/ns/flush1",
		Alias:            "flush1",
		Name:             "flush1",
		Type:             2,
		TopicType:        metricTopicType,
		EnableHistory:    1,
		Schema:           json.RawMessage(`[{"name":"temp","type":"double"}]`),
		ExtendProperties: json.RawMessage(`{}`),
	}
	testkit.Insert(t, store.CommonDB(), &node)
	ctx := context.Background()
	require.NoError(t, s.Start(ctx))
	require.NoError(t, s.Consume(ctx, "Test/ns/flush1", []byte(`{"temp":31.5,"quality":"Uncertain"}`)))
	// Sink timestamps are stored with millisecond precision; keep the samples
	// distinct so the (_id, _timestamp) merge key does not collapse the rows.
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, s.Consume(ctx, "Test/ns/flush1", []byte(`{"temp":32.5,"quality":"Bad"}`)))
	s.Close()

	// flushLoop 在 stop 时已把 2 条记录写入 uns_timeserial
	var count int64
	require.NoError(t, store.UnsDB().WithContext(ctx).Raw(`SELECT count(*) FROM public."flush1"`).Scan(&count).Error)
	assert.Equal(t, int64(2), count)
	var qualities []int64
	require.NoError(t, store.UnsDB().WithContext(ctx).Raw(
		`SELECT "_quality" FROM public."flush1" ORDER BY "_timestamp"`,
	).Scan(&qualities).Error)
	assert.Equal(t, []int64{2, 3}, qualities)
	assert.Equal(t, int64(2), s.written.Load())
}

func TestEnsurePhysicalForNode_Behavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{Enabled: true, QueueSize: 8}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:            704,
		Namespace:     "Test/ns/phys1",
		Alias:         "phys1",
		Name:          "phys1",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	require.NoError(t, store.CommonDB().Create(&node).Error)
	require.NoError(t, s.EnsurePhysicalForNode(ctx, node))
	// 视图已创建
	var viewExists bool
	require.NoError(t, s.writer.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM information_schema.views WHERE table_schema = 'public' AND table_name = 'phys1')`).Scan(&viewExists))
	assert.True(t, viewExists)
	// 非 type2 / SaveToDB=false / 空列表均跳过
	require.NoError(t, s.EnsurePhysicalForNodes(ctx, []repo.UnsNode{{ID: 1, Type: 1}}))
	require.NoError(t, s.EnsurePhysicalForNodes(ctx, nil))
	require.NoError(t, s.EnsurePhysicalForNodes(ctx, []repo.UnsNode{{ID: 705, Type: 2, TopicType: 3, EnableHistory: 2, Namespace: "Test/ns/nosave"}}))
}

func TestLastPayload_FromDBBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:            705,
		Namespace:     "Test/ns/last1",
		Alias:         "last1",
		Name:          "last1",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)

	// 无数据 → nil
	payload, err := s.LastPayload(ctx, node)
	require.NoError(t, err)
	assert.Nil(t, payload)

	// 写入两行，取最新
	base := time.Now().UTC().Add(-time.Minute)
	records := []Record{
		{UnsID: 705, Alias: "last1", Namespace: "Test/ns/last1", TopicType: metricTopicType,
			Time: base, Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "10.5"}},
		{UnsID: 705, Alias: "last1", Namespace: "Test/ns/last1", TopicType: metricTopicType,
			Time: base.Add(time.Second), Quality: 3, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "11.5"}},
	}
	require.NoError(t, s.writer.WriteRecords(ctx, records))

	payload, err = s.LastPayload(ctx, node)
	require.NoError(t, err)
	require.NotNil(t, payload)
	data := payload["data"].(map[string]any)
	assert.Equal(t, 11.5, data["temp"])
	assert.Equal(t, qualityBad, payload["quality"])
	assert.Equal(t, int64(705), node.ID)
	assert.Equal(t, []string{"temp"}, payload["fields"])
}

func TestLastPayload_EventNodeBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:            706,
		Namespace:     "Test/ns/lastevent",
		Alias:         "lastevent",
		Name:          "lastevent",
		Type:          2,
		TopicType:     1,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"a","type":"long"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)
	record := Record{
		UnsID: 706, Alias: "lastevent", Namespace: "Test/ns/lastevent", TopicType: 1,
		Time:         time.Now().UTC(),
		Fields:       []Field{{Name: "a", Type: "long"}},
		EventPayload: map[string]any{"a": 5},
	}
	require.NoError(t, s.writer.WriteRecords(ctx, []Record{record}))
	payload, err := s.LastPayload(ctx, node)
	require.NoError(t, err)
	require.NotNil(t, payload)
	data := payload["data"].(map[string]any)
	assert.Equal(t, json.Number("5"), data["a"])
}

func TestLastPayload_NonType2OrNoWriter(t *testing.T) {
	s := &Service{}
	payload, err := s.LastPayload(context.Background(), repo.UnsNode{ID: 1, Type: 1})
	require.NoError(t, err)
	assert.Nil(t, payload)
	_, dsn := testkit.NewTestDB(t)
	s2, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	s2.writer = nil
	defer s2.Close()
	payload, err = s2.LastPayload(context.Background(), repo.UnsNode{ID: 1, Type: 2})
	require.NoError(t, err)
	assert.Nil(t, payload)
}

func TestLastPayload_CacheBackfillBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	cacheClient := testkit.NewTestCache(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn, cacheClient)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:               707,
		Namespace:        "Test/ns/backfill",
		Alias:            "backfill",
		Name:             "backfill",
		Type:             2,
		TopicType:        metricTopicType,
		EnableHistory:    1,
		Schema:           json.RawMessage(`[{"name":"temp","type":"double"}]`),
		ExtendProperties: json.RawMessage(`{}`),
	}
	testkit.Insert(t, store.CommonDB(), &node)
	record := Record{
		UnsID: 707, Alias: "backfill", Namespace: "Test/ns/backfill", TopicType: metricTopicType,
		Time: time.Now().UTC(), Quality: 1,
		Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "20.5"},
	}
	require.NoError(t, s.writer.WriteRecords(ctx, []Record{record}))
	// 首次 LastPayload 从 DB 读并回填缓存
	payload, err := s.LastPayload(ctx, node)
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Equal(t, 20.5, payload["data"].(map[string]any)["temp"])
	// 第二次直接从缓存读
	cached, ok, err := s.latest.Get(ctx, 707)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 20.5, cached["data"].(map[string]any)["temp"])
}

func TestLatestSinkBatchSQL_Behavior(t *testing.T) {
	sql := latestSinkBatchSQL([]latestSinkTarget{
		{node: repo.UnsNode{ID: 1}, alias: `metric"one`},
		{node: repo.UnsNode{ID: 2}, alias: "metric-two"},
	})
	assert.Contains(t, sql, `SELECT 1::bigint AS node_id`)
	assert.Contains(t, sql, `"public"."metric""one"`)
	assert.Contains(t, sql, "UNION ALL")
	assert.Contains(t, sql, `SELECT 2::bigint AS node_id`)
	assert.Equal(t, 500, latestMetricDBBatchSize)
	assert.Equal(t, 100, latestEventDBBatchSize)
	assert.Equal(t, 2, latestPayloadReadConcurrency)
}

func TestClearLatest_Behavior(t *testing.T) {
	cacheClient := testkit.NewTestCache(t)
	s := &Service{latest: newLatestCache(cacheClient)}
	ctx := context.Background()
	require.NoError(t, s.latest.UpsertPayload(ctx, 1, map[string]any{"timeStamp": int64(100)}))
	require.NoError(t, s.latest.UpsertPayload(ctx, 2, map[string]any{"timeStamp": int64(100)}))
	// 只清理 type2 节点
	require.NoError(t, s.ClearLatest(ctx, []repo.UnsNode{{ID: 1, Type: 2}, {ID: 2, Type: 1}}))
	_, ok, _ := s.latest.Get(ctx, 1)
	assert.False(t, ok)
	_, ok, _ = s.latest.Get(ctx, 2)
	assert.True(t, ok)
	// 空列表安全
	require.NoError(t, s.ClearLatest(ctx, nil))
}

func TestStats_Behavior(t *testing.T) {
	s := &Service{}
	stats := s.Stats()
	assert.Zero(t, stats.Written)
	assert.Zero(t, stats.Dropped)
	assert.Zero(t, stats.Queued)
	s.written.Add(3)
	s.dropped.Add(1)
	stats = s.Stats()
	assert.Equal(t, int64(3), stats.Written)
	assert.Equal(t, int64(1), stats.Dropped)
}

func TestResolver_Behavior(t *testing.T) {
	s := &Service{resolver: NewDefinitionResolver(nil)}
	assert.NotNil(t, s.Resolver())
	assert.Nil(t, (*Service)(nil).Resolver())
}
