package asset

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/infra/filestore"
	"backend/internal/repo"
)

const (
	// defaultMultipartPartSize 是分片上传的默认分片大小（10MB）。
	defaultMultipartPartSize = 10 * 1024 * 1024
	// minMultipartPartSize 是服务端采纳的最小分片大小（5MB）；客户端申报小于该值
	// 的分片大小一律回退到默认值。
	minMultipartPartSize = 5 * 1024 * 1024
	// defaultMultipartURLTTL 是分片上传会话与分片 PUT 地址的默认有效期（1 小时）。
	defaultMultipartURLTTL = time.Hour
)

var (
	// ErrMultipartUnsupported 表示当前存储驱动不支持分片上传（local 仅面向开发环境）。
	ErrMultipartUnsupported = errors.New("multipart upload is not supported by the local storage driver; configure the s3/RustFS storage driver")
)

// OpenapiMultipartInitCommand 是 POST /openapi/v1/assets/files/multipart/init 的入参。
// BizID 与 cloud 契约对齐但无本地业务语义，仅透传保留。
type OpenapiMultipartInitCommand struct {
	FileName      string
	ContentType   string
	Size          int64
	PartSize      int64
	Business      string
	UseBy         string
	Visibility    string
	AppInstanceID string
	SessionID     string
	BizID         string
	UserID        int64
}

// MultipartInitResult 是 init 的返回：FileKey 为对象 key 的扁平标识，FilePath 为
// 项目作用域的对象 key，UploadID 为对象存储侧的上传会话凭据。
type MultipartInitResult struct {
	UploadID  string
	FileKey   string
	FilePath  string
	PartSize  int64
	PartCount int64
	ExpiresAt int64
}

// MultipartPartURLResult 是单个分片的预签名 PUT 地址。
type MultipartPartURLResult struct {
	PartNumber int32
	URL        string
	ExpiresAt  int64
}

// MultipartCompleteResult 是 complete 的返回：FileID 为 active 后的 asset 记录 ID，
// SizeBytes 为对象存储汇总的真实大小。
type MultipartCompleteResult struct {
	FileID    int64
	FilePath  string
	FileURL   string
	SizeBytes int64
	ExpiresAt int64
}

// CoreMultipartInitCommand 是 POST /api/core/assets/multipart/init 的入参。
// 与 openapi 版不同，ProjectID 由会话显式传入（不依赖 api key 上下文解析），
// 会话用户对该 project 的管理权限由 core logic 层校验。
type CoreMultipartInitCommand struct {
	ProjectID   int64
	FileName    string
	ContentType string
	Size        int64
	PartSize    int64
	UserID      int64
}

// validateOpenapiMultipartInit 校验分片上传 init 入参：文件名/扩展名、申报大小
// （0 < size ≤ 1GB，与单发直传共用上限）、公共元数据字段。partSize 单独在
// resolveMultipartPartSize 中归一化，不在此处校验。
func validateOpenapiMultipartInit(cmd OpenapiMultipartInitCommand) error {
	if err := validateOpenapiFileNameAndSize(cmd.FileName, cmd.Size); err != nil {
		return err
	}
	return validateOpenapiMeta(cmd.AppInstanceID, cmd.SessionID, cmd.Visibility, cmd.UseBy)
}

// resolveMultipartPartSize 采纳客户端 ≥5MB 的分片大小，否则回退到 10MB 默认值。
func resolveMultipartPartSize(partSize int64) int64 {
	if partSize >= minMultipartPartSize {
		return partSize
	}
	return defaultMultipartPartSize
}

// multipartPartCount 按申报大小与分片大小计算分片数量：向上取整，至少 1 片。
func multipartPartCount(size, partSize int64) int64 {
	if size <= 0 || partSize <= 0 {
		return 1
	}
	return (size + partSize - 1) / partSize
}

// CreateMultipartUpload 发起一次分片上传（openapi 版）：校验入参、按 api key 上下文
// 派生 projectID，然后复用 createMultipartUpload 完成落库与分片会话创建。
func (s *Service) CreateMultipartUpload(ctx context.Context, cmd OpenapiMultipartInitCommand) (*MultipartInitResult, error) {
	if err := validateOpenapiMultipartInit(cmd); err != nil {
		return nil, err
	}
	visibility := strings.ToLower(strings.TrimSpace(cmd.Visibility))
	if visibility == "" {
		visibility = VisibilityPrivate
	}
	projectID, err := s.projectScope(ctx, cmd.UserID)
	if err != nil {
		return nil, err
	}
	return s.createMultipartUpload(ctx, cmd, projectID, visibility)
}

