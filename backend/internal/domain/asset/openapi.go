package asset

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"backend/internal/httpx"
	"backend/internal/infra/filestore"
	"backend/internal/repo"
)

const (
	// maxOpenapiUploadSize caps a single OpenAPI direct upload at 1GB; multipart
	// uploads targeting large files share the same ceiling.
	maxOpenapiUploadSize = 1 * 1024 * 1024 * 1024
	// MaxDownloadExpireSec caps caller-provided presigned GET URL lifetimes
	// at 7 days; larger requests are clamped to this value.
	MaxDownloadExpireSec int64 = 604800
	// maxExternalIDLen mirrors the VARCHAR(64) app_instance_id/session_id
	// columns on asset_file; longer caller values are rejected, not truncated.
	maxExternalIDLen = 64
	// defaultUploadURLTTL is the presigned POST policy lifetime.
	defaultUploadURLTTL = time.Hour
	// defaultDownloadURLTTL is the default presigned GET URL lifetime.
	defaultDownloadURLTTL = time.Hour

	// VisibilityPublic marks files readable through the long-term public URL.
	VisibilityPublic = "public"
	// VisibilityPrivate marks files that require a presigned URL or backend proxy.
	VisibilityPrivate = "private"

	ownerTypeAIAppInstance = "aiAppInstance"
)

// forbiddenExtensions mirrors the SDK upload pre-check blacklist. The backend
// is the final authority and rejects these extensions again on upload.
var forbiddenExtensions = map[string]struct{}{
	"html": {}, "htm": {},
	"php": {}, "php5": {}, "php4": {}, "php3": {}, "php2": {}, "phtml": {}, "pht": {},
	"asp": {}, "aspx": {}, "asa": {}, "asax": {}, "ascx": {}, "ashx": {}, "asmx": {}, "cer": {},
	"jsp": {}, "jspa": {}, "jspx": {}, "jsw": {}, "jsv": {}, "jspf": {}, "jhtml": {},
	"htaccess": {}, "swf": {},
}

var (
	ErrPresignUnsupported = errors.New("presigned upload POST is not supported by the local storage driver; configure the s3/RustFS storage driver (local driver is for development only)")
	ErrForbidden          = errors.New("asset access denied")
)

type OpenapiUploadCommand struct {
	FileName      string
	ContentType   string
	Size          int64
	UseBy         string
	Visibility    string
	AppInstanceID string
	SessionID     string
	UserID        int64
}

type OpenapiUploadResult struct {
	FileID   int64
	FilePath string
	FileURL  string
	// PostURL 为客户端直传使用的预签名 POST 地址（multipart/form-data）。
	PostURL string
	// PostFields 为需随表单原样提交的隐藏字段（含 policy 与签名），file 字段最后追加。
	PostFields map[string]string
	ExpiresAt  int64
}

type OpenapiFileURLResult struct {
	FileURL   string
	ExpiresAt int64
}

func validateOpenapiUpload(cmd OpenapiUploadCommand) error {
	if err := validateOpenapiFileNameAndSize(cmd.FileName, cmd.Size); err != nil {
		return err
	}
	return validateOpenapiMeta(cmd.AppInstanceID, cmd.SessionID, cmd.Visibility, cmd.UseBy)
}

// validateOpenapiFileNameAndSize 校验文件名与申报大小（单发直传与分片上传共用）。
// 预签名模式只凭申报大小校验，超限由对象存储按 policy 直接拒绝；分片模式在
// complete 时按对象存储真实大小复核。
func validateOpenapiFileNameAndSize(name string, size int64) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrInvalid
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if _, forbidden := forbiddenExtensions[ext]; forbidden {
		return fmt.Errorf("%w: file extension .%s is not allowed", ErrInvalid, ext)
	}
	if size <= 0 || size > maxOpenapiUploadSize {
		return fmt.Errorf("%w: file size %d must be greater than 0 and within the 1GB limit", ErrInvalid, size)
	}
	return nil
}

// validateOpenapiMeta 校验 OpenAPI 上传的公共元数据字段（单发直传与分片上传共用）。
func validateOpenapiMeta(appInstanceID, sessionID, visibility, useBy string) error {
	if len(strings.TrimSpace(appInstanceID)) > maxExternalIDLen {
		return fmt.Errorf("%w: appInstanceId exceeds the 64-character limit", ErrInvalid)
	}
	if len(strings.TrimSpace(sessionID)) > maxExternalIDLen {
		return fmt.Errorf("%w: sessionId exceeds the 64-character limit", ErrInvalid)
	}
	switch strings.ToLower(strings.TrimSpace(visibility)) {
	case "", VisibilityPublic, VisibilityPrivate:
	default:
		return fmt.Errorf("%w: visibility must be public or private", ErrInvalid)
	}
	switch strings.ToLower(strings.TrimSpace(useBy)) {
	case "", "user", "workspace", "platform":
	default:
		return fmt.Errorf("%w: useBy must be user, workspace or platform", ErrInvalid)
	}
	return nil
}

