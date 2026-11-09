package launchpad

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// 移动端把应用「添加到主屏幕」后，桌面图标是 iOS Safari 自己去抓页面的
// apple-touch-icon 得来的。Safari 里没有本站登录态，鉴权的
// /launchpad/apps/{id}/icon 必然 401，桌面上就只剩一张网页截图。
//
// 解法是把授权凭证从「会话」换成「短期签名」：仍然只有通过可见性校验的用户能
// 拿到取图地址（ShortcutIconLink），但地址本身带服务端签名，公开端点只认签名。
// 这样既不用像 Cloud 那样把图标端点整体开成匿名可按 id 枚举（正是
// launchpadAppMediaURL 注释里刻意避免的），也不用把会话令牌塞进 URL。
const (
	// shortcutIconTTL 是取图地址的有效期。iOS 只在「添加到主屏幕」那一刻抓一次图，
	// 用户从打开引导页到点完通常在分钟级；一天足够宽松，也不会留下长期公开地址。
	shortcutIconTTL = 24 * time.Hour

	shortcutIconSignScope = "launchpad-shortcut-icon"
)

// SetShortcutIconSecret 注入签名密钥（bootstrap 在构造后补注入，与 SetAppGatewayConfig 一致）。
// 未注入时不签发地址，公开端点也一律拒绝。
func (s *Service) SetShortcutIconSecret(secret string) {
	if s != nil {
		s.shortcutIconSecret = secret
	}
}

// ShortcutIconLink 给调用者签发一条短期公开取图地址。
// App 对调用者不可见或没有图标时按 AppMediaAssetID 的口径返回错误（403/404）。
func (s *Service) ShortcutIconLink(ctx context.Context, userID, appID int64) (string, error) {
	if _, err := s.AppMediaAssetID(ctx, userID, appID, MediaKindIcon); err != nil {
		return "", err
	}
	if s.shortcutIconSecret == "" {
		return "", ErrNotFound
	}
	expires := time.Now().Add(shortcutIconTTL).Unix()
	return fmt.Sprintf(
		"/api/core/launchpad/shortcut-icons/%d?expires=%d&signature=%s",
		appID, expires, signShortcutIcon(s.shortcutIconSecret, appID, expires),
	), nil
}

// ShortcutIconAssetID 校验签名并返回图标资产 id。签名本身就是授权凭证，所以这里
// 不看调用者身份。任何一项不过（密钥缺失、过期、签名不符、App 不存在或无图标）
// 都返回 ErrNotFound：对匿名调用者不区分原因，避免透出「这个 id 的 App 存在」。
func (s *Service) ShortcutIconAssetID(ctx context.Context, appID, expires int64, signature string) (int64, error) {
	if s == nil || s.shortcutIconSecret == "" || appID <= 0 {
		return 0, ErrNotFound
	}
	if expires <= 0 || time.Now().Unix() > expires {
		return 0, ErrNotFound
	}
	want := signShortcutIcon(s.shortcutIconSecret, appID, expires)
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return 0, ErrNotFound
	}
	app, err := s.projects.GetProjectAppByID(ctx, appID)
	if err != nil || app.IconAssetID <= 0 {
		return 0, ErrNotFound
	}
	return app.IconAssetID, nil
}

func signShortcutIcon(secret string, appID, expires int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(shortcutIconSignScope + ":" +
		strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(expires, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}
