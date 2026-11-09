// openapiUnsAttachmentListLogic_test.go — 测试。
//
// 测试目标：OpenAPI 附件列表 入口。
// 正常路径：列出节点附件。
// 异常情况：无引用 400/不存在 404。
package uns

import (
	"io"
	"mime/multipart"
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsAttachmentList_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	created := createOpenapiUnsFileNode(t, svcCtx, "att-list-node")

	// 先传一个附件。
	l := NewOpenapiUnsAttachmentUploadLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsAttachmentUpload(
		&types.UnsAttachmentUploadReq{UnsId: created["id"].(int64)},
		&fakeMultipartFile{data: []byte("x")},
		&multipart.FileHeader{Filename: "c.txt"},
	)
	require.NoError(t, err)

	ll := NewOpenapiUnsAttachmentListLogic(ctx, svcCtx)
	resp, err := ll.OpenapiUnsAttachmentList(&types.UnsAttachmentListReq{
		UnsId: created["id"].(int64),
	})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, created["id"].(int64), body["unsId"])
	require.Equal(t, int64(1), body["total"])
	require.Len(t, body["list"].([]any), 1)
}

func TestOpenapiUnsAttachmentList_NoNodeReference(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsAttachmentListLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsAttachmentList(&types.UnsAttachmentListReq{})
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiUnsAttachmentList_NodeNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsAttachmentListLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsAttachmentList(&types.UnsAttachmentListReq{Topic: "State/ghost"})
	requireHTTPStatus(t, err, 404)
}
// fakeMultipartFile 实现 multipart.File 接口（io.Reader + ReaderAt + Seeker + Closer）。

// fakeMultipartFile 实现 multipart.File 接口（io.Reader + ReaderAt + Seeker + Closer）。
type fakeMultipartFile struct {
	data []byte
	off  int
}

func (f *fakeMultipartFile) Read(p []byte) (int, error) {
	if f.off >= len(f.data) {
		return 0, errEOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}

func (f *fakeMultipartFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.data)) {
		return 0, errEOF
	}
	n := copy(p, f.data[off:])
	return n, nil
}

func (f *fakeMultipartFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		f.off = int(offset)
	case 1:
		f.off += int(offset)
	case 2:
		f.off = len(f.data) + int(offset)
	}
	if f.off < 0 {
		return 0, errInvalidSeek
	}
	return int64(f.off), nil
}

func (f *fakeMultipartFile) Close() error { return nil }

var errEOF = io.EOF

var errInvalidSeek = errInvalidSeekImpl{}

type errInvalidSeekImpl struct{}

func (errInvalidSeekImpl) Error() string { return "invalid seek" }
