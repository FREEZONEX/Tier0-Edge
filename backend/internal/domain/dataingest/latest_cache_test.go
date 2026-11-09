package dataingest

// latest_cache_test.go
// 测试目标：latestCache 最新值缓存的 Redis 读写与纯函数助手。
// 正常路径：Upsert 后 Get 命中；时间戳新值覆盖旧值；批量 UpsertRecords 按时间取最新；
//   DeleteIDs 清理；payload 规范化（data/payload 镜像、dt/fields 推导、quality 归一化）。
// 异常情况：nodeID<=0 与空 payload 直接跳过；旧时间戳不覆盖；无缓存键返回 miss；
//   nil cache / nil redis 不 panic；非法 JSON 缓存值返回错误。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"backend/internal/infra/cache"
	"backend/internal/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLatestCacheGet_MissBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	payload, ok, err := c.Get(context.Background(), 42)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, payload)
}

func TestLatestCacheUpsertAndGetBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	ctx := context.Background()
	payload := map[string]any{"data": map[string]any{"temp": 25.5}, "timeStamp": int64(1732223334512)}
	require.NoError(t, c.UpsertPayload(ctx, 42, payload))

	got, ok, err := c.Get(ctx, 42)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(1732223334512), got["timeStamp"])
	// data/payload 互为镜像，quality 缺省为 Good
	assert.Equal(t, qualityGood, got["quality"])
	assert.Equal(t, got["data"], got["payload"])
}

func TestLatestCacheMarkNoDataBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	ctx := context.Background()
	require.NoError(t, c.MarkNoData(ctx, []int64{41, 42, 42, 0}))

	payload, ok, err := c.Get(ctx, 42)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Nil(t, payload)
	many, err := c.GetMany(ctx, []int64{41, 42})
	require.NoError(t, err)
	require.Contains(t, many, int64(41))
	require.Contains(t, many, int64(42))
	assert.Nil(t, many[41])
	assert.Nil(t, many[42])

	// 新数据可以覆盖负缓存；负缓存不能覆盖已经存在的正值。
	require.NoError(t, c.UpsertPayload(ctx, 42, map[string]any{"data": map[string]any{"v": 1}, "timeStamp": int64(100)}))
	require.NoError(t, c.MarkNoData(ctx, []int64{42}))
	payload, ok, err = c.Get(ctx, 42)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, payload)
	assert.Equal(t, float64(1), payload["data"].(map[string]any)["v"])
}

func TestLatestCacheUpsertPayload_OldTimestampIgnoredBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	ctx := context.Background()
	require.NoError(t, c.UpsertPayload(ctx, 7, map[string]any{"data": map[string]any{"v": "new"}, "timeStamp": int64(200)}))
	// 旧时间戳不覆盖（Lua 脚本返回 0）
	require.NoError(t, c.UpsertPayload(ctx, 7, map[string]any{"data": map[string]any{"v": "old"}, "timeStamp": int64(100)}))
	got, ok, _ := c.Get(ctx, 7)
	require.True(t, ok)
	assert.Equal(t, "new", got["data"].(map[string]any)["v"])
	// 相等或更新的时间戳覆盖
	require.NoError(t, c.UpsertPayload(ctx, 7, map[string]any{"data": map[string]any{"v": "equal"}, "timeStamp": int64(200)}))
	got, ok, _ = c.Get(ctx, 7)
	require.True(t, ok)
	assert.Equal(t, "equal", got["data"].(map[string]any)["v"])
}

func TestLatestCacheUpsertPayload_NoTimestampNoopBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	ctx := context.Background()
	// 无时间戳 → 不落缓存也不报错
	require.NoError(t, c.UpsertPayload(ctx, 8, map[string]any{"data": map[string]any{"v": 1}}))
	_, ok, err := c.Get(ctx, 8)
	require.NoError(t, err)
	assert.False(t, ok)
	// 空 payload / nodeID<=0 直接跳过
	require.NoError(t, c.UpsertPayload(ctx, 0, map[string]any{"data": map[string]any{"v": 1}, "timeStamp": int64(1)}))
	require.NoError(t, c.UpsertPayload(ctx, 8, nil))
}

func TestLatestCacheUpsertRecordsBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	ctx := context.Background()
	base := time.UnixMilli(1000)
	records := []Record{
		{UnsID: 9, Time: base, Quality: 1, Values: map[string]string{"v": "old"}, Fields: []Field{{Name: "v", Type: "string"}}},
		{UnsID: 9, Time: base.Add(time.Second), Quality: 3, Values: map[string]string{"v": "new"}, Fields: []Field{{Name: "v", Type: "string"}}},
		{UnsID: 0, Time: base, Values: map[string]string{"v": "skip"}}, // UnsID<=0 跳过
	}
	require.NoError(t, c.UpsertRecords(ctx, records))
	got, ok, err := c.Get(ctx, 9)
	require.NoError(t, err)
	require.True(t, ok)
	// 取时间戳最新的记录
	assert.Equal(t, "new", got["data"].(map[string]any)["v"])
	assert.Equal(t, qualityBad, got["quality"])
	// 空输入 / nil cache 安全
	require.NoError(t, c.UpsertRecords(ctx, nil))
	require.NoError(t, (*latestCache)(nil).UpsertRecords(ctx, records))
}

func TestLatestCacheDeleteIDsBehavior(t *testing.T) {
	c := newLatestCache(testkit.NewTestCache(t))
	ctx := context.Background()
	for _, id := range []int64{11, 12, 13} {
		require.NoError(t, c.UpsertPayload(ctx, id, map[string]any{"timeStamp": int64(100)}))
	}
	require.NoError(t, c.DeleteIDs(ctx, 11, 12, 12, 0)) // 重复 ID 与非法 ID 安全
	_, ok, _ := c.Get(ctx, 11)
	assert.False(t, ok)
	_, ok, _ = c.Get(ctx, 12)
	assert.False(t, ok)
	_, ok, _ = c.Get(ctx, 13)
	assert.True(t, ok)
	// 空输入安全
	require.NoError(t, c.DeleteIDs(ctx))
	require.NoError(t, (*latestCache)(nil).DeleteIDs(ctx, 1))
}

func TestLatestCacheKey_Behavior(t *testing.T) {
	assert.Equal(t, "cache:uns:latest:5", latestCacheKey(5))
}

func TestLatestCacheNilCacheBehavior(t *testing.T) {
	var c *latestCache
	ctx := context.Background()
	payload, ok, err := c.Get(ctx, 1)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, payload)
	require.NoError(t, c.UpsertPayload(ctx, 1, map[string]any{"timeStamp": int64(1)}))
	require.NoError(t, c.UpsertRecords(ctx, []Record{{UnsID: 1}}))
	require.NoError(t, c.MarkNoData(ctx, []int64{1}))
	require.NoError(t, c.DeleteIDs(ctx, 1))
}

func TestLatestCacheGet_InvalidJSONBehavior(t *testing.T) {
	client := testkit.NewTestCache(t)
	c := newLatestCache(client)
	ctx := context.Background()
	require.NoError(t, client.SetString(ctx, latestCacheKey(20), "{not-json", 0))
	_, ok, err := c.Get(ctx, 20)
	require.Error(t, err)
	assert.False(t, ok)
}

func TestLatestPayloadFromRecord_Behavior(t *testing.T) {
	record := Record{
		UnsID:     1,
		TopicType: metricTopicType,
		Time:      time.UnixMilli(1732223334512),
		Fields:    []Field{{Name: "temp", Type: "double"}},
		Values:    map[string]string{"temp": "25.5"},
		Quality:   2,
	}
	payload := latestPayloadFromRecord(record)
	require.NotNil(t, payload)
	assert.Equal(t, int64(1732223334512), payload["timeStamp"])
	assert.Equal(t, qualityUncertain, payload["quality"])
	data := payload["data"].(map[string]any)
	assert.Equal(t, 25.5, data["temp"])
}

