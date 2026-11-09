package asset

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"backend/internal/infra/filestore"
	"backend/internal/repo"
)

type Service struct {
	repo             *repo.AssetRepo
	files            filestore.Store
	driver           string
	publicBaseURL    string
	privateBucket    string
	publicBucket     string
	s3Endpoint       string
	s3PublicEndpoint string
	s3UseSSL         bool
	s3PathStyle      bool
	urlTTL           time.Duration
}

type Config struct {
	Driver        string
	FileRoot      string
	PublicBaseURL string
	S3            filestore.S3Config
}

type UploadCommand struct {
	FileName    string
	ContentType string
	Size        int64
	Sha256      string
	StorageKey  string
	UserID      int64
}

type UploadContentCommand struct {
	FileName    string
	ContentType string
	Size        int64
	Sha256      string
	Reader      io.Reader
	UserID      int64
}

type BindCommand struct {
	FileID        int64
	OwnerType     string
	OwnerID       int64
	PermissionKey string
	UserID        int64
}

type AttachmentUploadCommand struct {
	OwnerType   string
	OwnerID     int64
	UnsID       int64
	FileName    string
	ContentType string
	Size        int64
	Sha256      string
	Reader      io.Reader
	UserID      int64
}

func New(ctx context.Context, conf Config) (*Service, error) {
	driver := strings.ToLower(strings.TrimSpace(conf.Driver))
	if driver == "" {
		driver = "local"
	}
	var (
		files filestore.Store
		err   error
	)
	switch driver {
	case "local":
		files, err = filestore.NewLocal(conf.FileRoot)
	case "s3":
		var s3store *filestore.S3Store
		s3store, err = filestore.NewS3(conf.S3)
		if err == nil && conf.S3.BootstrapBuckets {
			// 仅内置 RustFS 模式：启动时幂等确保 bucket 存在（替代 deploy 侧
			// rustfs-init 容器）；外置 S3 不做 bucket 级写操作，由运维预建。
			err = s3store.EnsureBuckets(ctx)
		}
		if err == nil {
			files = s3store
		}
	default:
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	return &Service{
		repo:             repo.NewAssetRepo(ctx),
		files:            files,
		driver:           driver,
		publicBaseURL:    strings.TrimRight(strings.TrimSpace(conf.PublicBaseURL), "/"),
		privateBucket:    strings.TrimSpace(conf.S3.Bucket),
		publicBucket:     strings.TrimSpace(conf.S3.PublicBucket),
		s3Endpoint:       strings.TrimSpace(conf.S3.Endpoint),
		s3PublicEndpoint: strings.TrimRight(strings.TrimSpace(conf.S3.PublicEndpoint), "/"),
		s3UseSSL:         conf.S3.UseSSL,
		s3PathStyle:      conf.S3.PathStyle,
		urlTTL:           5 * time.Minute,
	}, nil
}

// bucketForVisibility resolves the bucket a new upload goes to. Public files
// fall back to the private bucket when no public bucket is configured
// (local/single-bucket dev layout). The result is persisted on asset_file at
// upload time; reads never re-derive it.
func (s *Service) bucketForVisibility(visibility string) string {
	if visibility == VisibilityPublic && s.publicBucket != "" {
		return s.publicBucket
	}
	return s.privateBucket
}

// bucketForFile resolves the bucket a stored file lives in from the value
// recorded at upload time, so later config changes (e.g. enabling
// FILESTORE_S3_PUBLIC_BUCKET) never point existing files at the wrong
// bucket. Legacy rows written before the bucket column keep the
// visibility-derived resolution.
func (s *Service) bucketForFile(item repo.AssetFile) string {
	if bucket := strings.TrimSpace(item.Bucket); bucket != "" {
		return bucket
	}
	return s.bucketForVisibility(item.Visibility)
}

func (s *Service) List(ctx context.Context, ownerType string, ownerID int64) (map[string]any, error) {
	items, err := s.repo.ListAssetFiles(ctx, repo.AssetFileFilter{OwnerType: ownerType, OwnerID: ownerID})
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(items))
	for _, item := range items {
		list = append(list, assetResp(item))
	}
	return map[string]any{"list": list, "total": len(list)}, nil
}

func (s *Service) Upload(ctx context.Context, cmd UploadCommand) (map[string]any, error) {
	if strings.TrimSpace(cmd.FileName) == "" {
		return nil, ErrInvalid
	}
	// A caller-supplied storage key that already has an active row in the
	// legacy deployment-level scope is reused instead of duplicated, so a
	// later metadata-only row can never silently shadow the real asset.
	if storageKey := strings.TrimSpace(cmd.StorageKey); storageKey != "" {
		existing, err := s.repo.ListActiveAssetFilesByStorageKey(ctx, storageKey)
		if err != nil {
			return nil, err
		}
		for _, item := range existing {
			if item.ProjectID == 0 {
				return assetResp(item), nil
			}
		}
	}
	file := repo.AssetFile{
		OriginalName:  strings.TrimSpace(cmd.FileName),
		ContentType:   cmd.ContentType,
		SizeBytes:     cmd.Size,
		Sha256:        cmd.Sha256,
		StorageDriver: s.driver,
		StorageKey:    cmd.StorageKey,
		Bucket:        s.privateBucket,
	}
	file.CreatedBy = cmd.UserID
	created, err := s.repo.CreateAssetFile(ctx, file)
	if err != nil {
		return nil, err
	}
	return assetResp(created), nil
}

