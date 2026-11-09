package notify

import (
	"testing"

	"backend/internal/domain/notify"
)

// 发信应用身份的下发口径：sender.id 是 SDK 自报的 docker app UUID，客户端反查不到
// 应用;senderAppId / senderIconUrl 才是可用的,解析不到时必须是空串而不是 "0" 或
// 一条指向 0 号应用的地址——前者会让客户端把它当成合法 id 去请求。
func TestIDOrEmptyOmitsUnresolved(t *testing.T) {
	for _, appID := range []int64{0, -1} {
		if got := idOrEmpty(appID); got != "" {
			t.Fatalf("idOrEmpty(%d) = %q, want empty", appID, got)
		}
		if got := notify.SenderIconURL(104, appID); got != "" {
			t.Fatalf("notify.SenderIconURL(104, %d) = %q, want empty", appID, got)
		}
	}
	// 没有 linkId 同样取不到图：那条路由按「这条消息是我的」授权,缺了它无从校验。
	if got := notify.SenderIconURL(0, 39); got != "" {
		t.Fatalf("notify.SenderIconURL(0, 39) = %q, want empty", got)
	}
}

func TestIDAndIconForResolvedApp(t *testing.T) {
	if got := idOrEmpty(39); got != "39" {
		t.Fatalf("idOrEmpty(39) = %q", got)
	}
	// 必须落在按消息授权的取图端点上。不能用 Launchpad 的 /apps/{id}/icon：
	// 收件人未必是发信应用所在项目的成员,那条会 403,收件人只能看到通用图形。
	if got := notify.SenderIconURL(104, 39); got != "/api/core/notify/user-message/104/sender-icon" {
		t.Fatalf("notify.SenderIconURL(104, 39) = %q", got)
	}
}
