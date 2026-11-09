package repo

import "testing"

// Launchpad 的 App 图标与封面按 App 取图：/api/core/launchpad/apps/:appId/{icon,cover}。
// operator 角色只持有 launchpad.view(620)，这两条 action 一旦从 620 上掉了，图片会静默
// 403 破图（前端只看到图裂，不报错）；而反过来若把不做归属校验的
// /api/core/assets/:fileId/download 挂回 620，operator 就能按 id 猜测下载任意资产。
// 两个方向都在这里钉住。
const (
	launchpadAppIconAction      = "/api/core/launchpad/apps/:appId/icon"
	launchpadAppCoverAction     = "/api/core/launchpad/apps/:appId/cover"
	launchpadShortcutLinkAction = "/api/core/launchpad/apps/:appId/shortcut-icon-link"
	genericAssetDownloadAction  = "/api/core/assets/:fileId/download"
	launchpadViewResourceID     = int64(620)
)

func TestLaunchpadViewGrantsScopedAppMedia(t *testing.T) {
	for _, actionValue := range []string{launchpadAppIconAction, launchpadAppCoverAction, launchpadShortcutLinkAction} {
		var found bool
		for _, action := range seedActions() {
			if action.ActionType != "api" || action.ActionValue != actionValue {
				continue
			}
			if action.ResourceID != launchpadViewResourceID {
				t.Fatalf("%s bound to resource %d, want %d (launchpad.view)",
					actionValue, action.ResourceID, launchpadViewResourceID)
			}
			if action.Methods != "GET" {
				t.Fatalf("%s methods = %q, want GET", actionValue, action.Methods)
			}
			found = true
		}
		if !found {
			t.Fatalf("seed is missing api action %q on launchpad.view; operator 会在 Launchpad 看到破图", actionValue)
		}
	}
}

func TestLaunchpadViewDoesNotGrantGenericAssetDownload(t *testing.T) {
	for _, action := range seedActions() {
		if action.ActionType == "api" && action.ActionValue == genericAssetDownloadAction &&
			action.ResourceID == launchpadViewResourceID {
			t.Fatalf("launchpad.view 不能挂 %s：该端点不做归属校验，等于放开按 id 下载任意资产",
				genericAssetDownloadAction)
		}
	}
}

// 存量库里 PR #525 已经种下过那条宽授权，必须显式停用，光从 seed 列表里删掉不会回收。
func TestGenericAssetDownloadActionIsRetired(t *testing.T) {
	var retired bool
	for _, value := range retiredSeedActionValues() {
		if value == genericAssetDownloadAction {
			retired = true
			break
		}
	}
	if !retired {
		t.Fatalf("%s 必须列入 retiredActionValues，否则存量部署的宽授权不会被停用", genericAssetDownloadAction)
	}
}

func TestLaunchpadMediaActionsMatchRealPaths(t *testing.T) {
	if !pathMatches(launchpadAppIconAction, "/api/core/launchpad/apps/1234/icon") {
		t.Fatalf("pathMatches(%q, real icon path) = false", launchpadAppIconAction)
	}
	if !pathMatches(launchpadAppCoverAction, "/api/core/launchpad/apps/1234/cover") {
		t.Fatalf("pathMatches(%q, real cover path) = false", launchpadAppCoverAction)
	}
	if !pathMatches(launchpadShortcutLinkAction, "/api/core/launchpad/apps/1234/shortcut-icon-link") {
		t.Fatalf("pathMatches(%q, real shortcut link path) = false", launchpadShortcutLinkAction)
	}
	// 图标端点不能顺带匹配到 App 的其它子路径（例如记录打开的 view、换取取图地址）
	if pathMatches(launchpadAppIconAction, "/api/core/launchpad/apps/1234/view") {
		t.Fatalf("icon action should not match the view path")
	}
	if pathMatches(launchpadAppIconAction, "/api/core/launchpad/apps/1234/shortcut-icon-link") {
		t.Fatalf("icon action should not match the shortcut link path")
	}
	if !methodMatches("GET", "GET") {
		t.Fatalf("methodMatches(GET, GET) = false")
	}
}
