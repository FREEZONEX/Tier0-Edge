// 登录日志 domain 服务单元测试：
//
//	目标：覆盖 Record 异步落库（UA 解析 browser/os）、Page 分页查询与过滤。
//	正常路径：Record 后异步插入可见；UA 解析出 Chrome/Mac；分页默认值与过滤生效。
//	异常情况：空 UA 解析为空串；队列外纯函数 parseTime 对非法输入返回 0。
package loginlog

import (
	"context"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUserAgent(t *testing.T) {
	browser, os := parseUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	assert.Contains(t, browser, "Chrome")
	assert.NotEmpty(t, os)

	browser, os = parseUserAgent("")
	assert.Empty(t, browser)
	assert.Empty(t, os)
}

func TestParseTime(t *testing.T) {
	assert.Equal(t, int64(0), parseTime(""))
	assert.Equal(t, int64(1700000000000), parseTime("1700000000000"))
	assert.Equal(t, int64(1700000000000), parseTime("2023-11-14T22:13:20Z"))
	assert.Equal(t, int64(0), parseTime("not-a-time"))
}

func TestService_RecordAndPage(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	_ = store
	ctx := context.Background()
	svc := New(ctx)

	svc.Record(RecordInput{
		UserID:    1,
		UserName:  "tier0",
		IP:        "10.0.0.8",
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		Code:      200,
	})
	svc.Record(RecordInput{UserName: "ghost", IP: "192.168.1.2", Code: 401, Msg: "invalid username or password"})

	var total int64
	var browser, os string
	require.Eventually(t, func() bool {
		view, err := svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10})
		if err != nil {
			return false
		}
		total = view["total"].(int64)
		if total != 2 {
			return false
		}
		items := view["data"].([]repo.UserLoginLog)
		for _, item := range items {
			if item.Code == 200 {
				browser, os = item.Browser, item.Os
			}
		}
		return true
	}, 5*time.Second, 50*time.Millisecond)

	assert.Equal(t, int64(2), total)
	assert.NotEmpty(t, browser, "success row should carry parsed browser")
	assert.NotEmpty(t, os, "success row should carry parsed os")
}

func TestService_PageFilters(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	_ = store
	ctx := context.Background()
	svc := New(ctx)

	svc.Record(RecordInput{UserID: 1, UserName: "tier0", IP: "10.0.0.8", Code: 200})
	svc.Record(RecordInput{UserName: "ghost", IP: "192.168.1.2", Code: 401})
	require.Eventually(t, func() bool {
		view, err := svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10})
		return err == nil && view["total"].(int64) == 2
	}, 5*time.Second, 50*time.Millisecond)

	view, err := svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10, Code: 401})
	require.NoError(t, err)
	assert.Equal(t, int64(1), view["total"].(int64))

	view, err = svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10, UserName: "gho"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), view["total"].(int64))

	view, err = svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10, IpAddr: "10.0.0"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), view["total"].(int64))

	// keyword 组合匹配账号或 IP（照审计 operatorKeyword 范式）。
	view, err = svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10, Keyword: "192.168"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), view["total"].(int64))

	view, err = svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10, Keyword: "tier0"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), view["total"].(int64))
}

func TestService_RecordBurstPersistsBeforeReturn(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	service := New(context.Background())
	for i := 0; i < 300; i++ {
		service.Record(RecordInput{UserID: 1, UserName: "burst-test", Code: 200})
	}
	var total int64
	require.NoError(t, store.CommonDB().Model(&repo.UserLoginLog{}).Where("user_name = ?", "burst-test").Count(&total).Error)
	require.Equal(t, int64(300), total, "completed calls must not leave records in a lossy process-local queue")
}

func TestService_PageCapAndLiteralSearch(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	svc := New(ctx)
	for _, name := range []string{"john_doe", "johnXdoe", "percent%name", "percentXname"} {
		require.NoError(t, svc.repo.InsertUserLoginLog(ctx, repo.UserLoginLog{UserName: name, Code: 200}))
	}
	for _, tc := range []struct {
		search string
		count  int64
	}{{"john_doe", 1}, {"%", 1}, {"_", 1}} {
		result, err := svc.Page(ctx, PageQuery{Keyword: tc.search, PageSize: 1000})
		require.NoError(t, err)
		require.Equal(t, 200, result["pageSize"])
		require.Equal(t, tc.count, result["total"])
	}
}
