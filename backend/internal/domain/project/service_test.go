package project

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"backend/internal/repo"
)

// TestFilterUnsharedResourceIDs 删除 App 清理资源时：
// 仍被其他存活 App 引用的 ID 与保留 ID（项目 UNS 根）不删，其余按原顺序返回。
func TestFilterUnsharedResourceIDs(t *testing.T) {
	shared := map[int64]struct{}{22: {}, 33: {}}
	got := filterUnsharedResourceIDs([]int64{11, 22, 0, 33, 44}, shared, 44)
	if len(got) != 1 || got[0] != 11 {
		t.Errorf("filterUnsharedResourceIDs = %v, want [11]（剔除共享 22/33、保留 44、无效 0）", got)
	}
	if got := filterUnsharedResourceIDs(nil, shared); len(got) != 0 {
		t.Errorf("filterUnsharedResourceIDs(nil) = %v, want empty", got)
	}
}

// TestReferencedResourceIDs 统计其他 App manifest 的资源引用：排除被删 App 自身，
// 无 manifest 或缺键的 App 不产生引用。
func TestReferencedResourceIDs(t *testing.T) {
	manifest := func(v map[string]any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		return raw
	}
	apps := []repo.ProjectApp{
		{ID: 1, ManifestJSON: manifest(map[string]any{"flowIds": []any{101.0, 102.0}, "unsNodeIds": []any{201.0}})},
		{ID: 2, ManifestJSON: manifest(map[string]any{"flowIds": []any{102.0, 103.0}, "unsNodeIds": []any{202.0}})},
		{ID: 3}, // 无 manifest
	}
	flowIDs, unsNodeIDs := referencedResourceIDs(apps, 1)
	if _, ok := flowIDs[101]; ok {
		t.Errorf("flow 101 仅被被删 App 自身引用，不应计入共享集合")
	}
	if _, ok := flowIDs[102]; !ok {
		t.Errorf("flow 102 被 App 2 引用，应计入共享集合")
	}
	if _, ok := flowIDs[103]; !ok {
		t.Errorf("flow 103 被 App 2 引用，应计入共享集合")
	}
	if _, ok := unsNodeIDs[201]; ok {
		t.Errorf("uns 201 仅被被删 App 自身引用，不应计入共享集合")
	}
	if _, ok := unsNodeIDs[202]; !ok {
		t.Errorf("uns 202 被 App 2 引用，应计入共享集合")
	}
}

// TestCleanupPreferCreated 校验删 App 清理资源时的集合选择：
// created 字段存在（含空数组，表示本 App 未新建任何资源）时只删 created，
// 仅当字段缺失（旧 manifest）才回退全量引用集合。
func TestCleanupPreferCreated(t *testing.T) {
	if got := cleanupPreferCreated([]int64{101}, []int64{101, 102}); !slices.Equal(got, []int64{101}) {
		t.Errorf("cleanupPreferCreated(created) = %v, want [101]（只删本 App 创建的）", got)
	}
	// created 空数组 ≠ 缺失：本 App 未新建任何资源，删除集合应为空，不误删全量引用
	if got := cleanupPreferCreated([]int64{}, []int64{101, 102}); len(got) != 0 {
		t.Errorf("cleanupPreferCreated(empty) = %v, want 空集合（created 字段存在但本 App 未新建）", got)
	}
	if got := cleanupPreferCreated(nil, []int64{101, 102}); !slices.Equal(got, []int64{101, 102}) {
		t.Errorf("cleanupPreferCreated(nil) = %v, want 回退全量引用 [101 102]（旧 manifest）", got)
	}
}