// CoreMultipartInit 发起一次分片上传（core 会话版，供 APP 导入大文件直传使用）：
// projectID 由会话显式传入并写入文件记录的 project 作用域；会话用户对该 project
// 的管理权限由 core logic 层调用 Project.CheckManage 校验，此处仅要求 projectID 合法。
// core 版固定 private 可见性，不承载 openapi 的 visibility/useBy 语义。
func (s *Service) CoreMultipartInit(ctx context.Context, cmd CoreMultipartInitCommand) (*MultipartInitResult, error) {
	if cmd.ProjectID <= 0 {
		return nil, fmt.Errorf("%w: projectId must be positive", ErrInvalid)
	}
	if err := validateOpenapiMultipartInit(OpenapiMultipartInitCommand{
		FileName: cmd.FileName, ContentType: cmd.ContentType, Size: cmd.Size, PartSize: cmd.PartSize,
	}); err != nil {
		return nil, err
	}
	return s.createMultipartUpload(ctx, OpenapiMultipartInitCommand{
		FileName:    cmd.FileName,
		ContentType: cmd.ContentType,
		Size:        cmd.Size,
		PartSize:    cmd.PartSize,
		Visibility:  VisibilityPrivate,
		UserID:      cmd.UserID,
	}, cmd.ProjectID, VisibilityPrivate)
}

// createMultipartUpload 是 openapi 与 core 两个入口共用的分片上传 init 实现：
// 生成项目作用域对象 key、调用对象存储创建分片会话，并以 temp 状态登记 asset
// 元数据（op_id 存 uploadId、size_bytes 存申报大小）。会话过期时间与默认分片大小
// 一同返回给客户端。
func (s *Service) createMultipartUpload(ctx context.Context, cmd OpenapiMultipartInitCommand, projectID int64, visibility string) (*MultipartInitResult, error) {
	key := newScopedStorageKey(projectID, cmd.AppInstanceID, cmd.SessionID, cmd.FileName)
	bucket := s.bucketForVisibility(visibility)
	uploadID, err := s.files.CreateMultipartUpload(ctx, bucket, key, cmd.ContentType)
	if err != nil {
		if errors.Is(err, filestore.ErrMultipartUnsupported) {
			return nil, ErrMultipartUnsupported
		}
		return nil, err
	}
	partSize := resolveMultipartPartSize(cmd.PartSize)
	expiresAt := time.Now().UTC().Add(defaultMultipartURLTTL)
	file := repo.AssetFile{
		FileKey:       strings.ReplaceAll(key, "/", "_"),
		OriginalName:  strings.TrimSpace(cmd.FileName),
		ContentType:   strings.TrimSpace(cmd.ContentType),
		SizeBytes:     cmd.Size,
		StorageDriver: s.driver,
		StorageKey:    key,
		Bucket:        bucket,
		Visibility:    visibility,
		Status:        "temp",
		ProjectID:     projectID,
		AppInstanceID: strings.TrimSpace(cmd.AppInstanceID),
		SessionID:     strings.TrimSpace(cmd.SessionID),
		OpID:          uploadID,
		ExpireAt:      &expiresAt,
	}
	file.CreatedBy = cmd.UserID
	created, err := s.repo.CreateAssetFile(ctx, file)
	if err != nil {
		// temp 记录落库失败则丢弃已创建的分片会话，避免对象存储残留孤儿分片。
		_ = s.files.AbortMultipartUpload(ctx, bucket, key, uploadID)
		return nil, err
	}
	return &MultipartInitResult{
		UploadID:  uploadID,
		FileKey:   created.FileKey,
		FilePath:  key,
		PartSize:  partSize,
		PartCount: multipartPartCount(cmd.Size, partSize),
		ExpiresAt: expiresAt.UnixMilli(),
	}, nil
}

// MultipartPartURLs 按 (fileKey, uploadId) 定位 temp 记录后，为每个分片序号签发
// 预签名 PUT 地址。分片序号非法（≤0）直接拒绝，不签发部分地址。
func (s *Service) MultipartPartURLs(ctx context.Context, fileKey, uploadID string, partNumbers []int32) ([]MultipartPartURLResult, error) {
	item, err := s.ResolveMultipartTemp(ctx, fileKey, uploadID)
	if err != nil {
		return nil, err
	}
	if len(partNumbers) == 0 {
		return nil, ErrInvalid
	}
	out := make([]MultipartPartURLResult, 0, len(partNumbers))
	for _, n := range partNumbers {
		if n <= 0 {
			return nil, fmt.Errorf("%w: partNumber must be positive", ErrInvalid)
		}
		u, ok, err := s.files.SignPutPartURL(ctx, s.bucketForFile(item), item.StorageKey, uploadID, n, defaultMultipartURLTTL)
		if err != nil {
			return nil, err
		}
		if !ok || strings.TrimSpace(u) == "" {
			return nil, ErrMultipartUnsupported
		}
		out = append(out, MultipartPartURLResult{
			PartNumber: n,
			URL:        s.rewriteToURLEndpoint(ctx, u),
			ExpiresAt:  time.Now().UTC().Add(defaultMultipartURLTTL).UnixMilli(),
		})
	}
	return out, nil
}

