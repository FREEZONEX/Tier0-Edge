package project

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
)

// validPNGBytes 生成一张 1x1 有效 PNG（供魔数/解码校验用例）。
func validPNGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// validJPEGBytes 生成一张 1x1 有效 JPEG（供魔数/解码校验用例）。
func validJPEGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// minimalWebPHeader 生成 DetectContentType 可识别为 image/webp 的最小头部
// （嗅探要求 RIFF + size≥4 + WEBPVP 前缀）。
func minimalWebPHeader() []byte {
	raw := []byte("RIFF")
	raw = append(raw, 4, 0, 0, 0) // RIFF size 字段
	raw = append(raw, []byte("WEBPVP")...)
	return raw
}

// TestValidatePackageMedia_RejectsFakeContent 覆盖包内媒体伪造内容校验：
// png/jpeg/webp/svg 与扩展名不符或内容非法时一律拒绝导入，不静默接受。
func TestValidatePackageMedia_RejectsFakeContent(t *testing.T) {
	pngBytes := validPNGBytes(t)
	jpegBytes := validJPEGBytes(t)
	validSVG := []byte(`<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)

	cases := []struct {
		name string
		file string
		data []byte
		want bool
	}{
		{"png with real png", "icon.png", pngBytes, true},
		{"jpg with real jpeg", "icon.jpg", jpegBytes, true},
		{"jpeg with real jpeg", "icon.jpeg", jpegBytes, true},
		{"webp with webp header", "icon.webp", minimalWebPHeader(), true},
		{"svg with svg text", "icon.svg", validSVG, true},
		// 伪造内容：扩展名与真实内容不符
		{"png with jpeg content", "icon.png", jpegBytes, false},
		{"jpg with png content", "icon.jpg", pngBytes, false},
		{"webp with png content", "icon.webp", pngBytes, false},
		// 完全垃圾内容
		{"png with garbage", "icon.png", []byte("not an image at all, definitely fake"), false},
		{"jpg with empty", "icon.jpg", nil, false},
		// svg 二进制/无 <svg 标签
		{"svg with binary", "icon.svg", []byte{0x89, 0x50, 0x00, 0x01}, false},
		{"svg without svg tag", "icon.svg", []byte("hello world"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePackageMedia(tc.file, tc.data)
			if tc.want && err != nil {
				t.Fatalf("validatePackageMedia(%q) = %v, want nil", tc.file, err)
			}
			if !tc.want && err == nil {
				t.Fatalf("validatePackageMedia(%q) = nil, want error", tc.file)
			}
		})
	}
}

// TestValidatePackageMedia_RejectsOversize 覆盖包内单媒体文件 20MiB 上限。
func TestValidatePackageMedia_RejectsOversize(t *testing.T) {
	oversize := make([]byte, 20<<20+1)
	if err := validatePackageMedia("icon.png", oversize); err == nil {
		t.Fatal("expected error for media file exceeding 20MiB, got nil")
	} else if !strings.Contains(err.Error(), "20MiB") {
		t.Fatalf("error = %v, want readable 20MiB limit message", err)
	}
}

// TestIsSVGContent 覆盖 svg 文本基本校验的边界。
func TestIsSVGContent(t *testing.T) {
	if !isSVGContent([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)) {
		t.Error("expected plain <svg> text to pass")
	}
	if !isSVGContent([]byte(`<?xml version="1.0"?><!DOCTYPE svg><SVG width="10"></SVG>`)) {
		t.Error("expected case-insensitive <svg> match to pass")
	}
	if isSVGContent(nil) {
		t.Error("expected nil to fail")
	}
	if isSVGContent([]byte{0x00, 0x01}) {
		t.Error("expected binary content (NUL byte) to fail")
	}
	if isSVGContent([]byte("plain text without svg")) {
		t.Error("expected text without <svg> to fail")
	}
}

// TestMapDeployCASError 覆盖 repo 层 CAS 并发冲突到领域层可读错误的映射。
func TestMapDeployCASError(t *testing.T) {
	if err := mapDeployCASError(repo.ErrAppDeployInProgress); !errors.Is(err, domainproject.ErrAppReplaceInProgress) {
		t.Fatalf("mapDeployCASError(ErrAppDeployInProgress) = %v, want ErrAppReplaceInProgress", err)
	}
	if err := mapDeployCASError(nil); err != nil {
		t.Fatalf("mapDeployCASError(nil) = %v, want nil", err)
	}
	other := errors.New("boom")
	if err := mapDeployCASError(other); !errors.Is(err, other) {
		t.Fatalf("mapDeployCASError(other) = %v, want unchanged error", err)
	}
}
