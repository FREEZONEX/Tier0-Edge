package dataingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/infra/cache"
)

const (
	latestCacheKeyPrefix = "cache:uns:latest"
	latestCacheBatchSize = 1000
	latestCacheNoDataTTL = time.Minute
	latestCacheNoDataKey = "_tier0NoData"

	qualityGood      = "Good"
	qualityUncertain = "Uncertain"
	qualityBad       = "Bad"
)

type latestCache struct {
	redis *cache.Client
}

func newLatestCache(redis *cache.Client) *latestCache {
	if redis == nil {
		return nil
	}
	return &latestCache{redis: redis}
}

func (c *latestCache) Get(ctx context.Context, nodeID int64) (map[string]any, bool, error) {
	if c == nil || c.redis == nil || nodeID <= 0 {
		return nil, false, nil
	}
	raw, err := c.redis.GetString(ctx, latestCacheKey(nodeID))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, false, err
	}
	if isLatestCacheNoData(payload) {
		return nil, true, nil
	}
	normalizeLatestPayload(payload)
	if len(payload) == 0 {
		return nil, false, nil
	}
	return payload, true, nil
}

func (c *latestCache) GetMany(ctx context.Context, nodeIDs []int64) (map[int64]map[string]any, error) {
	out := make(map[int64]map[string]any, len(nodeIDs))
	if c == nil || c.redis == nil || len(nodeIDs) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(nodeIDs))
	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID <= 0 {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		ids = append(ids, nodeID)
	}
	var firstErr error
	for start := 0; start < len(ids); start += latestCacheBatchSize {
		end := start + latestCacheBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		keys := make([]string, 0, len(batch))
		for _, nodeID := range batch {
			keys = append(keys, latestCacheKey(nodeID))
		}
		values, err := c.redis.MGetStrings(ctx, keys...)
		if err != nil {
			return out, err
		}
		for idx, raw := range values {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("decode latest cache node %d: %w", batch[idx], err)
				}
				continue
			}
			if isLatestCacheNoData(payload) {
				out[batch[idx]] = nil
				continue
			}
			normalizeLatestPayload(payload)
			if len(payload) > 0 {
				out[batch[idx]] = payload
			}
		}
	}
	return out, firstErr
}

func (c *latestCache) MarkNoData(ctx context.Context, nodeIDs []int64) error {
	if c == nil || c.redis == nil || len(nodeIDs) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{latestCacheNoDataKey: true})
	if err != nil {
		return err
	}
	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID <= 0 {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		if _, err := c.redis.SetNX(ctx, latestCacheKey(nodeID), string(body), latestCacheNoDataTTL); err != nil {
			return err
		}
	}
	return nil
}

func (c *latestCache) UpsertRecords(ctx context.Context, records []Record) error {
	if c == nil || c.redis == nil || len(records) == 0 {
		return nil
	}
	latestByID := make(map[int64]map[string]any, len(records))
	for _, record := range records {
		if record.UnsID <= 0 {
			continue
		}
		payload := latestPayloadFromRecord(record)
		if len(payload) == 0 {
			continue
		}
		current := latestByID[record.UnsID]
		if current == nil || latestPayloadTimestamp(payload) >= latestPayloadTimestamp(current) {
			latestByID[record.UnsID] = payload
		}
	}
	return c.UpsertPayloads(ctx, latestByID)
}

