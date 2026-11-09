package asset

// 测试目标：openapi 上传纯函数（openapi.go 无 DB 部分）。
// 覆盖的正常路径：文件名/大小校验、元数据字段校验、下载 TTL 归一化、存储 key 生成与
// 段清洗、PublicURL/publicBucketURL 分支、文件 URL endpoint 优先级链、ParseOwnerID。
// 覆盖的异常情况：非法扩展名、空文件名、越界大小、超长外部 ID、非法 visibility/useBy、
// 无 ctx authority 时 URL 不改写。

import (
	"context"
	"strings"
	"testing"
	"time"

	"backend/internal/httpx"
	"backend/internal/repo"
)

func TestValidateOpenapiFileNameAndSize(t *testing.T) {
	requireNoErr := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	requireNoErr(validateOpenapiFileNameAndSize("a.txt", 1))
	requireNoErr(validateOpenapiFileNameAndSize("a.txt", maxOpenapiUploadSize))

	if err := validateOpenapiFileNameAndSize("", 1); err == nil {
		t.Fatal("empty name should error")
	}
	if err := validateOpenapiFileNameAndSize("a.html", 1); err == nil {
		t.Fatal("forbidden extension should error")
	}
	if err := validateOpenapiFileNameAndSize("a.php", 1); err == nil {
		t.Fatal("forbidden php extension should error")
	}
	if err := validateOpenapiFileNameAndSize("a.htaCcess", 1); err == nil {
		t.Fatal("case-insensitive forbidden extension")
	}
	if err := validateOpenapiFileNameAndSize("a.txt", 0); err == nil {
		t.Fatal("zero size should error")
	}
	if err := validateOpenapiFileNameAndSize("a.txt", -1); err == nil {
		t.Fatal("negative size should error")
	}
	if err := validateOpenapiFileNameAndSize("a.txt", maxOpenapiUploadSize+1); err == nil {
		t.Fatal("over 1GB should error")
	}
}

func TestValidateOpenapiMeta(t *testing.T) {
	requireNoErr := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	requireNoErr(validateOpenapiMeta("", "", "", ""))
	requireNoErr(validateOpenapiMeta("inst-1", "sess-1", VisibilityPublic, "user"))
	requireNoErr(validateOpenapiMeta("inst-1", "sess-1", "", "platform"))

	if err := validateOpenapiMeta(strings.Repeat("a", 65), "", "", ""); err == nil {
		t.Fatal("overlong appInstanceId should error")
	}
	if err := validateOpenapiMeta("", strings.Repeat("b", 65), "", ""); err == nil {
		t.Fatal("overlong sessionId should error")
	}
	if err := validateOpenapiMeta("", "", "world", ""); err == nil {
		t.Fatal("invalid visibility should error")
	}
	if err := validateOpenapiMeta("", "", "", "robot"); err == nil {
		t.Fatal("invalid useBy should error")
	}
}

func TestResolveDownloadTTL(t *testing.T) {
	if resolveDownloadTTL(0) != time.Hour {
		t.Fatal("zero should default to 1h")
	}
	if resolveDownloadTTL(-5) != time.Hour {
		t.Fatal("negative should default to 1h")
	}
	if resolveDownloadTTL(60) != 60*time.Second {
		t.Fatal("positive should be honored")
	}
	if resolveDownloadTTL(MaxDownloadExpireSec+1) != time.Duration(MaxDownloadExpireSec)*time.Second {
		t.Fatal("over max should clamp")
	}
}

func TestSanitizeKeySegment(t *testing.T) {
	if sanitizeKeySegment("") != "" {
		t.Fatal("empty should be empty")
	}
	if sanitizeKeySegment("  ") != "" {
		t.Fatal("blank should be empty")
	}
	if got := sanitizeKeySegment("../.."); got != "_" {
		t.Fatalf("traversal segment = %q, want _", got)
	}
	if sanitizeKeySegment("..") != "" {
		t.Fatal("dot-dot should be empty")
	}
	if got := sanitizeKeySegment("app-1.2_3"); got != "app-1.2_3" {
		t.Fatalf("safe segment = %q", got)
	}
	if got := sanitizeKeySegment("a/b\\c d"); got != "a_b_c_d" {
		t.Fatalf("unsafe chars = %q", got)
	}
	if got := sanitizeKeySegment(strings.Repeat("a", 100)); len(got) != 64 {
		t.Fatalf("overlong segment length = %d", len(got))
	}
	if got := sanitizeKeySegment(".hidden."); got != "hidden" {
		t.Fatalf("dot trim = %q", got)
	}
}