func TestNormalizeLatestPayload_Behavior(t *testing.T) {
	// payload 键 → data 镜像
	payload := map[string]any{"payload": map[string]any{"a": 1}}
	normalizeLatestPayload(payload)
	assert.Equal(t, payload["payload"], payload["data"])
	// data 键 → payload 镜像
	payload = map[string]any{"data": map[string]any{"b": 2}}
	normalizeLatestPayload(payload)
	assert.Equal(t, payload["data"], payload["payload"])
	// 时间戳存在 → timeStamp/updateTime/dt/fields 推导
	payload = map[string]any{"data": map[string]any{"c": 3}, "updateTime": int64(500)}
	normalizeLatestPayload(payload)
	assert.Equal(t, int64(500), payload["timeStamp"])
	assert.Equal(t, int64(500), payload["updateTime"])
	assert.Equal(t, []string{"c"}, payload["fields"])
	assert.Equal(t, map[string]int64{"c": 500}, payload["dt"])
	assert.Equal(t, qualityGood, payload["quality"])
	// nil 安全
	normalizeLatestPayload(nil)
}

func TestLatestPayloadTimestamp_Behavior(t *testing.T) {
	assert.Equal(t, int64(0), latestPayloadTimestamp(nil))
	// timeStamp 优先
	assert.Equal(t, int64(1), latestPayloadTimestamp(map[string]any{"timeStamp": int64(1), "updateTime": int64(2)}))
	// updateTime 次之
	assert.Equal(t, int64(2), latestPayloadTimestamp(map[string]any{"updateTime": int64(2), "timestamp": int64(3)}))
	// dt 兜底取最大值
	assert.Equal(t, int64(9), latestPayloadTimestamp(map[string]any{"dt": map[string]int64{"a": 5, "b": 9}}))
}

func TestLatestPayloadFields_Behavior(t *testing.T) {
	assert.Nil(t, latestPayloadFields(nil))
	// []string 排序
	assert.Equal(t, []string{"a", "b"}, latestPayloadFields(map[string]any{"fields": []string{"b", "a"}}))
	// []any 过滤空白
	assert.Equal(t, []string{"a", "b"}, latestPayloadFields(map[string]any{"fields": []any{"b", "", "a"}}))
	// data/payload map 键
	assert.Equal(t, []string{"x", "y"}, latestPayloadFields(map[string]any{"data": map[string]any{"y": 1, "x": 2}}))
	assert.Equal(t, []string{"x"}, latestPayloadFields(map[string]any{"payload": map[string]any{"x": 1}}))
}

func TestLatestPayloadDT_Behavior(t *testing.T) {
	dt, ok := latestPayloadDT(map[string]any{"dt": map[string]int64{"a": 1}})
	assert.True(t, ok)
	assert.Equal(t, int64(1), dt["a"])
	// map[string]any 转换
	dt, ok = latestPayloadDT(map[string]any{"dt": map[string]any{"a": json.Number("3")}})
	assert.True(t, ok)
	assert.Equal(t, int64(3), dt["a"])
	// 空 dt 或非 map → 失败
	_, ok = latestPayloadDT(map[string]any{"dt": map[string]any{}})
	assert.False(t, ok)
	_, ok = latestPayloadDT(map[string]any{})
	assert.False(t, ok)
	_, ok = latestPayloadDT(nil)
	assert.False(t, ok)
}

func TestQualityFromRecord_Behavior(t *testing.T) {
	// RawValues 优先
	assert.Equal(t, qualityBad, qualityFromRecord(Record{RawValues: map[string]string{"_quality": "3"}}))
	assert.Equal(t, qualityUncertain, qualityFromRecord(Record{RawValues: map[string]string{"quality": "2"}}))
	// 回退 Quality 数值
	assert.Equal(t, qualityGood, qualityFromRecord(Record{Quality: 1}))
	assert.Equal(t, qualityBad, qualityFromRecord(Record{Quality: 3}))
	assert.Equal(t, qualityBad, qualityFromRecord(Record{Quality: 1, SourceQuality: "Bad"}))
	assert.Equal(t, qualityBad, qualityFromRecord(Record{RawValues: map[string]string{"quality": "Good"}, SourceQuality: "Bad"}))
}

