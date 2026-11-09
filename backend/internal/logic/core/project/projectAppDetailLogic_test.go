// 本文件覆盖 projectAppDetailLogic.go 的 ProjectAppDetail 及其纯函数
// （manifestAssetID / projectAppDetailIconURL / deployStatusForDetail）：
// 正常路径：手动应用（deployStatus=success）、deploying/failed/stopped 状态映射、图标/封面 URL；
// 异常情况：应用不存在 → 404。
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// 验证跨项目并发占用、覆盖部署期间双重预占，以及失败回滚释放新别名。
func TestProjectAppNetworkAliasLifecycle(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	repoLayer := repo.NewProjectRepo(db)
	p1 := createTestProject(t, svcCtx, ctx, "alias-one")
	p2 := createTestProject(t, svcCtx, ctx, "alias-two")
	makeApp := func(n int, projectID int64) repo.ProjectApp {
		return repo.ProjectApp{ProjectID: projectID, AppKey: fmt.Sprintf("alias-%d", n), Name: fmt.Sprintf("alias-%d", n), SiteType: "dynamic", Status: "active", ManifestJSON: json.RawMessage(`{"container_port":3000}`)}
	}
	type result struct {
		app repo.ProjectApp
		err error
	}
	results := make(chan result, 6)
	for i := 0; i < 6; i++ {
		go func(n int) {
			projectID := p1
			if n%2 == 0 {
				projectID = p2
			}
			app, err := repoLayer.CreateProjectAppWithNetworkAlias(ctx, makeApp(n, projectID), "order-api")
			results <- result{app, err}
		}(i)
	}
	var winner repo.ProjectApp
	success := 0
	for i := 0; i < 6; i++ {
		r := <-results
		if r.err == nil {
			winner = r.app
			success++
		} else {
			require.ErrorIs(t, r.err, repo.ErrAppNetworkAliasTaken)
		}
	}
	require.Equal(t, 1, success)
	snapshot := append(json.RawMessage(nil), winner.ManifestJSON...)
	require.NoError(t, markAppReplaceDeploying(ctx, repoLayer, winner.ProjectID, winner.ID, "active", 1, "orders-v2"))
	for _, alias := range []string{"order-api", "orders-v2"} {
		_, err := repoLayer.CreateProjectAppWithNetworkAlias(ctx, makeApp(10, p1), alias)
		require.ErrorIs(t, err, repo.ErrAppNetworkAliasTaken)
	}
	require.NoError(t, restoreReplaceSnapshot(ctx, repoLayer, winner.ProjectID, winner.ID, snapshot, 0, "test rollback", 1))
	require.NoError(t, repoLayer.UpdateProjectAppStatus(ctx, winner.ProjectID, winner.ID, "active", 1))
	_, err := repoLayer.CreateProjectAppWithNetworkAlias(ctx, makeApp(11, p1), "orders-v2")
	require.NoError(t, err)
	// 原别名属于同一 App 时允许覆盖部署；其他 App 仍不可抢占。
	require.NoError(t, markAppReplaceDeploying(ctx, repoLayer, winner.ProjectID, winner.ID, "active", 1, "order-api"))
	err = markAppReplaceDeploying(ctx, repoLayer, winner.ProjectID, winner.ID, "active", 1, "order-api")
	require.Error(t, err)
	require.False(t, errors.Is(err, repo.ErrAppNetworkAliasTaken))
	resp, err := NewProjectAppDetailLogic(ctx, svcCtx).ProjectAppDetail(&types.ProjectAppReq{ProjectId: winner.ProjectID, AppId: winner.ID})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, "order-api", data["networkAlias"])
	require.Equal(t, "http://order-api:3000", data["internalUrl"])
}

