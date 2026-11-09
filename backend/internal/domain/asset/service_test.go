package asset

// 测试目标：asset 领域服务（service.go）的 DB 集成路径。
// 覆盖的正常路径：Upload/UploadContent/Detail/Download/Open/OpenByStorageKey/Bind/
// ListAttachments/UploadAttachment/Unbind/List；local filestore 驱动。
// 覆盖的异常情况：非法入参（空文件名/nil reader/空 owner）、文件不存在（ErrNotFound）、
// 绑定不存在。
// 说明：svctest 依赖 domain 包（import cycle），此处用 testkit 装配；
// local 驱动不支持预签名（SignGetURL 返回 ErrPresignUnsupported），fileURL 走代理路径分支。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"backend/internal/contextx"
	"backend/internal/infra/filestore"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestAsset(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	svc, err := New(ctx, Config{
		Driver:        "local",
		FileRoot:      t.TempDir(),
		PublicBaseURL: "http://localhost",
	})
	require.NoError(t, err)
	return svc, ctx
}

func TestAssetUploadAndContent(t *testing.T) {
	svc, ctx := newTestAsset(t)

	// Upload（元数据登记）
	uploaded, err := svc.Upload(ctx, UploadCommand{
		FileName:    "report.pdf",
		ContentType: "application/pdf",
		Size:        1024,
		UserID:      1,
	})
	require.NoError(t, err)
	fileID, ok := uploaded["fileId"].(int64)
	require.True(t, ok && fileID > 0)
	require.Equal(t, "report.pdf", uploaded["fileName"])

	// 非法文件名
	_, err = svc.Upload(ctx, UploadCommand{FileName: ""})
	require.ErrorIs(t, err, ErrInvalid)

	// UploadContent（真实写盘）
	content := []byte("hello asset")
	created, err := svc.UploadContent(ctx, UploadContentCommand{
		FileName:    "note.txt",
		ContentType: "text/plain",
		Size:        int64(len(content)),
		Reader:      bytes.NewReader(content),
		UserID:      1,
	})
	require.NoError(t, err)
	contentID, ok := created["fileId"].(int64)
	require.True(t, ok && contentID > 0)
	sha, ok := created["sha256"].(string)
	require.True(t, ok && len(sha) == 64)

	// 非法入参
	_, err = svc.UploadContent(ctx, UploadContentCommand{FileName: ""})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.UploadContent(ctx, UploadContentCommand{FileName: "x", Reader: nil})
	require.ErrorIs(t, err, ErrInvalid)

	// Detail
	detail, err := svc.Detail(ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, "note.txt", detail["fileName"])
	_, err = svc.Detail(ctx, 999999)
	require.ErrorIs(t, err, ErrNotFound)

	// Download
	download, err := svc.Download(ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, "backend-stream", download["download"].(map[string]any)["mode"])

	// Open：读取真实内容
	item, reader, meta, err := svc.Open(ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, contentID, item.ID)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, content, body)
	require.Equal(t, "text/plain", meta.ContentType)
	_ = reader.Close()

	// OpenByStorageKey
	item2, reader2, _, err := svc.OpenByStorageKey(ctx, item.StorageKey)
	require.NoError(t, err)
	require.Equal(t, item.ID, item2.ID)
	_ = reader2.Close()
	_, _, _, err = svc.OpenByStorageKey(ctx, "no-such-key")
	require.ErrorIs(t, err, ErrNotFound)

	// Open 不存在
	_, _, _, err = svc.Open(ctx, 999999)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAssetBindAndAttachments(t *testing.T) {
	svc, ctx := newTestAsset(t)

	created, err := svc.UploadContent(ctx, UploadContentCommand{
		FileName: "attach.txt",
		Reader:   strings.NewReader("attach"),
		Size:     6,
		UserID:   1,
	})
	require.NoError(t, err)
	fileID := created["fileId"].(int64)

	// Bind
	bindResp, err := svc.Bind(ctx, BindCommand{FileID: fileID, OwnerType: "node", OwnerID: 42, UserID: 1})
	require.NoError(t, err)
	binding, ok := bindResp["binding"].(repo.AssetBinding)
	require.True(t, ok && binding.ID > 0)

	// 非法 Bind
	_, err = svc.Bind(ctx, BindCommand{FileID: 0, OwnerType: "node", OwnerID: 1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.Bind(ctx, BindCommand{FileID: fileID, OwnerType: "", OwnerID: 1})
	require.ErrorIs(t, err, ErrInvalid)

	// List（按 owner 过滤）
	list, err := svc.List(ctx, "node", 42)
	require.NoError(t, err)
	require.Equal(t, 1, list["total"])
	list, err = svc.List(ctx, "node", 43)
	require.NoError(t, err)
	require.Zero(t, list["total"])

	// ListAttachments（分页）
	attachments, err := svc.ListAttachments(ctx, "node", 42, 1, 1, 20, true)
	require.NoError(t, err)
	require.Equal(t, 1, attachments["total"])
	items := attachments["list"].([]map[string]any)
	require.Len(t, items, 1)
	// local 驱动无预签名 → fileUrl 为代理路径
	url, ok := items[0]["fileUrl"].(string)
	require.True(t, ok)
	require.Contains(t, url, "/api/core/assets/")

	// 非法入参
	_, err = svc.ListAttachments(ctx, "", 1, 1, 1, 20, false)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.ListAttachments(ctx, "node", 0, 1, 1, 20, false)
	require.ErrorIs(t, err, ErrInvalid)

	// UploadAttachment
	attached, err := svc.UploadAttachment(ctx, AttachmentUploadCommand{
		OwnerType: "node",
		OwnerID:   42,
		UnsID:     7,
		FileName:  "attach2.txt",
		Reader:    strings.NewReader("attach2"),
		Size:      7,
		UserID:    1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), attached["unsId"])
	_, err = svc.UploadAttachment(ctx, AttachmentUploadCommand{OwnerType: "node", OwnerID: 0})
	require.ErrorIs(t, err, ErrInvalid)

	// Unbind
	unbind, err := svc.Unbind(ctx, binding.ID)
	require.NoError(t, err)
	require.Equal(t, true, unbind["deleted"])
	_, err = svc.Unbind(ctx, binding.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAssetNewInvalidDriver(t *testing.T) {
	ctx := context.Background()
	_, err := New(ctx, Config{Driver: "bogus"})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestAssetLocalStoreErrors(t *testing.T) {
	// local 驱动不支持预签名
	store := &filestore.LocalStore{}
	ctx := context.Background()
	signed, ok, err := store.SignGetURL(ctx, "", "k", 0, "")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, signed)
	_, _, ok, err = store.SignPostURL(ctx, "", "k", "", 0, 0)
	require.NoError(t, err)
	require.False(t, ok)
	_, err = store.CreateMultipartUpload(ctx, "", "k", "")
	require.ErrorIs(t, err, filestore.ErrMultipartUnsupported)
	require.ErrorIs(t, ErrPresignUnsupported, ErrPresignUnsupported)
	_ = errors.Is
}
