package filestore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestS3Store 构造一个仅用于签发的 S3Store：endpoint 指向不可达地址也没关系，
// 只要 Region 非空，minio-go 签发时跳过 bucket location 网络探测，不发起真实请求。
func newTestS3Store(t *testing.T, publicEndpoint string) *S3Store {
	t.Helper()
	store, err := NewS3(S3Config{
		Endpoint:        "localhost:9000",
		Region:          "us-east-1",
		Bucket:          "bucket-a",
		AccessKeyID:     "root",
		SecretAccessKey: "password",
		PublicEndpoint:  publicEndpoint,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return store
}

func decodePostPolicy(t *testing.T, fields map[string]string) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(fields["policy"])
	if err != nil {
		t.Fatalf("decode policy base64: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal policy: %v", err)
	}
	return doc
}

// hasExactCondition 断言 policy conditions 含 ["eq", "$key", "value"] 形式的精确匹配条件
func hasExactCondition(t *testing.T, doc map[string]any, key, value string) {
	t.Helper()
	conditions, ok := doc["conditions"].([]any)
	if !ok {
		t.Fatalf("policy conditions missing or wrong type")
	}
	for _, c := range conditions {
		if arr, ok := c.([]any); ok && len(arr) == 3 && arr[0] == "eq" && arr[1] == "$"+key && arr[2] == value {
			return
		}
	}
	t.Fatalf("exact condition {%s: %s} not found in %v", key, value, conditions)
}

// hasContentLengthRange 断言 policy conditions 含 ["content-length-range", 1, max]
func hasContentLengthRange(t *testing.T, doc map[string]any, max int64) {
	t.Helper()
	conditions, ok := doc["conditions"].([]any)
	if !ok {
		t.Fatalf("policy conditions missing or wrong type")
	}
	for _, c := range conditions {
		arr, ok := c.([]any)
		if !ok || len(arr) != 3 {
			continue
		}
		if arr[0] == "content-length-range" && arr[1].(float64) == 1 && arr[2].(float64) == float64(max) {
			return
		}
	}
	t.Fatalf("content-length-range [1, %d] condition not found in %v", max, conditions)
}

func TestSignPostURL(t *testing.T) {
	store := newTestS3Store(t, "")
	postURL, fields, ok, err := store.SignPostURL(context.Background(), "", "7/assets/20260804/ab12cd.png", "image/png", 2048, time.Hour)
	if err != nil {
		t.Fatalf("SignPostURL: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	// 空 bucket 回落到默认私有 bucket；localhost 类 endpoint 走 path style
	if postURL != "http://localhost:9000/bucket-a/" {
		t.Fatalf("unexpected postURL: %s", postURL)
	}
	if fields["key"] != "7/assets/20260804/ab12cd.png" {
		t.Fatalf("unexpected key field: %s", fields["key"])
	}
	for _, k := range []string{"policy", "x-amz-algorithm", "x-amz-credential", "x-amz-date", "x-amz-signature"} {
		if fields[k] == "" {
			t.Fatalf("field %s missing", k)
		}
	}
	doc := decodePostPolicy(t, fields)
	hasExactCondition(t, doc, "bucket", "bucket-a")
	hasExactCondition(t, doc, "key", "7/assets/20260804/ab12cd.png")
	hasContentLengthRange(t, doc, 2048)
	// contentType 非空时签入 Content-Type 条件并随表单字段下发，客户端回显后对象保留媒体类型
	if fields["Content-Type"] != "image/png" {
		t.Fatalf("unexpected Content-Type field: %s", fields["Content-Type"])
	}
	hasExactCondition(t, doc, "Content-Type", "image/png")
}

func TestSignPostURLPublicEndpointRewrite(t *testing.T) {
	store := newTestS3Store(t, "https://gateway.example.com")
	postURL, _, ok, err := store.SignPostURL(context.Background(), "bucket-b", "k.png", "", 1, time.Hour)
	if err != nil || !ok {
		t.Fatalf("SignPostURL: ok=%v err=%v", ok, err)
	}
	// POST policy 签名不含 host，改写与既有预签名 GET 行为一致
	if postURL != "https://gateway.example.com/bucket-b/" {
		t.Fatalf("unexpected rewritten postURL: %s", postURL)
	}
}

func TestSignPostURLValidation(t *testing.T) {
	store := newTestS3Store(t, "")
	if _, _, _, err := store.SignPostURL(context.Background(), "", "k.png", "", 0, time.Hour); err == nil {
		t.Fatalf("expect error for sizeBytes=0")
	}
	if _, _, _, err := store.SignPostURL(context.Background(), "", "../escape.png", "", 1, time.Hour); err == nil {
		t.Fatalf("expect error for invalid key")
	}
}

// local 驱动不支持预签名 POST：返回 ok=false 而不是报错，由调用方映射为业务错误。
func TestLocalSignPostURLUnsupported(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	postURL, fields, ok, err := store.SignPostURL(context.Background(), "", "k.png", "", 1, time.Hour)
	if err != nil {
		t.Fatalf("SignPostURL: %v", err)
	}
	if ok || postURL != "" || fields != nil {
		t.Fatalf("expected unsupported result, got ok=%v postURL=%q fields=%v", ok, postURL, fields)
	}
}

// TestSignPutPartURL 断言分片 PUT 地址携带 partNumber/uploadId 签名参数，
// 且空 bucket 回落到默认私有 bucket（path style）。
func TestSignPutPartURL(t *testing.T) {
	store := newTestS3Store(t, "")
	u, ok, err := store.SignPutPartURL(context.Background(), "", "7/assets/20260804/ab12cd.png", "upload-abc", 3, time.Hour)
	if err != nil {
		t.Fatalf("SignPutPartURL: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse part url: %v", err)
	}
	if parsed.Path != "/bucket-a/7/assets/20260804/ab12cd.png" {
		t.Fatalf("unexpected part url path: %s", parsed.Path)
	}
	if parsed.Query().Get("partNumber") != "3" {
		t.Fatalf("unexpected partNumber query: %s", parsed.Query().Get("partNumber"))
	}
	if parsed.Query().Get("uploadId") != "upload-abc" {
		t.Fatalf("unexpected uploadId query: %s", parsed.Query().Get("uploadId"))
	}
}

// TestSignPutPartURLPublicEndpointRewrite 断言分片 PUT 地址与预签名 GET 一致
// 走 public endpoint 改写（SigV4 签名按原始 host 计算，网关反代回写 Host）。
func TestSignPutPartURLPublicEndpointRewrite(t *testing.T) {
	store := newTestS3Store(t, "https://gateway.example.com")
	u, ok, err := store.SignPutPartURL(context.Background(), "bucket-b", "k.png", "up-1", 2, time.Hour)
	if err != nil || !ok {
		t.Fatalf("SignPutPartURL: ok=%v err=%v", ok, err)
	}
	if !strings.HasPrefix(u, "https://gateway.example.com/bucket-b/k.png") {
		t.Fatalf("unexpected rewritten part url: %s", u)
	}
}

// TestSignPutPartURLValidation 覆盖 partNumber=0、空 uploadId、非法 key 的拒绝路径。
func TestSignPutPartURLValidation(t *testing.T) {
	store := newTestS3Store(t, "")
	if _, _, err := store.SignPutPartURL(context.Background(), "", "k.png", "up-1", 0, time.Hour); err == nil {
		t.Fatalf("expect error for partNumber=0")
	}
	if _, _, err := store.SignPutPartURL(context.Background(), "", "k.png", "", 1, time.Hour); err == nil {
		t.Fatalf("expect error for empty uploadId")
	}
	if _, _, err := store.SignPutPartURL(context.Background(), "", "../escape.png", "up-1", 1, time.Hour); err == nil {
		t.Fatalf("expect error for invalid key")
	}
}

// TestLocalMultipartUnsupported 断言 local 驱动对分片上传控制面整体返回不支持：
// 控制面方法返回 ErrMultipartUnsupported，签名方法返回 ok=false。
func TestLocalMultipartUnsupported(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	if _, err := store.CreateMultipartUpload(context.Background(), "", "k.png", "image/png"); !errors.Is(err, ErrMultipartUnsupported) {
		t.Fatalf("expected ErrMultipartUnsupported, got %v", err)
	}
	if err := store.CompleteMultipartUpload(context.Background(), "", "k.png", "up-1", nil); !errors.Is(err, ErrMultipartUnsupported) {
		t.Fatalf("expected ErrMultipartUnsupported, got %v", err)
	}
	if err := store.AbortMultipartUpload(context.Background(), "", "k.png", "up-1"); !errors.Is(err, ErrMultipartUnsupported) {
		t.Fatalf("expected ErrMultipartUnsupported, got %v", err)
	}
	if _, err := store.ListParts(context.Background(), "", "k.png", "up-1"); !errors.Is(err, ErrMultipartUnsupported) {
		t.Fatalf("expected ErrMultipartUnsupported, got %v", err)
	}
	u, ok, err := store.SignPutPartURL(context.Background(), "", "k.png", "up-1", 1, time.Hour)
	if err != nil {
		t.Fatalf("SignPutPartURL: %v", err)
	}
	if ok || u != "" {
		t.Fatalf("expected unsupported result, got ok=%v url=%q", ok, u)
	}
}
