package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/zeromicro/go-zero/core/logx"
)

type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	PublicBucket    string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	UseSSL          bool
	PathStyle       bool
	// PublicEndpoint, when set, is the client-facing S3 base address presigned
	// URLs are rewritten to before they are returned to callers. Deployments
	// where Endpoint points at an in-cluster address (e.g. the built-in RustFS
	// service) use it so upload/download URLs stay reachable from outside the
	// cluster. Signing itself always uses the primary Endpoint: the client is
	// expected to reach PublicEndpoint through a gateway reverse proxy that
	// rewrites the Host header back to the signing host, so the SigV4
	// signature stays valid for the rewritten URL.
	PublicEndpoint string
	// BootstrapBuckets 为 true 时（仅内置 RustFS 模式）由调用方在启动期
	// 调用 EnsureBuckets 幂等建桶；外置 S3 必须为 false，bucket 由运维预建。
	BootstrapBuckets bool
}

type S3Store struct {
	client         *minio.Client
	bucket         string
	publicBucket   string
	publicEndpoint string
}

// resolveBucket returns the explicitly requested bucket, falling back to the
// configured default (private) bucket when empty.
func (s *S3Store) resolveBucket(bucket string) string {
	if strings.TrimSpace(bucket) == "" {
		return s.bucket
	}
	return strings.TrimSpace(bucket)
}

func NewS3(conf S3Config) (*S3Store, error) {
	endpoint := strings.TrimSpace(conf.Endpoint)
	if endpoint == "" || strings.TrimSpace(conf.Bucket) == "" {
		return nil, errors.New("s3 endpoint and bucket are required")
	}
	client, err := newS3Client(endpoint, conf)
	if err != nil {
		return nil, err
	}
	store := &S3Store{client: client, bucket: strings.TrimSpace(conf.Bucket)}
	store.publicBucket = strings.TrimSpace(conf.PublicBucket)
	if publicEndpoint := strings.TrimSpace(conf.PublicEndpoint); publicEndpoint != "" {
		// Validate once so a malformed value fails fast at startup instead of
		// surfacing as broken presigned URLs on every request.
		if _, err := parsePublicEndpoint(publicEndpoint); err != nil {
			return nil, err
		}
		store.publicEndpoint = publicEndpoint
	}
	return store, nil
}

const (
	ensureBucketInitialBackoff = time.Second
	ensureBucketMaxBackoff     = 5 * time.Second
	ensureBucketTimeout        = 60 * time.Second
)

// EnsureBuckets 启动时幂等确保私有/公有 bucket 存在，公有桶带匿名下载策略，
// 替代 deploy 侧 rustfs-init 一次性容器（minio/mc）的建桶职责。仅在内置
// RustFS 模式（FILESTORE_S3_BOOTSTRAP_BUCKETS=true）由 asset service 调用；
// 外置 S3 的 bucket 与策略由运维预建，backend 不做 bucket 级写操作。
// 内置 RustFS 启动慢于 backend 时按退避重试；AccessDenied（凭证无建桶权限、
// 桶由运维预建）降级为信息日志不阻断；其余错误重试至超时后返回 error，
// 与 rustfs-init 失败即安装失败的语义对齐。
func (s *S3Store) EnsureBuckets(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ensureBucketTimeout)
	defer cancel()
	// 公有桶与私有桶同名时，唯一的一次确保必须带匿名下载策略，
	// 否则 PublicURL 产出的未签名直链会 403（rustfs-init 对同名场景同样下发策略）。
	if s.publicBucket == "" || s.publicBucket == s.bucket {
		return s.ensureBucket(ctx, s.bucket, s.publicBucket == s.bucket)
	}
	if err := s.ensureBucket(ctx, s.bucket, false); err != nil {
		return err
	}
	return s.ensureBucket(ctx, s.publicBucket, true)
}

