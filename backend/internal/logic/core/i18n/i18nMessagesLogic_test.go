// TASK-028 I18nMessagesLogic 单元测试：
//
//	目标：覆盖语言包读取（normalizeLang 归一化、BOM 剥离、JSON 解析）。
//	正常路径：在 etc/i18n 下放置临时语言包后，I18nMessages 返回 messages 映射；
//	          zh_CN/en_US 大小写与下划线/连字符均归一化。
//	异常情况：语言不支持返回 400；语言包不存在返回 404；JSON 非法返回 500。
package i18n

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	respx "backend/internal/httpx"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestI18nMessagesLogic_Messages_Success(t *testing.T) {
	dir := t.TempDir()
	packDir := filepath.Join(dir, "etc", "i18n")
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	pack := `{"common.save": "保存", "common.cancel": "取消"}`
	require.NoError(t, os.WriteFile(filepath.Join(packDir, "zh_CN.json"), []byte(pack), 0o644))
	t.Chdir(dir)

	svcCtx := svctest.NewTestServiceContext(t)
	l := NewI18nMessagesLogic(svcCtxCtx(), svcCtx)
	resp, err := l.I18nMessages(&types.I18nMessagesReq{Lang: "zh-CN"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	view, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	messages, ok := view["messages"].(map[string]string)
	require.True(t, ok, "messages should be map[string]string")
	assert.Equal(t, "保存", messages["common.save"])
	assert.Equal(t, "取消", messages["common.cancel"])
}

func TestI18nMessagesLogic_Messages_BOMStripped(t *testing.T) {
	dir := t.TempDir()
	packDir := filepath.Join(dir, "etc", "i18n")
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	pack := "\xEF\xBB\xBF" + `{"a": "b"}`
	require.NoError(t, os.WriteFile(filepath.Join(packDir, "en_US.json"), []byte(pack), 0o644))
	t.Chdir(dir)

	svcCtx := svctest.NewTestServiceContext(t)
	l := NewI18nMessagesLogic(svcCtxCtx(), svcCtx)
	resp, err := l.I18nMessages(&types.I18nMessagesReq{Lang: "en"})
	require.NoError(t, err)

	view := resp.Data.(map[string]any)
	messages := view["messages"].(map[string]string)
	assert.Equal(t, "b", messages["a"])
}

func TestI18nMessagesLogic_UnsupportedLanguage(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	l := NewI18nMessagesLogic(svcCtxCtx(), svcCtx)

	_, err := l.I18nMessages(&types.I18nMessagesReq{Lang: "fr-FR"})
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 400, httpErr.Status)
}

func TestI18nMessagesLogic_PackNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	svcCtx := svctest.NewTestServiceContext(t)
	l := NewI18nMessagesLogic(svcCtxCtx(), svcCtx)

	_, err := l.I18nMessages(&types.I18nMessagesReq{Lang: "zh-CN"})
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 404, httpErr.Status)
}

func TestI18nMessagesLogic_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	packDir := filepath.Join(dir, "etc", "i18n")
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(packDir, "zh_CN.json"), []byte("{not-json"), 0o644))
	t.Chdir(dir)

	svcCtx := svctest.NewTestServiceContext(t)
	l := NewI18nMessagesLogic(svcCtxCtx(), svcCtx)

	_, err := l.I18nMessages(&types.I18nMessagesReq{Lang: "zh-CN"})
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 500, httpErr.Status)
}

func TestI18nMessagesLogic_ReadErrorsPropagate(t *testing.T) {
	dir := t.TempDir()
	packDir := filepath.Join(dir, "etc", "i18n")
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	// 目录而非文件：os.ReadFile 返回 IsNotExist=false 的其他错误应原样传播
	require.NoError(t, os.Mkdir(filepath.Join(packDir, "zh_CN.json"), 0o755))
	t.Chdir(dir)

	svcCtx := svctest.NewTestServiceContext(t)
	l := NewI18nMessagesLogic(svcCtxCtx(), svcCtx)

	_, err := l.I18nMessages(&types.I18nMessagesReq{Lang: "zh-CN"})
	require.Error(t, err)
	var httpErr *respx.HTTPError
	// 非 NotExist 错误不包装成 HTTPError
	require.False(t, ErrorAsHTTP(err, &httpErr))
}

// svcCtxCtx 提供无 Subject 的 context（I18nMessages 不依赖登录用户）。
func svcCtxCtx() context.Context { return context.Background() }

// ErrorAsHTTP 供断言用：判断 err 是否可断言为 *HTTPError。
func ErrorAsHTTP(err error, target **respx.HTTPError) bool {
	if err == nil || target == nil {
		return false
	}
	httpErr, ok := err.(*respx.HTTPError)
	if ok {
		*target = httpErr
	}
	return ok
}