// CreatePresignedUpload 签发预签名 POST（POST policy）上传表单，并以 temp 状态登记
// asset 元数据。存储对象 key 始终按调用方的项目作用域隔离。
// POST policy 将 content-length-range [1, size] 签入 policy：对象实际大小超出声明值时
// 由对象存储直接 403 拒绝，客户端自报的 Size 因此具备服务端强制力。
func (s *Service) CreatePresignedUpload(ctx context.Context, cmd OpenapiUploadCommand) (*OpenapiUploadResult, error) {
	if err := validateOpenapiUpload(cmd); err != nil {
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
	key := newScopedStorageKey(projectID, cmd.AppInstanceID, cmd.SessionID, cmd.FileName)
	bucket := s.bucketForVisibility(visibility)
	postURL, postFields, ok, err := s.files.SignPostURL(ctx, bucket, key, cmd.ContentType, cmd.Size, defaultUploadURLTTL)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(postURL) == "" {
		return nil, ErrPresignUnsupported
	}
	// 上传地址同样按 FILESTORE_PUBLIC_BASE_URL > 请求派生 endpoint 改写，
	// 使浏览器直传走当前入口（POST policy 签名不含 host，改写安全）
	postURL = s.rewriteToURLEndpoint(ctx, postURL)
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
	}
	file.CreatedBy = cmd.UserID
	var binding *repo.AssetBinding
	if appInstanceID := strings.TrimSpace(cmd.AppInstanceID); appInstanceID != "" {
		metadata, _ := json.Marshal(map[string]string{
			"appInstanceId": appInstanceID,
			"sessionId":     strings.TrimSpace(cmd.SessionID),
		})
		row := repo.AssetBinding{
			OwnerType: ownerTypeAIAppInstance,
			OwnerID:   ParseOwnerID(appInstanceID),
			Metadata:  metadata,
		}
		row.CreatedBy = cmd.UserID
		binding = &row
	}
	created, err := s.repo.CreateAssetFileWithBinding(ctx, file, binding)
	if err != nil {
		return nil, err
	}
	result := &OpenapiUploadResult{
		FileID:     created.ID,
		FilePath:   key,
		PostURL:    postURL,
		PostFields: postFields,
	}
	if visibility == VisibilityPublic {
		result.FileURL = s.PublicURL(ctx, created)
	} else {
		result.ExpiresAt = time.Now().Add(defaultUploadURLTTL).UnixMilli()
	}
	return result, nil
}

// ResolveOpenapiFile loads a file by storage key and verifies the caller can
// access the project the file belongs to. The scope is derived from the
// stored file (owner/member of asset_file.project_id) instead of the caller's
// current lowest project, so project membership changes never strand existing
// files. Deployment-level files (project_id = 0) keep the original rule: the
// caller's resolved scope must still be deployment-level.
func (s *Service) ResolveOpenapiFile(ctx context.Context, filePath string, userID int64) (repo.AssetFile, error) {
	storageKey := strings.TrimSpace(filePath)
	if storageKey == "" {
		return repo.AssetFile{}, ErrInvalid
	}
	items, err := s.repo.ListActiveAssetFilesByStorageKey(ctx, storageKey)
	if err != nil {
		return repo.AssetFile{}, normalizeNotFound(err)
	}
	if len(items) == 0 {
		return repo.AssetFile{}, ErrNotFound
	}
	// Newest accessible row wins; rows in other scopes must not shadow it.
	for _, item := range items {
		ok, err := s.canAccessFile(ctx, item.ProjectID, userID)
		if err != nil {
			return repo.AssetFile{}, err
		}
		if ok {
			return item, nil
		}
	}
	return repo.AssetFile{}, ErrForbidden
}

// canAccessFile reports whether the user may operate on a file recorded in
// the given project scope. Project files require ownership or membership of
// that project; deployment-level files (projectID 0) require the caller's
// resolved scope to still be deployment-level, matching the upload-time rule.
func (s *Service) canAccessFile(ctx context.Context, projectID, userID int64) (bool, error) {
	if projectID == 0 {
		scope, err := s.projectScope(ctx, userID)
		if err != nil {
			return false, err
		}
		return scope == 0, nil
	}
	return repo.NewProjectRepo(ctx).UserHasProjectAccess(ctx, userID, projectID)
}