func (s *Service) UploadContent(ctx context.Context, cmd UploadContentCommand) (map[string]any, error) {
	if strings.TrimSpace(cmd.FileName) == "" || cmd.Reader == nil {
		return nil, ErrInvalid
	}
	key := newStorageKey(cmd.FileName)
	hasher := sha256.New()
	if err := s.files.Put(ctx, s.privateBucket, key, io.TeeReader(cmd.Reader, hasher), filestore.PutMeta{
		ContentType: cmd.ContentType,
		SizeBytes:   cmd.Size,
	}); err != nil {
		return nil, err
	}
	sha := strings.TrimSpace(cmd.Sha256)
	if sha == "" {
		sha = hex.EncodeToString(hasher.Sum(nil))
	}
	file := repo.AssetFile{
		FileKey:       strings.ReplaceAll(key, "/", "_"),
		OriginalName:  strings.TrimSpace(cmd.FileName),
		ContentType:   cmd.ContentType,
		SizeBytes:     cmd.Size,
		Sha256:        sha,
		StorageDriver: s.driver,
		StorageKey:    key,
		Bucket:        s.privateBucket,
	}
	file.CreatedBy = cmd.UserID
	created, err := s.repo.CreateAssetFile(ctx, file)
	if err != nil {
		_ = s.files.Delete(ctx, s.privateBucket, key)
		return nil, err
	}
	return assetResp(created), nil
}

func (s *Service) Detail(ctx context.Context, id int64) (map[string]any, error) {
	item, err := s.repo.GetAssetFile(ctx, id)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return assetResp(item), nil
}

func (s *Service) Download(ctx context.Context, id int64) (map[string]any, error) {
	item, err := s.repo.GetAssetFile(ctx, id)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	resp := assetResp(item)
	resp["download"] = map[string]any{
		"mode":       "backend-stream",
		"driver":     item.StorageDriver,
		"storageKey": item.StorageKey,
	}
	return resp, nil
}

func (s *Service) Open(ctx context.Context, id int64) (repo.AssetFile, io.ReadCloser, filestore.FileMeta, error) {
	item, err := s.repo.GetAssetFile(ctx, id)
	if err != nil {
		return repo.AssetFile{}, nil, filestore.FileMeta{}, normalizeNotFound(err)
	}
	reader, meta, err := s.files.Get(ctx, s.bucketForFile(item), item.StorageKey)
	if err != nil {
		return repo.AssetFile{}, nil, filestore.FileMeta{}, err
	}
	if meta.ContentType == "" {
		meta.ContentType = item.ContentType
	}
	if meta.SizeBytes == 0 {
		meta.SizeBytes = item.SizeBytes
	}
	return item, reader, meta, nil
}

// OpenByStorageKey 按 storageKey 打开当前有效的 asset 文件内容(取最新一条绑定)。
func (s *Service) OpenByStorageKey(ctx context.Context, storageKey string) (repo.AssetFile, io.ReadCloser, filestore.FileMeta, error) {
	files, err := s.repo.ListActiveAssetFilesByStorageKey(ctx, strings.TrimSpace(storageKey))
	if err != nil {
		return repo.AssetFile{}, nil, filestore.FileMeta{}, err
	}
	if len(files) == 0 {
		return repo.AssetFile{}, nil, filestore.FileMeta{}, ErrNotFound
	}
	return s.Open(ctx, files[0].ID)
}

func (s *Service) Bind(ctx context.Context, cmd BindCommand) (map[string]any, error) {
	if cmd.FileID == 0 || strings.TrimSpace(cmd.OwnerType) == "" {
		return nil, ErrInvalid
	}
	bindingReq := repo.AssetBinding{
		AssetID:   cmd.FileID,
		OwnerType: cmd.OwnerType,
		OwnerID:   cmd.OwnerID,
	}
	bindingReq.CreatedBy = cmd.UserID
	binding, err := s.repo.BindAsset(ctx, bindingReq)
	if err != nil {
		return nil, err
	}
	return map[string]any{"binding": binding}, nil
}