func TestNewScopedStorageKey(t *testing.T) {
	key := newScopedStorageKey(42, "", "", "report.pdf")
	if !strings.HasPrefix(key, "42/assets/") || !strings.HasSuffix(key, ".pdf") {
		t.Fatalf("key = %q", key)
	}
	segments := strings.Split(key, "/")
	if len(segments) != 4 {
		t.Fatalf("key segments = %v", segments)
	}
	// 带 appInstanceId/sessionId
	key = newScopedStorageKey(7, "app-1", "sess-1", "x.png")
	if !strings.HasPrefix(key, "7/assets/app-1/sess-1/") {
		t.Fatalf("key with ids = %q", key)
	}
	// 随机段长度（hex 12 字节 = 24 字符 + ext）
	last := segments[len(segments)-1]
	if len(last) < 24 {
		t.Fatalf("random segment too short: %q", last)
	}
}

func TestPublicURL(t *testing.T) {
	svc := &Service{driver: "local", publicBaseURL: "http://localhost"}
	item := repoAssetFile("k1")
	if url := svc.PublicURL(context.Background(), item); url != "http://localhost/openapi/v1/assets/files/public?filePath=k1" {
		t.Fatalf("local public url = %q", url)
	}
	svc.publicBaseURL = ""
	if url := svc.PublicURL(context.Background(), item); url != "/openapi/v1/assets/files/public?filePath=k1" {
		t.Fatalf("relative public url = %q", url)
	}
	// s3 + 公共桶（CDN 前缀）：public 可见性文件走 CDN 链接
	svc = &Service{driver: "s3", publicBucket: "pub", privateBucket: "pri", publicBaseURL: "https://cdn.example.com"}
	publicItem := repoAssetFile("file.txt")
	publicItem.Visibility = VisibilityPublic
	if url := svc.PublicURL(context.Background(), publicItem); url != "https://cdn.example.com/file.txt" {
		t.Fatalf("cdn url = %q", url)
	}
	// 含分隔符的 key：逐段 escape（/ 保留，段内特殊字符转义）
	publicItem = repoAssetFile("a/b.txt")
	publicItem.Visibility = VisibilityPublic
	if url := svc.PublicURL(context.Background(), publicItem); url != "https://cdn.example.com/a/b.txt" {
		t.Fatalf("cdn url with slash = %q", url)
	}
	// s3 路径风格
	svc = &Service{driver: "s3", publicBucket: "pub", s3Endpoint: "http://minio:9000", s3PathStyle: true}
	pathItem := repoAssetFile("a.txt")
	pathItem.Visibility = VisibilityPublic
	if url := svc.PublicURL(context.Background(), pathItem); url != "http://minio:9000/pub/a.txt" {
		t.Fatalf("path-style url = %q", url)
	}
	// s3 虚拟主机风格
	svc = &Service{driver: "s3", publicBucket: "pub", s3Endpoint: "https://minio.example.com", s3UseSSL: true}
	hostItem := repoAssetFile("a.txt")
	hostItem.Visibility = VisibilityPublic
	if url := svc.PublicURL(context.Background(), hostItem); url != "https://pub.minio.example.com/a.txt" {
		t.Fatalf("virtual-host url = %q", url)
	}
	// s3 但文件在私有桶 → 走后端代理路径
	svc = &Service{driver: "s3", publicBucket: "pub", privateBucket: "pri", publicBaseURL: ""}
	if url := svc.PublicURL(context.Background(), repoAssetFile("a.txt")); !strings.HasPrefix(url, "/openapi/v1/assets/files/public") {
		t.Fatalf("private bucket url = %q", url)
	}
}

func TestParseOwnerID(t *testing.T) {
	if ParseOwnerID("42") != 42 {
		t.Fatal("numeric parse")
	}
	if ParseOwnerID("abc") != 0 {
		t.Fatal("non-numeric parse should be 0")
	}
	if ParseOwnerID("") != 0 {
		t.Fatal("empty parse should be 0")
	}
}