func (s *S3Store) ensureBucket(ctx context.Context, bucket string, publicRead bool) error {
	backoff := ensureBucketInitialBackoff
	for {
		exists, err := s.client.BucketExists(ctx, bucket)
		if err == nil && !exists {
			err = s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
			if err != nil {
				// 并发创建或已存在视为成功（对齐 mc mb 的容错语义）
				switch minio.ToErrorResponse(err).Code {
				case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
					err = nil
				}
			}
		}
		if err == nil && publicRead {
			err = s.client.SetBucketPolicy(ctx, bucket, anonymousDownloadPolicy(bucket))
		}
		if err == nil {
			return nil
		}
		if minio.ToErrorResponse(err).Code == "AccessDenied" {
			// 预期的降级路径（凭证无建桶权限、桶由运维预建），不是故障，
			// 不用 ERROR 以免与可行动的存储错误混淆。
			logx.WithContext(ctx).Infof("filestore: skip ensuring s3 bucket %q (AccessDenied, expected pre-created by operator): %v", bucket, err)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ensure s3 bucket %q: %w (last error: %v)", bucket, ctx.Err(), err)
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, ensureBucketMaxBackoff)
	}
}

// anonymousDownloadPolicy 等价 `mc anonymous set download`：允许匿名 GetObject。
// gateway /public/ 前缀直链不签名，依赖该策略才能匿名下载。
func anonymousDownloadPolicy(bucket string) string {
	return fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
}

// parsePublicEndpoint validates the client-facing endpoint used to rewrite
// presigned URLs: it must parse as an absolute URL with a host. A missing
// scheme is tolerated and defaults to http, mirroring how Endpoint is
// treated by newS3Client.
func parsePublicEndpoint(publicEndpoint string) (*url.URL, error) {
	raw := strings.TrimSpace(publicEndpoint)
	if raw == "" {
		return nil, errors.New("s3 public endpoint is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid s3 public endpoint %q", publicEndpoint)
	}
	return u, nil
}

// RewriteURLHost returns rawURL with its scheme and host replaced by those of
// publicEndpoint; path and query are kept byte-for-byte so the SigV4
// signature computed for the original host stays valid once the gateway
// rewrites the Host header back to the signing host. 供调用方按请求动态传入
// public endpoint 改写预签名 URL / 公开桶直链（见 domain/asset 的 endpoint 优先级链）。
func RewriteURLHost(rawURL, publicEndpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid url to rewrite: %q", rawURL)
	}
	pub, err := parsePublicEndpoint(publicEndpoint)
	if err != nil {
		return "", err
	}
	u.Scheme = pub.Scheme
	u.Host = pub.Host
	return u.String(), nil
}

func newS3Client(endpoint string, conf S3Config) (*minio.Client, error) {
	useSSL := conf.UseSSL
	if strings.HasPrefix(endpoint, "http://") {
		useSSL = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		useSSL = true
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	options := &minio.Options{
		Creds:  credentials.NewStaticV4(strings.TrimSpace(conf.AccessKeyID), strings.TrimSpace(conf.SecretAccessKey), strings.TrimSpace(conf.SessionToken)),
		Secure: useSSL,
		Region: strings.TrimSpace(conf.Region),
	}
	if conf.PathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}
	options.Transport = newS3Transport()
	return minio.New(endpoint, options)
}

// newS3Transport 为 minio 客户端提供带显式超时的 http.Transport。
// 默认 transport 无 dial/响应头超时，endpoint 不可达时请求会静默挂起，
// 直到被上层请求超时砍掉，表现为无业务错误的 503。
func newS3Transport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		base = &http.Transport{}
	}
	t := base.Clone()
	t.DialContext = (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	t.TLSHandshakeTimeout = 5 * time.Second
	t.ResponseHeaderTimeout = 60 * time.Second
	t.IdleConnTimeout = 90 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second
	return t
}

// signURL rewrites a freshly issued presigned URL to the client-facing
// endpoint when one is configured; otherwise it is returned unchanged.
func (s *S3Store) signURL(rawURL string) (string, error) {
	if s.publicEndpoint == "" {
		return rawURL, nil
	}
	return RewriteURLHost(rawURL, s.publicEndpoint)
}