func TestQualityFromAny_Behavior(t *testing.T) {
	assert.Equal(t, qualityGood, qualityFromAny(nil))
	assert.Equal(t, qualityUncertain, qualityFromAny("Uncertain"))
	assert.Equal(t, qualityBad, qualityFromAny([]byte("Bad")))
	assert.Equal(t, qualityUncertain, qualityFromAny(json.Number("2")))
	assert.Equal(t, qualityBad, qualityFromAny(json.Number("3")))
	assert.Equal(t, qualityGood, qualityFromAny(1))
	assert.Equal(t, qualityUncertain, qualityFromAny(int64(2)))
	assert.Equal(t, qualityBad, qualityFromAny(float64(3)))
	assert.Equal(t, qualityGood, qualityFromAny(float64(1.5))) // 截断为 1
	assert.Equal(t, qualityGood, qualityFromAny("garbage"))
	assert.Equal(t, qualityGood, qualityFromAny(uint64(1)))
}

func TestQualityCodeAndWorseQuality_Behavior(t *testing.T) {
	assert.Equal(t, int64(1), qualityCode(nil))
	assert.Equal(t, int64(2), qualityCode("Uncertain"))
	assert.Equal(t, int64(3), qualityCode("Bad"))
	assert.Equal(t, qualityUncertain, worseQuality(qualityGood, qualityUncertain))
	assert.Equal(t, qualityBad, worseQuality(qualityBad, qualityUncertain))
}

func TestNormalizeQuality_Behavior(t *testing.T) {
	assert.Equal(t, qualityGood, normalizeQuality(""))
	assert.Equal(t, qualityGood, normalizeQuality("0"))
	assert.Equal(t, qualityGood, normalizeQuality("1"))
	assert.Equal(t, qualityGood, normalizeQuality("good"))
	assert.Equal(t, qualityUncertain, normalizeQuality("2"))
	assert.Equal(t, qualityUncertain, normalizeQuality("UNCERTAIN"))
	assert.Equal(t, qualityBad, normalizeQuality("3"))
	assert.Equal(t, qualityBad, normalizeQuality("bad"))
	assert.Equal(t, qualityGood, normalizeQuality("unknown"))
}

func TestQualityFromNumber_Behavior(t *testing.T) {
	assert.Equal(t, qualityGood, qualityFromNumber(0))
	assert.Equal(t, qualityGood, qualityFromNumber(1))
	assert.Equal(t, qualityUncertain, qualityFromNumber(2))
	assert.Equal(t, qualityBad, qualityFromNumber(3))
	assert.Equal(t, qualityGood, qualityFromNumber(99))
}

func TestAnyInt64_Behavior(t *testing.T) {
	assert.Equal(t, int64(1), anyInt64(int64(1)))
	assert.Equal(t, int64(1), anyInt64(1))
	assert.Equal(t, int64(1), anyInt64(int32(1)))
	assert.Equal(t, int64(1), anyInt64(int16(1)))
	assert.Equal(t, int64(1), anyInt64(int8(1)))
	assert.Equal(t, int64(1), anyInt64(uint(1)))
	assert.Equal(t, int64(1), anyInt64(uint64(1)))
	assert.Equal(t, int64(1), anyInt64(uint32(1)))
	assert.Equal(t, int64(1), anyInt64(uint16(1)))
	assert.Equal(t, int64(1), anyInt64(uint8(1)))
	assert.Equal(t, int64(1), anyInt64(float64(1.9)))
	assert.Equal(t, int64(1), anyInt64(float32(1)))
	assert.Equal(t, int64(2), anyInt64(json.Number("2")))
	assert.Equal(t, int64(3), anyInt64("3"))
	assert.Equal(t, int64(0), anyInt64("not-a-number"))
	assert.Equal(t, int64(0), anyInt64(nil))
	assert.Equal(t, int64(0), anyInt64(map[string]any{}))
}

func TestLatestCacheRedisNotInitializedBehavior(t *testing.T) {
	// cache.Client 未连接 redis（nil 底层）时返回错误而非 panic
	client := &cache.Client{}
	c := newLatestCache(client)
	_, _, err := c.Get(context.Background(), 1)
	require.Error(t, err)
}
