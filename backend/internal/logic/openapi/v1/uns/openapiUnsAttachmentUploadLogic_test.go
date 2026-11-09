// openapiUnsAttachmentUploadLogic_test.go — 测试。
//
// 测试目标：OpenAPI 上传附件 入口。
// 正常路径：topic/unsId 上传成功。
// 异常情况：无引用 400/不存在 404。
package uns

import (
	"mime/multipart"
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsAttachmentUpload_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	created := createOpenapiUnsFileNode(t, svcCtx, "att-node")

	l := NewOpenapiUnsAttachmentUploadLogic(ctx, svcCtx)
	f := &fakeMultipartFile{data: []byte("hello")}
	header := &multipart.FileHeader{Filename: "a.txt"}
	resp, err := l.OpenapiUnsAttachmentUpload(
		&types.UnsAttachmentUploadReq{UnsId: created["id"].(int64)},
		f,
		header,
	)
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, "a.txt", data["fileName"])
	// header.Size 未设置（0），生产上传 size 取自 multipart.FileHeader.Size。
	require.Equal(t, int64(0), data["size"])
}

func TestOpenapiUnsAttachmentUpload_TopicAndFileName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "att-topic-node")

	l := NewOpenapiUnsAttachmentUploadLogic(ctx, svcCtx)
	f := &fakeMultipartFile{data: []byte("data")}
	header := &multipart.FileHeader{Filename: "b.bin"}
	resp, err := l.OpenapiUnsAttachmentUpload(
		&types.UnsAttachmentUploadReq{Topic: "State/att-topic-node"},
		f,
		header,
	)
	require.NoError(t, err)
	require.Equal(t, "b.bin", resp.Data.(map[string]any)["fileName"])
}

func TestOpenapiUnsAttachmentUpload_NoNodeReference(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsAttachmentUploadLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsAttachmentUpload(&types.UnsAttachmentUploadReq{}, nil, nil)
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiUnsAttachmentUpload_NodeNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsAttachmentUploadLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsAttachmentUpload(&types.UnsAttachmentUploadReq{Topic: "State/ghost"}, nil, nil)
	requireHTTPStatus(t, err, 404)
}
