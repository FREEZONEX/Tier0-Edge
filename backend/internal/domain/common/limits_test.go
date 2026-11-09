package common

// 测试目标：长度限制工具（limits.go）。
// 覆盖的正常路径：ExceedMaxLength 边界（等于上限不超、超过上限超）。
// 覆盖的异常情况：空值、中文字符按 rune 计数。

import (
	"strings"
	"testing"
)

func TestExceedMaxLength(t *testing.T) {
	if ExceedMaxLength("abc", 3) {
		t.Fatal("exact-length value should not exceed")
	}
	if !ExceedMaxLength("abcd", 3) {
		t.Fatal("over-length value should exceed")
	}
	if ExceedMaxLength("", 63) {
		t.Fatal("empty value should not exceed")
	}
	// 中文按 rune 计数：3 个中文字符在 3 以内
	if ExceedMaxLength("你好啊", 3) {
		t.Fatal("3 runes should not exceed limit 3")
	}
	if !ExceedMaxLength("你好啊你", 3) {
		t.Fatal("4 runes should exceed limit 3")
	}
}

func TestExceedNameLimitAndDescLimit(t *testing.T) {
	if ExceedNameLimit(strings.Repeat("a", NameMaxLen)) {
		t.Fatal("name at limit should pass")
	}
	if !ExceedNameLimit(strings.Repeat("a", NameMaxLen+1)) {
		t.Fatal("name over limit should fail")
	}
	if ExceedDescLimit(strings.Repeat("b", DescMaxLen)) {
		t.Fatal("desc at limit should pass")
	}
	if !ExceedDescLimit(strings.Repeat("b", DescMaxLen+1)) {
		t.Fatal("desc over limit should fail")
	}
}

func TestLimitConstants(t *testing.T) {
	if NameMaxLen != 63 || DescMaxLen != 512 || AliasMaxLen != 128 ||
		DisplayNameMaxLen != 128 || APIKeyNameMaxLen != 100 || ConnectionNameMaxLen != 128 {
		t.Fatal("limit constants mismatch")
	}
}