// TestCleanupAppImportedResources_CreatedPreference 校验删 App 清理优先 createdFlowIds：
// 本 App 创建的 Flow 在删 App 时被清理；被顶层 id 命中匹配（非 created）且被其他 App
// 引用的既有 Flow 保留，不误删。
func TestCleanupAppImportedResources_CreatedPreference(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}
	const (
		projectID      = int64(900005)
		otherProjectID = int64(900008)
		appAID         = int64(900006)
		appBID         = int64(900007)
		appCID         = int64(900009)
		appDID         = int64(900010)
		flowA          = int64(900101) // 本 App 创建
		flowB          = int64(900102) // 命中匹配的既有资源，且被另一 Project 的 App B 引用
		flowC          = int64(900103) // createdFlowIds 空数组（本 App 未新建）的 App 引用的既有资源
		flowD          = int64(900104) // 旧 manifest（无 created 字段）引用的资源
		userID         = int64(7)
	)
	cleanup := func() {
		_ = db.Exec("DELETE FROM project_app WHERE id IN (?, ?, ?, ?)", appAID, appBID, appCID, appDID).Error
		_ = db.Exec("DELETE FROM project WHERE id IN (?, ?)", projectID, otherProjectID).Error
		_ = db.Exec("DELETE FROM uns_nodered_flow WHERE id IN (?, ?, ?, ?)", flowA, flowB, flowC, flowD).Error
	}
	cleanup()
	defer cleanup()
	if err := db.Exec("INSERT INTO project (id, project_key, name) VALUES (?, ?, ?)", projectID,
		fmt.Sprintf("cleanup-test-%d", time.Now().UnixNano()), "cleanup-test").Error; err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := db.Exec("INSERT INTO project (id, project_key, name) VALUES (?, ?, ?)", otherProjectID,
		fmt.Sprintf("cleanup-other-%d", time.Now().UnixNano()), "cleanup-other").Error; err != nil {
		t.Fatalf("insert other project: %v", err)
	}
	insertApp := func(id, projectID int64, manifest string) {
		if err := db.Exec("INSERT INTO project_app (id, project_id, app_key, name, manifest_json) VALUES (?, ?, ?, ?, ?::jsonb)",
			id, projectID, fmt.Sprintf("app_%d", id), fmt.Sprintf("app-%d", id), manifest).Error; err != nil {
			t.Fatalf("insert app %d: %v", id, err)
		}
	}
	// App A 的 flowIds 全量引用 [flowA, flowB]，但 createdFlowIds 只有 flowA（本 App 创建）；
	// 另一 Project 的 App B 也引用 flowB（跨 Project 命中匹配的既有资源）
	insertApp(appAID, projectID, `{"flowIds":[900101,900102],"createdFlowIds":[900101]}`)
	insertApp(appBID, otherProjectID, `{"flowIds":[900102]}`)
	// App C：createdFlowIds 空数组（本 App 未新建任何 Flow），引用的既有资源不得误删
	insertApp(appCID, projectID, `{"flowIds":[900103],"createdFlowIds":[]}`)
	// App D：旧 manifest 无 created 字段，回退全量引用（历史行为保留）
	insertApp(appDID, projectID, `{"flowIds":[900104]}`)
	insertFlow := func(id int64) {
		name := fmt.Sprintf("flow-%d", id)
		if err := db.Exec("INSERT INTO uns_nodered_flow (id, flow_id, flow_name, flow_type, node_type, parent_id) VALUES (?, ?, ?, 1, 2, 0)",
			id, name, name).Error; err != nil {
			t.Fatalf("insert flow %d: %v", id, err)
		}
	}
	insertFlow(flowA)
	insertFlow(flowB)
	insertFlow(flowC)
	insertFlow(flowD)

	s := &Service{
		projects: repo.NewProjectRepo(ctx),
		uns:      repo.NewUnsRepo(ctx),
		flows:    repo.NewFlowRepo(ctx),
	}
	appRepo := repo.NewProjectRepo(ctx)
	for _, appID := range []int64{appAID, appCID, appDID} {
		app, err := appRepo.GetProjectApp(ctx, projectID, appID)
		if err != nil {
			t.Fatalf("GetProjectApp %d: %v", appID, err)
		}
		if err := s.CleanupAppImportedResources(ctx, app, userID); err != nil {
			t.Fatalf("CleanupAppImportedResources(%d): %v", appID, err)
		}
	}
	deletedTimeOf := func(id int64) int64 {
		var deletedTime int64
		if err := db.Raw("SELECT deleted_time FROM uns_nodered_flow WHERE id = ?", id).Scan(&deletedTime).Error; err != nil {
			t.Fatalf("query flow %d: %v", id, err)
		}
		return deletedTime
	}
	if deletedTimeOf(flowA) == 0 {
		t.Errorf("flow %d 是本 App 创建的，删 App 时应被清理", flowA)
	}
	if deletedTimeOf(flowB) != 0 {
		t.Errorf("flow %d 是跨 Project 命中匹配的既有资源且被 App B 引用，不应被误删", flowB)
	}
	if deletedTimeOf(flowC) != 0 {
		t.Errorf("flow %d 的 App 声明 createdFlowIds 为空（本 App 未新建），不应被误删", flowC)
	}
	if deletedTimeOf(flowD) == 0 {
		t.Errorf("flow %d 的旧 manifest 无 created 字段，应回退全量引用清理", flowD)
	}
}
