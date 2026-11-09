// TASK-028 asset 模块测试共享助手：
//
//	目标：为 9 个 logic 文件提供统一的场景搭建与断言工具，避免重复代码。
//	正常路径：Envelope 解包、HTTP 错误断言、领域层直插测试数据的种子助手
//	          （asset 文件、绑定、project 等），以及分片上传用例的
//	          minio 内存仿真 S3 驱动（newS3Service）。
//	异常情况：无（纯工具函数，不涉及业务异常分支）。
package asset

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	domainasset "backend/internal/domain/asset"
	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/infra/filestore"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// randomToken 生成一个随机会话 id（S3 仿真 uploadId 用）。
func randomToken() string {
	sum := md5.Sum([]byte(fmt.Sprintf("%d", timeNowNano())))
	return hex.EncodeToString(sum[:8])
}

func timeNowNano() int64 { return time.Now().UnixNano() }

func md5hex(b []byte) string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

// insertTempAsset 直接落一条分片上传 temp 记录（status=temp、op_id=uploadID），
// 用于绕过对象存储驱动直接测试 logic 层 follow-up 操作。
func insertTempAsset(t *testing.T, svcCtx *svc.ServiceContext, projectID int64, storageKey, uploadID, fileKey string) repo.AssetFile {
	t.Helper()
	f := repo.AssetFile{
		FileKey:       fileKey,
		OriginalName:  "big.zip",
		ContentType:   "application/zip",
		SizeBytes:     20 * 1024 * 1024,
		StorageDriver: "local",
		StorageKey:    storageKey,
		Bucket:        "",
		Visibility:    "private",
		Status:        "temp",
		ProjectID:     projectID,
		OpID:          uploadID,
	}
	f.CreatedBy = 1
	return *testkit.Insert(t, svcCtx.App.Store.CommonDB(), &f)
}

// newSvcEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
// userID 传 1 表示 Seed 内置 tier0 管理员（可管理任意 project）。
func newSvcEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// respData 解包成功响应的 Data 为 map[string]any。
func respData(t *testing.T, resp *types.Envelope) map[string]any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	m, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	return m
}

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// insertAsset 经领域层 Upload 创建一条 asset_file 记录（本地存储驱动，无外部依赖）。
func insertAsset(t *testing.T, svcCtx *svc.ServiceContext, name string) int64 {
	t.Helper()
	view, err := svcCtx.App.Asset.Upload(context.Background(), domainasset.UploadCommand{
		FileName:    name,
		ContentType: "application/octet-stream",
		Size:        10,
		UserID:      1,
	})
	require.NoError(t, err)
	id, ok := view["fileId"].(int64)
	require.True(t, ok, "upload view fileId should be int64")
	require.Positive(t, id)
	return id
}

// mustBind 经领域层把 asset 绑定到 (ownerType, ownerID)，返回绑定 ID。
func mustBind(t *testing.T, svcCtx *svc.ServiceContext, fileID int64, ownerType string, ownerID int64) int64 {
	t.Helper()
	view, err := svcCtx.App.Asset.Bind(context.Background(), domainasset.BindCommand{
		FileID:    fileID,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		UserID:    1,
	})
	require.NoError(t, err)
	b, ok := view["binding"].(repo.AssetBinding)
	require.True(t, ok, "bind view binding should be repo.AssetBinding")
	require.Positive(t, b.ID)
	return b.ID
}

// mustCreateProject 经领域层创建 project（tier0 admin，创建者即 owner），返回 project ID。
func mustCreateProject(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) int64 {
	t.Helper()
	view, err := svcCtx.App.Project.Create(ctx, domainproject.SaveCommand{Name: name, UserID: 1})
	require.NoError(t, err)
	p, ok := view["project"].(repo.Project)
	require.True(t, ok, "create view project should be repo.Project")
	require.Positive(t, p.ID)
	return p.ID
}