// OpenapiFileURL returns the access URL for a file: public files get a
// long-term public URL without expiry; private files get a presigned GET URL.
func (s *Service) OpenapiFileURL(ctx context.Context, item repo.AssetFile, expiredSec int64) (OpenapiFileURLResult, error) {
	if item.Visibility == VisibilityPublic {
		return OpenapiFileURLResult{FileURL: s.PublicURL(ctx, item)}, nil
	}
	ttl := resolveDownloadTTL(expiredSec)
	signed, expiresAt, err := s.SignGetURL(ctx, item, ttl, "")
	if err != nil {
		return OpenapiFileURLResult{}, err
	}
	return OpenapiFileURLResult{FileURL: signed, ExpiresAt: expiresAt}, nil
}

// resolveDownloadTTL normalizes the caller-requested download URL lifetime:
// non-positive values fall back to the default, values above the server-side
// maximum (MaxDownloadExpireSec) are clamped to it.
func resolveDownloadTTL(expiredSec int64) time.Duration {
	switch {
	case expiredSec <= 0:
		return defaultDownloadURLTTL
	case expiredSec > MaxDownloadExpireSec:
		return time.Duration(MaxDownloadExpireSec) * time.Second
	default:
		return time.Duration(expiredSec) * time.Second
	}
}

// SignGetURL signs a short-lived GET URL for private files. Drivers that
// cannot sign (local, development only) return ErrPresignUnsupported.
// 签名地址按 FILESTORE_PUBLIC_BASE_URL > ctx 请求派生 endpoint 改写 scheme+host，
// 两者皆缺时保留 store 侧 FILESTORE_S3_PUBLIC_ENDPOINT > 内部 endpoint 行为。
func (s *Service) SignGetURL(ctx context.Context, item repo.AssetFile, ttl time.Duration, contentDisposition string) (string, int64, error) {
	signed, ok, err := s.files.SignGetURL(ctx, s.bucketForFile(item), item.StorageKey, ttl, contentDisposition)
	if err != nil {
		return "", 0, err
	}
	if !ok || strings.TrimSpace(signed) == "" {
		return "", 0, ErrPresignUnsupported
	}
	return s.rewriteToURLEndpoint(ctx, signed), time.Now().Add(ttl).UnixMilli(), nil
}

// PublicURL builds the long-term public URL for a public-visibility file.
// Division of labor in the two-bucket layout:
//   - s3 driver and the file was actually recorded in the configured public
//     bucket: the URL is a direct bucket link (anonymous read).
//     FILESTORE_PUBLIC_BASE_URL acts as the URL prefix for CDN/custom-domain
//     setups; otherwise the link first honors the request-derived endpoint
//     (scheme://host of the current request, served by the gateway's bucket
//     prefix proxy), then the configured S3 public endpoint, and finally the
//     internal S3 endpoint with the configured path-style/virtual-hosted lookup.
//   - otherwise (no public bucket, local driver, or legacy files recorded in
//     the private bucket before the public bucket was configured): the URL
//     points at the backend public capability route
//     (/openapi/v1/assets/files/public), which streams the object through the
//     backend; the random storage key is unguessable and the route only
//     serves public-visibility files.
func (s *Service) PublicURL(ctx context.Context, item repo.AssetFile) string {
	if s.driver == "s3" && s.publicBucket != "" && s.bucketForFile(item) == s.publicBucket {
		return s.publicBucketURL(ctx, item.StorageKey)
	}
	path := "/openapi/v1/assets/files/public?filePath=" + url.QueryEscape(item.StorageKey)
	if s.publicBaseURL == "" {
		return path
	}
	return s.publicBaseURL + path
}

// publicBucketURL builds the anonymous direct link to the public bucket:
// FILESTORE_PUBLIC_BASE_URL when set (CDN/custom domain); otherwise the
// request-derived endpoint (scheme://host of the current request — in the
// gateway-proxy layout this is the platform entrance, which reverse-proxies
// the bucket prefixes to the object store); otherwise, when a client-facing
// S3 public endpoint is configured, a path-style link under it; otherwise a
// URL derived from the S3 endpoint and public bucket name in the configured
// bucket lookup style (path-style vs virtual-hosted).
func (s *Service) publicBucketURL(ctx context.Context, key string) string {
	escaped := escapeStorageKeyPath(key)
	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/" + escaped
	}
	if endpoint := s.requestDerivedEndpoint(ctx); endpoint != "" {
		return fmt.Sprintf("%s/%s/%s", endpoint, s.publicBucket, escaped)
	}
	if s.s3PublicEndpoint != "" {
		return fmt.Sprintf("%s/%s/%s", s.s3PublicEndpoint, s.publicBucket, escaped)
	}
	endpoint := s.s3Endpoint
	useSSL := s.s3UseSSL
	if rest, ok := strings.CutPrefix(endpoint, "http://"); ok {
		useSSL, endpoint = false, rest
	} else if rest, ok := strings.CutPrefix(endpoint, "https://"); ok {
		useSSL, endpoint = true, rest
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if s.s3PathStyle {
		return fmt.Sprintf("%s://%s/%s/%s", scheme, endpoint, s.publicBucket, escaped)
	}
	return fmt.Sprintf("%s://%s.%s/%s", scheme, s.publicBucket, endpoint, escaped)
}

