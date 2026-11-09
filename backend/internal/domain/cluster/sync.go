package cluster

import (
	"encoding/json"

	"net/http"
	"sort"
	"strconv"
	"strings"

	"backend/internal/repo"
)

func jsonInt64Array(raw json.RawMessage) []int64 {
	var values []int64
	_ = json.Unmarshal(raw, &values)
	return values
}

func jsonStringArray(raw json.RawMessage) []string {
	var values []string
	_ = json.Unmarshal(raw, &values)
	if values == nil {
		return []string{}
	}
	return values
}

func stringArrayFromInts(values []int64) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatInt(value, 10))
	}
	return out
}

func equalInt64Slices(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func canonicalInt64s(values []int64) []int64 {
	canonical := append([]int64(nil), values...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i] < canonical[j] })
	return canonical
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func splitCSV(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func cloneBytes(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

// syncStatusAuditCode 将同步行状态映射为 direct 审计的 HTTP 风格结果码:
// failed 记失败,partial 记 accepted,其余(含 success/skipped)记成功。
func syncStatusAuditCode(status string) int {
	switch status {
	case repo.CloudSyncLogStatusFailed:
		return http.StatusInternalServerError
	case repo.CloudSyncLogStatusPartial:
		return http.StatusAccepted
	default:
		return http.StatusOK
	}
}