func (c *latestCache) UpsertPayloads(ctx context.Context, payloads map[int64]map[string]any) error {
	if c == nil || c.redis == nil || len(payloads) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(payloads))
	for nodeID := range payloads {
		if nodeID > 0 {
			ids = append(ids, nodeID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, nodeID := range ids {
		payload := payloads[nodeID]
		if len(payload) == 0 {
			continue
		}
		normalizeLatestPayload(payload)
		ts := latestPayloadTimestamp(payload)
		if ts <= 0 {
			continue
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := c.redis.UpdateIfNewer(ctx, latestCacheKey(nodeID), string(body), ts); err != nil {
			return err
		}
	}
	return nil
}

func (c *latestCache) UpsertPayload(ctx context.Context, nodeID int64, payload map[string]any) error {
	if c == nil || c.redis == nil || nodeID <= 0 || len(payload) == 0 {
		return nil
	}
	normalizeLatestPayload(payload)
	ts := latestPayloadTimestamp(payload)
	if ts <= 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.redis.UpdateIfNewer(ctx, latestCacheKey(nodeID), string(body), ts)
	return err
}

func (c *latestCache) DeleteIDs(ctx context.Context, ids ...int64) error {
	if c == nil || c.redis == nil || len(ids) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		keys = append(keys, latestCacheKey(id))
	}
	return c.redis.Del(ctx, keys...)
}

func latestCacheKey(nodeID int64) string {
	return fmt.Sprintf("%s:%d", latestCacheKeyPrefix, nodeID)
}

func isLatestCacheNoData(payload map[string]any) bool {
	value, ok := payload[latestCacheNoDataKey]
	return ok && value == true
}

func latestPayloadFromRecord(record Record) map[string]any {
	payload := livePayloadFromRecord(record)
	if len(payload) == 0 {
		return nil
	}
	ts := latestPayloadTimestamp(payload)
	payload["timeStamp"] = ts
	payload["quality"] = qualityFromRecord(record)
	return payload
}

func normalizeLatestPayload(payload map[string]any) {
	if payload == nil {
		return
	}
	data, dataOK := payload["data"]
	body, bodyOK := payload["payload"]
	if !dataOK && bodyOK {
		payload["data"] = body
		data = body
		dataOK = true
	}
	if !bodyOK && dataOK {
		payload["payload"] = data
	}
	ts := latestPayloadTimestamp(payload)
	if ts > 0 {
		payload["timeStamp"] = ts
		if openapiAny := payload["updateTime"]; openapiAny == nil || anyInt64(openapiAny) == 0 {
			payload["updateTime"] = ts
		}
	}
	fields := latestPayloadFields(payload)
	if len(fields) > 0 {
		payload["fields"] = fields
		if _, ok := latestPayloadDT(payload); !ok && ts > 0 {
			dt := make(map[string]int64, len(fields))
			for _, field := range fields {
				dt[field] = ts
			}
			payload["dt"] = dt
		}
	}
	payload["quality"] = qualityFromAny(payload["quality"])
}

func latestPayloadTimestamp(payload map[string]any) int64 {
	if payload == nil {
		return 0
	}
	for _, key := range []string{"timeStamp", "updateTime", "timestamp"} {
		if ts := anyInt64(payload[key]); ts > 0 {
			return ts
		}
	}
	if dt, ok := latestPayloadDT(payload); ok {
		var latest int64
		for _, ts := range dt {
			if ts > latest {
				latest = ts
			}
		}
		return latest
	}
	return 0
}

func latestPayloadFields(payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	switch raw := payload["fields"].(type) {
	case []string:
		out := append([]string(nil), raw...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if field := strings.TrimSpace(fmt.Sprint(item)); field != "" {
				out = append(out, field)
			}
		}
		sort.Strings(out)
		return out
	}
	body, _ := payload["data"].(map[string]any)
	if len(body) == 0 {
		body, _ = payload["payload"].(map[string]any)
	}
	if len(body) == 0 {
		return nil
	}
	out := make([]string, 0, len(body))
	for field := range body {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func latestPayloadDT(payload map[string]any) (map[string]int64, bool) {
	if payload == nil {
		return nil, false
	}
	switch raw := payload["dt"].(type) {
	case map[string]int64:
		return raw, true
	case map[string]any:
		out := make(map[string]int64, len(raw))
		for key, value := range raw {
			out[key] = anyInt64(value)
		}
		return out, len(out) > 0
	}
	return nil, false
}

func qualityFromRecord(record Record) string {
	quality := qualityFromAny(record.Quality)
	for _, key := range []string{metricQualityColumn, "quality", "_qos"} {
		if raw, ok := record.RawValues[key]; ok {
			quality = qualityFromAny(raw)
			break
		}
	}
	return worseQuality(quality, qualityFromAny(record.SourceQuality))
}

func qualityCode(value any) int64 {
	switch qualityFromAny(value) {
	case qualityUncertain:
		return 2
	case qualityBad:
		return 3
	default:
		return 1
	}
}

func worseQuality(left, right string) string {
	if qualityCode(right) > qualityCode(left) {
		return qualityFromAny(right)
	}
	return qualityFromAny(left)
}

func qualityFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return qualityGood
	case string:
		return normalizeQuality(v)
	case []byte:
		return normalizeQuality(string(v))
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return qualityFromNumber(n)
		}
		return normalizeQuality(v.String())
	case int:
		return qualityFromNumber(int64(v))
	case int8:
		return qualityFromNumber(int64(v))
	case int16:
		return qualityFromNumber(int64(v))
	case int32:
		return qualityFromNumber(int64(v))
	case int64:
		return qualityFromNumber(v)
	case uint:
		return qualityFromNumber(int64(v))
	case uint8:
		return qualityFromNumber(int64(v))
	case uint16:
		return qualityFromNumber(int64(v))
	case uint32:
		return qualityFromNumber(int64(v))
	case uint64:
		return qualityFromNumber(int64(v))
	case float32:
		return qualityFromNumber(int64(v))
	case float64:
		return qualityFromNumber(int64(v))
	default:
		return normalizeQuality(fmt.Sprint(v))
	}
}

func normalizeQuality(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "1", strings.ToLower(qualityGood):
		return qualityGood
	case "2", strings.ToLower(qualityUncertain):
		return qualityUncertain
	case "3", strings.ToLower(qualityBad):
		return qualityBad
	default:
		return qualityGood
	}
}

func qualityFromNumber(value int64) string {
	switch value {
	case 2:
		return qualityUncertain
	case 3:
		return qualityBad
	default:
		return qualityGood
	}
}

func anyInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int16:
		return int64(v)
	case int8:
		return int64(v)
	case uint:
		return int64(v)
	case uint64:
		return int64(v)
	case uint32:
		return int64(v)
	case uint16:
		return int64(v)
	case uint8:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}