// requestDerivedEndpoint 返回按当前请求派生的对外 endpoint（scheme://host）。
// requestauthority 中间件未注入 authority（无 HTTP 请求上下文，如后台任务）时返回空。
func (s *Service) requestDerivedEndpoint(ctx context.Context) string {
	scheme, host, ok := httpx.AuthorityFrom(ctx)
	if !ok {
		return ""
	}
	return scheme + "://" + host
}

// rewriteToURLEndpoint 按显式配置优先的 endpoint 改写已签名 URL 的 scheme+host：
// FILESTORE_PUBLIC_BASE_URL（显式配置，最高）> ctx 请求派生 endpoint；两者皆缺时
// 原样返回，保留 store 侧 FILESTORE_S3_PUBLIC_ENDPOINT > 内部 endpoint 的既有行为。
// 改写只动 scheme/host，path/query 原样，经网关同端口桶前缀代理后签名仍有效。
func (s *Service) rewriteToURLEndpoint(ctx context.Context, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	endpoint := s.publicBaseURL
	if endpoint == "" {
		endpoint = s.requestDerivedEndpoint(ctx)
	}
	if endpoint == "" {
		return raw
	}
	rewritten, err := filestore.RewriteURLHost(raw, endpoint)
	if err != nil {
		return raw
	}
	return rewritten
}

// escapeStorageKeyPath URL-escapes each path segment of a storage key while
// keeping the "/" separators intact, so the key can be embedded in a URL path.
func escapeStorageKeyPath(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// ResolvePublicFile loads a file by storage key only when it is marked
// public. Private files report not-found so the public route never leaks
// their existence. When several active rows share a key, the newest public
// row wins: a non-public row must never shadow an existing public
// capability URL.
func (s *Service) ResolvePublicFile(ctx context.Context, filePath string) (repo.AssetFile, error) {
	storageKey := strings.TrimSpace(filePath)
	if storageKey == "" {
		return repo.AssetFile{}, ErrInvalid
	}
	items, err := s.repo.ListActiveAssetFilesByStorageKey(ctx, storageKey)
	if err != nil {
		return repo.AssetFile{}, normalizeNotFound(err)
	}
	for _, item := range items {
		if item.Visibility == VisibilityPublic {
			return item, nil
		}
	}
	return repo.AssetFile{}, ErrNotFound
}

// DeleteOpenapiFile removes the storage object and soft-deletes the file and
// its bindings.
func (s *Service) DeleteOpenapiFile(ctx context.Context, item repo.AssetFile) error {
	if err := s.files.Delete(ctx, s.bucketForFile(item), item.StorageKey); err != nil {
		return err
	}
	return normalizeNotFound(s.repo.SoftDeleteAssetFile(ctx, item.ID))
}

func (s *Service) projectScope(ctx context.Context, userID int64) (int64, error) {
	return repo.NewProjectRepo(ctx).ResolveUserProjectID(ctx, userID)
}

// newScopedStorageKey builds the storage object key for OpenAPI uploads. The
// project scope is always the first path segment so different projects can
// never share a key namespace:
//
//	{projectID}/assets/{date}/{random}{ext}
//	{projectID}/assets/{appInstanceId}/{sessionId}/{date}/{random}{ext}
func newScopedStorageKey(projectID int64, appInstanceID, sessionID, fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	buf := make([]byte, 12)
	random := strconv.FormatInt(time.Now().UTC().UnixNano(), 16)
	if _, err := rand.Read(buf); err == nil {
		random = hex.EncodeToString(buf)
	}
	parts := []string{strconv.FormatInt(projectID, 10), "assets"}
	if segment := sanitizeKeySegment(appInstanceID); segment != "" {
		parts = append(parts, segment)
		if sessionSegment := sanitizeKeySegment(sessionID); sessionSegment != "" {
			parts = append(parts, sessionSegment)
		}
	}
	parts = append(parts, time.Now().UTC().Format("20060102"), random+ext)
	return strings.Join(parts, "/")
}

// sanitizeKeySegment reduces a caller-supplied id to a single safe path
// segment: no slashes, no traversal, bounded length.
func sanitizeKeySegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" || out == ".." {
		return ""
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
