package asset

// 测试目标：active content type 判定（content_type.go）。
// 覆盖的正常路径：HTML/XML/SVG/JS/ECMAScript 等可执行类型判定为 active；图片/PDF/纯文本
// 判定为 inert；*+xml 变体判定为 active；带 charset 参数时忽略参数。
// 覆盖的异常情况：空串、非法 MIME、大写混合输入。

import (
	"testing"
)

func TestIsActiveContentType(t *testing.T) {
	active := []string{
		"text/html",
		"text/html; charset=utf-8",
		"application/xhtml+xml",
		"image/svg+xml",
		"text/xml",
		"application/xml",
		"application/javascript",
		"application/x-javascript",
		"text/javascript",
		"application/ecmascript",
		"text/ecmascript",
		"application/atom+xml",
		"application/rss+xml",
		"TEXT/HTML",
	}
	for _, contentType := range active {
		if !IsActiveContentType(contentType) {
			t.Fatalf("IsActiveContentType(%q) = false, want true", contentType)
		}
	}

	inert := []string{
		"",
		"image/png",
		"image/jpeg",
		"application/pdf",
		"text/plain",
		"text/plain; charset=utf-8",
		"application/octet-stream",
		"video/mp4",
		"  ",
	}
	for _, contentType := range inert {
		if IsActiveContentType(contentType) {
			t.Fatalf("IsActiveContentType(%q) = true, want false", contentType)
		}
	}
}
