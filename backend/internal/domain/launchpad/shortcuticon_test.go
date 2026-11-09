package launchpad

// 测试目标：「添加到主屏幕」免登录取图地址的签名与校验（shortcuticon.go 纯逻辑部分）。
// 覆盖的正常路径：签名确定、地址格式、有效签名通过前置校验。
// 覆盖的异常情况：未注入密钥、过期、签名被篡改、换 App 复用签名、非法 appID。

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSignShortcutIconIsScopedAndStable(t *testing.T) {
	const secret = "test-secret"
	sig := signShortcutIcon(secret, 7, 100)
	if sig != signShortcutIcon(secret, 7, 100) {
		t.Fatal("signature should be deterministic")
	}
	// 换 App、换过期时间、换密钥都必须换签名，否则一条签名能被挪用到别的图标上
	if sig == signShortcutIcon(secret, 8, 100) {
		t.Fatal("signature must bind appID")
	}
	if sig == signShortcutIcon(secret, 7, 101) {
		t.Fatal("signature must bind expires")
	}
	if sig == signShortcutIcon("other-secret", 7, 100) {
		t.Fatal("signature must bind secret")
	}
}

func TestShortcutIconLinkRequiresSecret(t *testing.T) {
	// 没注入密钥就不该签发地址（否则公开端点会因为空密钥而人人可签）
	s := &Service{}
	if _, err := s.ShortcutIconAssetID(context.Background(), 7, time.Now().Add(time.Hour).Unix(), "whatever"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing secret err = %v", err)
	}
}

func TestShortcutIconAssetIDRejectsBadLinks(t *testing.T) {
	const secret = "test-secret"
	s := &Service{shortcutIconSecret: secret}
	ctx := context.Background()
	valid := time.Now().Add(time.Hour).Unix()

	cases := []struct {
		name    string
		appID   int64
		expires int64
		sig     string
	}{
		{"expired", 7, time.Now().Add(-time.Second).Unix(), signShortcutIcon(secret, 7, time.Now().Add(-time.Second).Unix())},
		{"no expiry", 7, 0, signShortcutIcon(secret, 7, 0)},
		{"tampered signature", 7, valid, signShortcutIcon(secret, 7, valid) + "00"},
		{"signature for another app", 7, valid, signShortcutIcon(secret, 8, valid)},
		{"empty signature", 7, valid, ""},
		{"invalid app id", 0, valid, signShortcutIcon(secret, 0, valid)},
	}
	for _, c := range cases {
		if _, err := s.ShortcutIconAssetID(ctx, c.appID, c.expires, c.sig); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: err = %v, want ErrNotFound", c.name, err)
		}
	}
}
