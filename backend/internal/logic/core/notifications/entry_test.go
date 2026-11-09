package notifications

import (
	"testing"

	domainnotify "backend/internal/domain/notify"
	"backend/internal/repo"
)

// entryFromItem 曾经逐字段重建 NotificationEntry，domain 上新增的字段会被静默丢掉
// （sourceIconUrl 就这么丢过一次，两端都不报错，只能靠线上抓包才发现）。
// 这里钉住：来源应用解析得出时，图标地址必须一路透到响应里。
func TestEntryFromItemKeepsSourceIconURL(t *testing.T) {
	item := repo.UserMessageItem{}
	item.Link.ID = 104
	item.Message.ID = 1
	item.Message.SenderType = domainnotify.SenderTypeApp
	item.Source.SourceType = domainnotify.SenderTypeApp
	item.Source.SourceID = "docker-app-uuid"
	item.Source.SourceName = "项目通知台"
	item.Source.AppID = 66
	item.Source.ProjectID = 16

	got := entryFromItem(item)
	if want := "/api/core/notify/user-message/104/sender-icon"; got.SourceIconURL != want {
		t.Fatalf("SourceIconURL = %q, want %q", got.SourceIconURL, want)
	}
	if got.SourceName != "项目通知台" {
		t.Fatalf("SourceName = %q", got.SourceName)
	}
}

// 平台通知没有应用,不该给出图标地址。
func TestEntryFromItemOmitsIconForPlatform(t *testing.T) {
	item := repo.UserMessageItem{}
	item.Link.ID = 105
	item.Message.ID = 2
	item.Source.SourceType = "platform"
	if got := entryFromItem(item); got.SourceIconURL != "" {
		t.Fatalf("SourceIconURL = %q, want empty", got.SourceIconURL)
	}
}