func (s *S3Store) Put(ctx context.Context, bucket, key string, r io.Reader, meta PutMeta) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.resolveBucket(bucket), key, r, meta.SizeBytes, minio.PutObjectOptions{
		ContentType:      meta.ContentType,
		DisableMultipart: meta.SizeBytes >= 0 && meta.SizeBytes <= 5*1024*1024*1024,
	})
	return err
}

func (s *S3Store) Get(ctx context.Context, bucket, key string) (io.ReadCloser, FileMeta, error) {
	if err := validateObjectKey(key); err != nil {
		return nil, FileMeta{}, err
	}
	obj, err := s.client.GetObject(ctx, s.resolveBucket(bucket), key, minio.GetObjectOptions{})
	if err != nil {
		return nil, FileMeta{}, err
	}
	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, FileMeta{}, err
	}
	return obj, FileMeta{
		ContentType: stat.ContentType,
		SizeBytes:   stat.Size,
		ETag:        stat.ETag,
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, bucket, key string) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	return s.client.RemoveObject(ctx, s.resolveBucket(bucket), key, minio.RemoveObjectOptions{})
}

func (s *S3Store) Exists(ctx context.Context, bucket, key string) (bool, error) {
	if err := validateObjectKey(key); err != nil {
		return false, err
	}
	_, err := s.client.StatObject(ctx, s.resolveBucket(bucket), key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" || resp.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

func (s *S3Store) SignGetURL(ctx context.Context, bucket, key string, ttl time.Duration, contentDisposition string) (string, bool, error) {
	if err := validateObjectKey(key); err != nil {
		return "", false, err
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	reqParams := url.Values{}
	if strings.TrimSpace(contentDisposition) != "" {
		reqParams.Set("response-content-disposition", strings.TrimSpace(contentDisposition))
	}
	u, err := s.client.PresignedGetObject(ctx, s.resolveBucket(bucket), key, ttl, reqParams)
	if err != nil {
		return "", false, err
	}
	signed, err := s.signURL(u.String())
	if err != nil {
		return "", false, err
	}
	return signed, true, nil
}

// SignPostURL 通过 minio-go PostPolicy 签发预签名 POST 上传表单：key 精确匹配、
// content-length-range [1, sizeBytes]、ttl 后过期（默认与既有预签名 PUT 一致为 1 小时），
// contentType 非空时附加 Content-Type 条件。
// 返回的 postURL 走与预签名 GET 相同的 public endpoint 改写（POST policy 的签名只覆盖
// policy 内容，不含 host，改写 host 不影响签名校验）；fields 需随表单原样提交，
// file 字段必须最后追加。
func (s *S3Store) SignPostURL(ctx context.Context, bucket, key, contentType string, sizeBytes int64, ttl time.Duration) (string, map[string]string, bool, error) {
	if err := validateObjectKey(key); err != nil {
		return "", nil, false, err
	}
	if sizeBytes <= 0 {
		return "", nil, false, errors.New("sizeBytes must be positive")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	policy := minio.NewPostPolicy()
	if err := policy.SetBucket(s.resolveBucket(bucket)); err != nil {
		return "", nil, false, err
	}
	if err := policy.SetKey(key); err != nil {
		return "", nil, false, err
	}
	if err := policy.SetExpires(time.Now().UTC().Add(ttl)); err != nil {
		return "", nil, false, err
	}
	if err := policy.SetContentLengthRange(1, sizeBytes); err != nil {
		return "", nil, false, err
	}
	// contentType 非空时签入 Content-Type 条件：minio-go 会把它放进返回的表单字段，
	// 客户端按契约原样回显后对象保留声明的媒体类型（避免公有文件直链被当作
	// octet-stream 下载）；为空则不约束。
	if ct := strings.TrimSpace(contentType); ct != "" {
		if err := policy.SetContentType(ct); err != nil {
			return "", nil, false, err
		}
	}
	u, formData, err := s.client.PresignedPostPolicy(ctx, policy)
	if err != nil {
		return "", nil, false, err
	}
	signed, err := s.signURL(u.String())
	if err != nil {
		return "", nil, false, err
	}
	return signed, formData, true, nil
}

func validateObjectKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, "\x00") || strings.HasPrefix(key, "/") || strings.Contains(key, "../") {
		return errors.New("invalid file key")
	}
	return nil
}

// core 暴露 minio-go 的分片上传控制面（Core 内嵌 *Client，复用同一个签名客户端）。
func (s *S3Store) core() minio.Core {
	return minio.Core{Client: s.client}
}

// CreateMultipartUpload 发起分片上传，返回对象存储侧的 uploadId。
func (s *S3Store) CreateMultipartUpload(ctx context.Context, bucket, key, contentType string) (string, error) {
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	return s.core().NewMultipartUpload(ctx, s.resolveBucket(bucket), key, minio.PutObjectOptions{
		ContentType: strings.TrimSpace(contentType),
	})
}

// CompleteMultipartUpload 用客户端回传的 (partNumber, etag) 列表合并已上传分片。
func (s *S3Store) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletePart) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	if strings.TrimSpace(uploadID) == "" {
		return errors.New("uploadID is required")
	}
	complete := make([]minio.CompletePart, 0, len(parts))
	for _, part := range parts {
		complete = append(complete, minio.CompletePart{
			PartNumber: int(part.PartNumber),
			ETag:       part.ETag,
		})
	}
	_, err := s.core().CompleteMultipartUpload(ctx, s.resolveBucket(bucket), key, strings.TrimSpace(uploadID), complete, minio.PutObjectOptions{})
	return err
}

