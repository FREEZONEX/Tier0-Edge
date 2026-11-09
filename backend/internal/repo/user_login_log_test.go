// 登录日志 repo 层单元测试（DryRun SQL 断言 + 纯函数）：
//
//	目标：覆盖 ListUserLoginLogs/InsertUserLoginLog 生成的 SQL 形状（过滤、排序、分页、列集），
//	      以及 normalize/truncate 纯函数。
//	说明：repo 包禁止依赖 testkit（import cycle），DB 行为真库验证由
//	      internal/domain/loginlog 与 internal/logic/core/loginlog 的 svctest 覆盖
//	      （其走完整 Migrate 链路，同时验证 sys_user_login_log 的 DDL）。
package repo

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newLoginLogRepoForSQL(t *testing.T, capture *sqlCaptureLogger) *UserLoginLogRepo {
	t.Helper()
	return &UserLoginLogRepo{db: newDryRunDBWithCapture(t, capture)}
}

// TestUserLoginLog_ListSQLFilters 覆盖过滤条件的 SQL 形状：模糊匹配、code、时间范围、排序。
func TestUserLoginLog_ListSQLFilters(t *testing.T) {
	capture := &sqlCaptureLogger{}
	r := newLoginLogRepoForSQL(t, capture)
	code := int64(401)
	_, _, err := r.ListUserLoginLogs(nil, UserLoginLogFilter{
		PageNo:    2,
		PageSize:  50,
		Keyword:   "10.0",
		UserName:  "tier0",
		IpAddr:    "10.0.0",
		Code:      &code,
		StartTime: 1700000000000,
		EndTime:   1700000001000,
	})
	if err != nil {
		t.Fatalf("ListUserLoginLogs() error = %v", err)
	}
	joined := strings.Join(capture.sqls, "\n")
	for _, want := range []string{
		// keyword 组合条件（gorm 会给 OR 组包一层或多层括号，断言宽松匹配）
		"(user_name ILIKE",
		"OR ip_addr ILIKE",
		"user_name ILIKE",
		"ip_addr ILIKE",
		"code =",
		"created_time >=",
		"created_time <=",
		"created_time DESC",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("SQL missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "LIMIT 50") || !strings.Contains(joined, "OFFSET 50") {
		t.Fatalf("SQL missing pagination:\n%s", joined)
	}
}

// TestUserLoginLog_InsertSQLColumns 覆盖插入列集（Code 0 -> 200、补 created_time 的默认值归一在真库测试中验证）。
func TestUserLoginLog_InsertSQLColumns(t *testing.T) {
	capture := &sqlCaptureLogger{}
	r := newLoginLogRepoForSQL(t, capture)
	if err := r.InsertUserLoginLog(nil, UserLoginLog{UserName: "ghost", IpAddr: "192.168.1.2", Code: 401, Msg: "bad credentials"}); err != nil {
		t.Fatalf("InsertUserLoginLog() error = %v", err)
	}
	joined := strings.Join(capture.sqls, "\n")
	if !strings.Contains(joined, "sys_user_login_log") {
		t.Fatalf("SQL should target sys_user_login_log:\n%s", joined)
	}
	for _, want := range []string{"user_id", "user_name", "ip_addr", "login_location", "browser", "os", "code", "msg", "created_time"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("insert SQL missing column %q:\n%s", want, joined)
		}
	}
}

// TestTruncateUserLoginField 覆盖按 Unicode 字符截断与 trim（多字节字符不得切出非法 UTF-8）。
func TestTruncateUserLoginField(t *testing.T) {
	if got := truncateUserLoginField("  abc  ", 10); got != "abc" {
		t.Fatalf("truncateUserLoginField trim = %q", got)
	}
	if got := truncateUserLoginField("abcdef", 3); got != "abc" {
		t.Fatalf("truncateUserLoginField cut = %q", got)
	}
	if got := truncateUserLoginField("abc", 0); got != "abc" {
		t.Fatalf("truncateUserLoginField no-limit = %q", got)
	}
	// 34 个汉字（102 字节）按 100 字符截断：结果必须是合法 UTF-8 且保留 100 个字符。
	long := strings.Repeat("中", 34)
	got := truncateUserLoginField(long, 100)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateUserLoginField produced invalid UTF-8: %q", got)
	}
	if want := strings.Repeat("中", 34); got != want {
		t.Fatalf("truncateUserLoginField 34-char name under 100-char limit = %d chars, want 34", len([]rune(got)))
	}
	trimmed := truncateUserLoginField(strings.Repeat("账", 120), 100)
	if !utf8.ValidString(trimmed) || len([]rune(trimmed)) != 100 {
		t.Fatalf("truncateUserLoginField rune cut = %d runes, want 100 and valid UTF-8", len([]rune(trimmed)))
	}
}

// TestNormalizeUserLoginLog 覆盖 CreatedAt 毫秒时间戳归一。
func TestNormalizeUserLoginLog(t *testing.T) {
	item := UserLoginLog{CreatedTime: time.UnixMilli(1700000000000).UTC()}
	normalizeUserLoginLog(&item)
	if item.CreatedAt != 1700000000000 {
		t.Fatalf("CreatedAt = %d, want 1700000000000", item.CreatedAt)
	}
	item.CreatedAt = 42
	normalizeUserLoginLog(&item)
	if item.CreatedAt != 42 {
		t.Fatalf("CreatedAt should not be overwritten, got %d", item.CreatedAt)
	}
}
