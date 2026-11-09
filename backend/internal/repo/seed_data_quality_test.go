package repo

import "testing"

// 测试目标：数据质量 Core API 均有独立读/写权限资源，避免 Permission 对管理员也返回 403。
// 正常路径：七条 API action 全部存在；异常路径：写操作不得绑定到只读资源。
func TestSeedDataQualityPermissions(t *testing.T) {
	resources := map[int64]string{}
	for _, resource := range seedResources() {
		resources[resource.ID] = resource.ResourceKey
	}
	if resources[217] != "uns.dataQuality.read" || resources[218] != "uns.dataQuality.manage" {
		t.Fatalf("data quality resources missing: read=%q manage=%q", resources[217], resources[218])
	}
	want := map[string]int64{
		"GET /api/core/uns/data-quality/policies/:unsId": 217,
		"GET /api/core/uns/data-quality/status":          217,
		"GET /api/core/uns/data-quality/active-summary":  217,
		"GET /api/core/uns/data-quality/issues":          217,
		"PUT /api/core/uns/data-quality/policies/:unsId": 218,
		"POST /api/core/uns/data-quality/runs/:unsId":    218,
		"POST /api/core/uns/data-quality/issues/:id/ack": 218,
	}
	for _, action := range seedActions() {
		key := action.Methods + " " + action.ActionValue
		if resourceID, ok := want[key]; ok {
			if action.ResourceID != resourceID {
				t.Fatalf("action %s resource=%d, want %d", key, action.ResourceID, resourceID)
			}
			delete(want, key)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing data quality actions: %#v", want)
	}
}