// MultipartComplete 完成分片上传：先按对象存储 ListParts 汇总真实上传大小，拒绝
// 超申报大小的会话（中止并清理），再以客户端回传的 (partNumber, etag) 合并分片，
// 最后把 temp 记录置为 active 并覆盖 size_bytes 为真实大小。大小依据只来自对象
// 存储，不信任客户端自报的 etag 与大小。
func (s *Service) MultipartComplete(ctx context.Context, fileKey, uploadID string, parts []filestore.CompletePart) (*MultipartCompleteResult, error) {
	item, err := s.ResolveMultipartTemp(ctx, fileKey, uploadID)
	if err != nil {
		return nil, err
	}
	bucket := s.bucketForFile(item)
	uploaded, err := s.files.ListParts(ctx, bucket, item.StorageKey, uploadID)
	if err != nil {
		if errors.Is(err, filestore.ErrMultipartUnsupported) {
			return nil, ErrMultipartUnsupported
		}
		return nil, err
	}
	var realSize int64
	for _, part := range uploaded {
		realSize += part.SizeBytes
	}
	if realSize <= 0 || realSize > item.SizeBytes {
		// 未上传任何分片或真实大小超过申报值：拒绝并清理会话与已上传分片。
		_ = s.files.AbortMultipartUpload(ctx, bucket, item.StorageKey, uploadID)
		return nil, fmt.Errorf("%w: uploaded %d bytes exceeds the declared size %d bytes", ErrInvalid, realSize, item.SizeBytes)
	}
	if err := s.files.CompleteMultipartUpload(ctx, bucket, item.StorageKey, uploadID, parts); err != nil {
		if errors.Is(err, filestore.ErrMultipartUnsupported) {
			return nil, ErrMultipartUnsupported
		}
		return nil, err
	}
	if err := s.repo.MarkMultipartUploadActive(ctx, item.ID, realSize); err != nil {
		// 对象已合并但记录更新失败：uploadId 已失效无法重试，返回错误让调用方感知
		// 并走人工排查，不静默吞掉。
		return nil, err
	}
	fileURLResult, err := s.OpenapiFileURL(ctx, item, 0)
	if err != nil {
		return nil, err
	}
	return &MultipartCompleteResult{
		FileID:    item.ID,
		FilePath:  item.StorageKey,
		FileURL:   fileURLResult.FileURL,
		SizeBytes: realSize,
		ExpiresAt: fileURLResult.ExpiresAt,
	}, nil
}

// MultipartAbort 中止分片上传：丢弃对象存储侧的分片会话并软删除 temp 记录。
func (s *Service) MultipartAbort(ctx context.Context, fileKey, uploadID string) (bool, error) {
	item, err := s.ResolveMultipartTemp(ctx, fileKey, uploadID)
	if err != nil {
		return false, err
	}
	if err := s.files.AbortMultipartUpload(ctx, s.bucketForFile(item), item.StorageKey, uploadID); err != nil {
		if errors.Is(err, filestore.ErrMultipartUnsupported) {
			return false, ErrMultipartUnsupported
		}
		return false, err
	}
	if err := s.repo.SoftDeleteAssetFile(ctx, item.ID); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveMultipartTemp 按 (fileKey, uploadId) 定位分片上传的 temp 记录：uploadId
// 全局唯一，据此查找；fileKey 与记录比对作二次校验，不匹配按不存在处理。
// 导出供 core logic 层在 follow-up 操作前读取记录所属项目（ProjectID）并复检会话
// 用户的管理权限；openapi 路径继续由 domain 内部调用，行为不变。
func (s *Service) ResolveMultipartTemp(ctx context.Context, fileKey, uploadID string) (repo.AssetFile, error) {
	fileKey = strings.TrimSpace(fileKey)
	uploadID = strings.TrimSpace(uploadID)
	if fileKey == "" || uploadID == "" {
		return repo.AssetFile{}, ErrInvalid
	}
	item, err := s.repo.GetTempAssetFileByOpID(ctx, uploadID)
	if err != nil {
		return repo.AssetFile{}, normalizeNotFound(err)
	}
	if item.FileKey != fileKey {
		return repo.AssetFile{}, ErrNotFound
	}
	return item, nil
}