func (s *Service) UploadAttachment(ctx context.Context, cmd AttachmentUploadCommand) (map[string]any, error) {
	if cmd.OwnerID == 0 || strings.TrimSpace(cmd.OwnerType) == "" {
		return nil, ErrInvalid
	}
	data, err := s.UploadContent(ctx, UploadContentCommand{
		FileName:    cmd.FileName,
		ContentType: cmd.ContentType,
		Size:        cmd.Size,
		Sha256:      cmd.Sha256,
		Reader:      cmd.Reader,
		UserID:      cmd.UserID,
	})
	if err != nil {
		return nil, err
	}
	fileID, _ := anyInt64(data["fileId"])
	if fileID <= 0 {
		return nil, ErrInvalid
	}
	if _, err := s.Bind(ctx, BindCommand{
		FileID:    fileID,
		OwnerType: cmd.OwnerType,
		OwnerID:   cmd.OwnerID,
		UserID:    cmd.UserID,
	}); err != nil {
		return nil, err
	}
	item, err := s.repo.GetAssetFile(ctx, fileID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return s.attachmentResp(ctx, item, cmd.UnsID, true)
}

func (s *Service) ListAttachments(ctx context.Context, ownerType string, ownerID, unsID int64, page, size int, includeFileURL bool) (map[string]any, error) {
	if ownerID <= 0 || strings.TrimSpace(ownerType) == "" {
		return nil, ErrInvalid
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	items, err := s.repo.ListAssetFiles(ctx, repo.AssetFileFilter{OwnerType: ownerType, OwnerID: ownerID})
	if err != nil {
		return nil, err
	}
	total := len(items)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	list := make([]map[string]any, 0, end-start)
	for _, item := range items[start:end] {
		resp, err := s.attachmentResp(ctx, item, unsID, includeFileURL)
		if err != nil {
			return nil, err
		}
		list = append(list, resp)
	}
	return map[string]any{
		"unsId": unsID,
		"list":  list,
		"total": total,
		"page":  page,
		"size":  size,
	}, nil
}

func (s *Service) Unbind(ctx context.Context, id int64) (map[string]any, error) {
	if err := s.repo.DeleteAssetBinding(ctx, id); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"deleted": true}, nil
}

// Binding 按 id 读取绑定行；解绑审计在删除前经此取 fileId/owner 信息。
func (s *Service) Binding(ctx context.Context, id int64) (repo.AssetBinding, error) {
	return s.repo.GetAssetBinding(ctx, id)
}

func assetResp(item repo.AssetFile) map[string]any {
	resp := map[string]any{
		"id":             item.ID,
		"fileId":         item.ID,
		"fileName":       item.OriginalName,
		"filePath":       strconv.FormatInt(item.ID, 10),
		"fileKey":        item.FileKey,
		"originalName":   item.OriginalName,
		"contentType":    item.ContentType,
		"sizeBytes":      item.SizeBytes,
		"sha256":         item.Sha256,
		"storageDriver":  item.StorageDriver,
		"storageKey":     item.StorageKey,
		"visibility":     item.Visibility,
		"status":         item.Status,
		"createdBy":      item.CreatedBy,
		"createdTime":    item.CreatedTime,
		"updatedTime":    item.UpdatedTime,
		"deletedTime":    item.DeletedTime,
		"attachmentPath": strconv.FormatInt(item.ID, 10),
	}
	if item.BindingID != 0 {
		resp["bindingId"] = item.BindingID
		resp["ownerType"] = item.OwnerType
		resp["ownerId"] = item.OwnerID
	}
	return resp
}

func (s *Service) attachmentResp(ctx context.Context, item repo.AssetFile, unsID int64, includeFileURL bool) (map[string]any, error) {
	resp := map[string]any{
		"unsId":       unsID,
		"fileName":    item.OriginalName,
		"filePath":    item.StorageKey,
		"contentType": item.ContentType,
		"size":        item.SizeBytes,
		"sha256":      item.Sha256,
		"createdAt":   item.CreatedTime,
	}
	if includeFileURL {
		fileURL, expiresAt, err := s.fileURL(ctx, item)
		if err != nil {
			return nil, err
		}
		resp["fileUrl"] = fileURL
		if expiresAt > 0 {
			resp["fileUrlExpiresAt"] = expiresAt
		}
	}
	return resp, nil
}

func (s *Service) fileURL(ctx context.Context, item repo.AssetFile) (string, int64, error) {
	if signed, ok, err := s.files.SignGetURL(ctx, s.bucketForFile(item), item.StorageKey, s.urlTTL, ""); err != nil {
		return "", 0, err
	} else if ok && strings.TrimSpace(signed) != "" {
		return s.rewriteToURLEndpoint(ctx, signed), time.Now().Add(s.urlTTL).UnixMilli(), nil
	}
	path := "/api/core/assets/" + strconv.FormatInt(item.ID, 10) + "/download"
	if s.publicBaseURL == "" {
		return path, 0, nil
	}
	return s.publicBaseURL + path, 0, nil
}

func anyInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

func newStorageKey(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("assets/%s/%d%s", time.Now().UTC().Format("20060102"), time.Now().UTC().UnixNano(), ext)
	}
	return fmt.Sprintf("assets/%s/%s%s", time.Now().UTC().Format("20060102"), hex.EncodeToString(buf), ext)
}

func normalizeNotFound(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func ParseOwnerID(value string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id
}

var (
	ErrInvalid  = errors.New("invalid asset")
	ErrNotFound = errors.New("asset not found")
)