// newS3Service 构造驱动为 S3 的 asset 领域服务（minio 客户端指向内存仿真端点），
// 使分片上传链路可测：init/partURLs/complete/abort 全部在本地完成，不依赖真实
// S3/RustFS。构造不发起任何网络连接（minio.New 仅解析 endpoint）。
func newS3Service(t *testing.T, svcCtx *svc.ServiceContext) *domainasset.Service {
	t.Helper()
	_ = svcCtx
	srv := newS3SimServer(t)
	svc, err := domainasset.New(context.Background(), domainasset.Config{
		Driver:        "s3",
		PublicBaseURL: srv.addr(),
		S3: filestore.S3Config{
			Endpoint:        strings.TrimPrefix(srv.addr(), "http://"),
			Bucket:          "private",
			PublicEndpoint:  srv.addr(),
			AccessKeyID:     "minioadmin",
			SecretAccessKey: "minioadmin",
			PathStyle:       true,
		},
	})
	require.NoError(t, err)
	return svc
}

// s3SimServer 是内存 S3 仿真：保存分片上传会话与已上传分片，
// CompleteMultipartUpload 按 uploadId 合并分片并生成对象。
// 实现 minio Core 用到的 5 个端点：POST ?uploads（init）、GET ?uploadId（list parts）、
// POST ?uploadId（complete）、DELETE ?uploadId（abort）、PUT ?partNumber&uploadId（分片上传）。
type s3SimServer struct {
	mu      sync.Mutex
	server  *httptest.Server
	uploads map[string][]s3SimPart
}

type s3SimPart struct {
	number int
	size   int64
	etag   string
}

func newS3SimServer(t *testing.T) *s3SimServer {
	s := &s3SimServer{uploads: map[string][]s3SimPart{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *s3SimServer) addr() string { return s.server.URL }

func (s *s3SimServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	uploadID := r.URL.Query().Get("uploadId")
	key := r.URL.Path

	switch {
	case r.Method == http.MethodGet && r.URL.RawQuery == "location=":
		// get bucket location：minio 在 region 为空时探测 bucket 区域（path=/private/?location=）
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`))
	case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
		// initiate multipart upload
		id := "sim-upload-" + randomToken()
		s.uploads[id] = nil
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>private</Bucket><Key>` + key + `</Key><UploadId>` + id + `</UploadId></InitiateMultipartUploadResult>`))
	case r.Method == http.MethodPut && uploadID != "" && r.URL.Query().Get("partNumber") != "":
		// upload part：记录大小（真实 body 长度），返回 ETag
		body, _ := io.ReadAll(r.Body)
		partNumber, _ := strconv.Atoi(r.URL.Query().Get("partNumber"))
		parts := s.uploads[uploadID]
		s.uploads[uploadID] = append(parts, s3SimPart{number: partNumber, size: int64(len(body)), etag: fmt.Sprintf("%q", md5hex(body))})
		w.Header().Set("ETag", fmt.Sprintf("%q", md5hex(body)))
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && uploadID != "":
		// list parts
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListPartsResult><Bucket>private</Bucket><Key>` + key + `</Key><UploadId>` + uploadID + `</UploadId>`)
		for _, p := range s.uploads[uploadID] {
			b.WriteString(fmt.Sprintf(`<Part><PartNumber>%d</PartNumber><Size>%d</Size><ETag>%s</ETag></Part>`, p.number, p.size, p.etag))
		}
		b.WriteString(`</ListPartsResult>`)
		_, _ = w.Write([]byte(b.String()))
	case r.Method == http.MethodPost && uploadID != "":
		// complete multipart upload
		delete(s.uploads, uploadID)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Bucket>private</Bucket><Key>` + key + `</Key><ETag>"completed"</ETag></CompleteMultipartUploadResult>`))
	case r.Method == http.MethodDelete && uploadID != "":
		// abort multipart upload
		delete(s.uploads, uploadID)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "not implemented: "+r.Method+" "+r.URL.String(), http.StatusNotImplemented)
	}
}