// AbortMultipartUpload 丢弃未完成的分片上传会话。
func (s *S3Store) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	if strings.TrimSpace(uploadID) == "" {
		return errors.New("uploadID is required")
	}
	return s.core().AbortMultipartUpload(ctx, s.resolveBucket(bucket), key, strings.TrimSpace(uploadID))
}

// ListParts 分页拉取当前上传会话的全部已上传分片（含对象存储记录的真实大小）。
func (s *S3Store) ListParts(ctx context.Context, bucket, key, uploadID string) ([]PartInfo, error) {
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	if strings.TrimSpace(uploadID) == "" {
		return nil, errors.New("uploadID is required")
	}
	bucketName := s.resolveBucket(bucket)
	out := make([]PartInfo, 0, 8)
	marker := 0
	for {
		result, err := s.core().ListObjectParts(ctx, bucketName, key, strings.TrimSpace(uploadID), marker, 1000)
		if err != nil {
			return nil, err
		}
		for _, part := range result.ObjectParts {
			out = append(out, PartInfo{
				PartNumber: int32(part.PartNumber),
				SizeBytes:  part.Size,
				ETag:       part.ETag,
			})
		}
		if !result.IsTruncated || result.NextPartNumberMarker <= marker {
			break
		}
		marker = result.NextPartNumberMarker
	}
	return out, nil
}

// SignPutPartURL 签发单个分片的预签名 PUT 地址：partNumber 与 uploadId 作为
// query 参数签入，客户端直接 PUT 到该地址即可上传对应分片。URL 走与预签名
// GET/POST 相同的 public endpoint 改写逻辑。
func (s *S3Store) SignPutPartURL(ctx context.Context, bucket, key, uploadID string, partNumber int32, ttl time.Duration) (string, bool, error) {
	if err := validateObjectKey(key); err != nil {
		return "", false, err
	}
	if partNumber <= 0 {
		return "", false, errors.New("partNumber must be positive")
	}
	if strings.TrimSpace(uploadID) == "" {
		return "", false, errors.New("uploadID is required")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	reqParams := url.Values{}
	reqParams.Set("partNumber", strconv.Itoa(int(partNumber)))
	reqParams.Set("uploadId", strings.TrimSpace(uploadID))
	u, err := s.client.Presign(ctx, http.MethodPut, s.resolveBucket(bucket), key, ttl, reqParams)
	if err != nil {
		return "", false, err
	}
	signed, err := s.signURL(u.String())
	if err != nil {
		return "", false, err
	}
	return signed, true, nil
}