func TestBucketForVisibilityAndFile(t *testing.T) {
	svc := &Service{privateBucket: "pri", publicBucket: "pub"}
	if svc.bucketForVisibility(VisibilityPublic) != "pub" {
		t.Fatal("public visibility bucket")
	}
	if svc.bucketForVisibility(VisibilityPrivate) != "pri" {
		t.Fatal("private visibility bucket")
	}
	// 无公共桶时回退私有桶
	svc = &Service{privateBucket: "pri"}
	if svc.bucketForVisibility(VisibilityPublic) != "pri" {
		t.Fatal("public without bucket should fall back")
	}
	// 文件记录 bucket 优先
	svc = &Service{privateBucket: "pri", publicBucket: "pub"}
	if svc.bucketForFile(repoAssetFile("")) != "pri" {
		t.Fatal("file with empty bucket should fall back to visibility")
	}
	item := repoAssetFile("")
	item.Bucket = "legacy"
	if svc.bucketForFile(item) != "legacy" {
		t.Fatal("file bucket should win")
	}
}

// repoAssetFile 构造最小 AssetFile（openapi 纯函数测试用）。
func repoAssetFile(key string) repo.AssetFile {
	return repo.AssetFile{StorageKey: key, Visibility: VisibilityPrivate}
}

func TestFileURLEndpointPriority(t *testing.T) {
	signed := "https://rustfs:9000/pri/a.txt?X-Amz-Signature=abc"
	ctx := httpx.WithAuthority(context.Background(), "https", "192.168.7.9:8080")

	// 1. FILESTORE_PUBLIC_BASE_URL 显式配置最高（覆盖请求派生 endpoint）
	svc := &Service{publicBaseURL: "https://cdn.example.com", s3PublicEndpoint: "https://public.example.com"}
	if got := svc.rewriteToURLEndpoint(ctx, signed); got != "https://cdn.example.com/pri/a.txt?X-Amz-Signature=abc" {
		t.Fatalf("public base URL should win: %q", got)
	}

	// 2. ctx 请求派生 endpoint 次之（覆盖 s3PublicEndpoint）
	svc = &Service{s3PublicEndpoint: "https://public.example.com"}
	if got := svc.rewriteToURLEndpoint(ctx, signed); got != "https://192.168.7.9:8080/pri/a.txt?X-Amz-Signature=abc" {
		t.Fatalf("request-derived endpoint should win over s3 public endpoint: %q", got)
	}

	// 3. 无显式/请求派生 endpoint：保留 store 侧 FILESTORE_S3_PUBLIC_ENDPOINT 行为（原样返回）
	svc = &Service{s3PublicEndpoint: "https://public.example.com"}
	if got := svc.rewriteToURLEndpoint(context.Background(), signed); got != signed {
		t.Fatalf("s3 public endpoint handled by store, url should stay: %q", got)
	}

	// 4. 全部缺失：内部 endpoint 签名地址原样返回
	svc = &Service{}
	if got := svc.rewriteToURLEndpoint(context.Background(), signed); got != signed {
		t.Fatalf("internal endpoint url should stay: %q", got)
	}

	// 空 URL 原样返回
	if got := svc.rewriteToURLEndpoint(ctx, ""); got != "" {
		t.Fatalf("empty url should stay empty, got %q", got)
	}
}

func TestPublicBucketURLRequestDerivedEndpoint(t *testing.T) {
	svc := &Service{driver: "s3", publicBucket: "pub", s3Endpoint: "http://minio:9000", s3PathStyle: true}
	item := repoAssetFile("a.txt")
	item.Visibility = VisibilityPublic

	// 请求派生 endpoint 位于 publicBaseURL 与 s3PublicEndpoint 之间
	ctx := httpx.WithAuthority(context.Background(), "https", "192.168.7.9:8080")
	if got := svc.PublicURL(ctx, item); got != "https://192.168.7.9:8080/pub/a.txt" {
		t.Fatalf("request-derived public bucket url = %q", got)
	}

	// publicBaseURL 仍然最高
	svc.publicBaseURL = "https://cdn.example.com"
	if got := svc.PublicURL(ctx, item); got != "https://cdn.example.com/a.txt" {
		t.Fatalf("cdn prefix should win: %q", got)
	}

	// 无 ctx authority：回落既有 s3PublicEndpoint / 内部 endpoint 行为
	svc.publicBaseURL = ""
	if got := svc.PublicURL(context.Background(), item); got != "http://minio:9000/pub/a.txt" {
		t.Fatalf("no ctx should keep legacy behavior, got %q", got)
	}
	svc.s3PublicEndpoint = "https://public.example.com"
	if got := svc.PublicURL(context.Background(), item); got != "https://public.example.com/pub/a.txt" {
		t.Fatalf("s3 public endpoint fallback = %q", got)
	}
}