// TestProjectAppDetailLogic_ManifestVariants 通过 DB 直插不同 manifest 的应用，验证详情响应。
func TestProjectAppDetailLogic_ManifestVariants(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	projectID := createTestProject(t, svcCtx, ctx, "detail-variants")

	insertApp := func(name, siteType, status string, manifest map[string]any) repo.ProjectApp {
		t.Helper()
		raw, err := json.Marshal(manifest)
		require.NoError(t, err)
		app := testkit.Insert(t, db, &repo.ProjectApp{
			ProjectID:    projectID,
			AppKey:       "key-" + name,
			Name:         name,
			SiteType:     siteType,
			ManifestJSON: raw,
			Status:       status,
		})
		return *app
	}

	// 手动应用 → manual=true, deployStatus=success
	manual := insertApp("manual-app", "manualExternal", "active", map[string]any{
		"app_type": "manual", "openInPlatform": true, "iconAssetId": float64(0),
	})
	// 部署中 → deploying
	deploying := insertApp("deploying-app", "sameOriginStatic", "active", map[string]any{"deploying": true})
	// 部署失败 → failed
	failed := insertApp("failed-app", "sameOriginStatic", "active", map[string]any{"deploy_error": "boom"})
	// 已停止 → stopped
	stopped := insertApp("stopped-app", "sameOriginStatic", "stopped", map[string]any{})
	// 图标/封面 → URL 拼装
	iconed := insertApp("iconed-app", "sameOriginStatic", "active", map[string]any{"coverAssetId": float64(42)})

	detail := func(appID int64) map[string]any {
		t.Helper()
		resp, err := NewProjectAppDetailLogic(ctx, svcCtx).ProjectAppDetail(&types.ProjectAppReq{
			ProjectId: projectID, AppId: appID,
		})
		require.NoError(t, err)
		return resp.Data.(map[string]any)
	}

	d := detail(manual.ID)
	require.NotEmpty(t, manual.AppPrincipalID)
	require.Equal(t, manual.AppPrincipalID, d["appPrincipalId"])
	require.NotEqual(t, manual.AppPrincipalID, detail(deploying.ID)["appPrincipalId"])
	require.Equal(t, true, d["manual"])
	require.Equal(t, "success", d["deployStatus"])
	require.Equal(t, "manual", d["appType"])
	require.Equal(t, true, d["openInPlatform"])

	require.Equal(t, "deploying", detail(deploying.ID)["deployStatus"])
	require.Equal(t, "failed", detail(failed.ID)["deployStatus"])
	require.Equal(t, "boom", detail(failed.ID)["deployError"])
	require.Equal(t, "stopped", detail(stopped.ID)["deployStatus"])

	di := detail(iconed.ID)
	require.Equal(t, int64(42), di["coverAssetId"])
	require.Equal(t, "/api/core/assets/42/download", di["coverUrl"])
	require.Equal(t, "", di["iconUrl"])

	// 无 volumes 持久化 → 返回空数组而非 null（Replace 弹窗预填约定）
	require.Equal(t, []artifactVolume{}, d["volumes"])

	// 已持久化磁盘映射 → 详情原样返回
	withVolumes := insertApp("volumes-app", "dynamic", "active", map[string]any{
		"volumes": []any{map[string]any{"hostPath": "/srv/data", "containerPath": "/data", "readOnly": true}},
	})
	dv := detail(withVolumes.ID)["volumes"].([]artifactVolume)
	require.Len(t, dv, 1)
	require.Equal(t, "/srv/data", dv[0].HostPath)
	require.Equal(t, "/data", dv[0].ContainerPath)
	require.Equal(t, true, dv[0].ReadOnly)
}

func TestProjectAppDetailLogic_NotExist(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	_, err := NewProjectAppDetailLogic(ctx, svcCtx).ProjectAppDetail(&types.ProjectAppReq{
		ProjectId: 999999, AppId: 999999,
	})
	require.Error(t, err)
	require.Equal(t, 404, httpErrStatus(err))
}

func TestManifestAssetID_Behavior(t *testing.T) {
	manifest := map[string]any{
		"a": float64(3), "b": int64(4), "c": 5, "d": "6", "e": "not-a-number", "f": nil,
	}
	if got := manifestAssetID(manifest, "a"); got != 3 {
		t.Errorf("float64 = %d, want 3", got)
	}
	if got := manifestAssetID(manifest, "b"); got != 4 {
		t.Errorf("int64 = %d, want 4", got)
	}
	if got := manifestAssetID(manifest, "c"); got != 5 {
		t.Errorf("int = %d, want 5", got)
	}
	if got := manifestAssetID(manifest, "d"); got != 6 {
		t.Errorf("string = %d, want 6", got)
	}
	if got := manifestAssetID(manifest, "e"); got != 0 {
		t.Errorf("invalid string = %d, want 0", got)
	}
	if got := manifestAssetID(manifest, "f"); got != 0 {
		t.Errorf("nil = %d, want 0", got)
	}
	if got := manifestAssetID(manifest, "missing"); got != 0 {
		t.Errorf("missing = %d, want 0", got)
	}
}

func TestProjectAppDetailIconURL_Behavior(t *testing.T) {
	if got := projectAppDetailIconURL(0); got != "" {
		t.Errorf("iconAssetID 0 = %q, want empty", got)
	}
	if got := projectAppDetailIconURL(-1); got != "" {
		t.Errorf("iconAssetID -1 = %q, want empty", got)
	}
	if got := projectAppDetailIconURL(77); got != "/api/core/assets/77/download" {
		t.Errorf("iconAssetID 77 = %q", got)
	}
}

func TestDeployStatusForDetail_Behavior(t *testing.T) {
	if got := deployStatusForDetail("active", true, ""); got != "deploying" {
		t.Errorf("deploying = %q, want deploying", got)
	}
	if got := deployStatusForDetail("active", false, "err"); got != "failed" {
		t.Errorf("deploy_error = %q, want failed", got)
	}
	if got := deployStatusForDetail("stopped", false, ""); got != "stopped" {
		t.Errorf("stopped = %q, want stopped", got)
	}
	if got := deployStatusForDetail("failed", false, ""); got != "failed" {
		t.Errorf("failed = %q, want failed", got)
	}
	if got := deployStatusForDetail("active", false, ""); got != "success" {
		t.Errorf("active = %q, want success", got)
	}
	if got := deployStatusForDetail("deploying", false, ""); got != "success" {
		t.Errorf("deploying without flag = %q, want success", got)
	}
	if got := deployStatusForDetail("disabled", false, ""); got != "pending" {
		t.Errorf("disabled = %q, want pending", got)
	}
}
