package filestore

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 最小 S3 服务端模拟：HEAD/PUT bucket、PUT ?policy，
// 可选 denyAll 模式对全部请求返回 AccessDenied，或 failAll 模式返回 500。
type fakeS3 struct {
	mu       sync.Mutex
	buckets  map[string]bool
	policies map[string]string
	denyAll  bool
	failAll  bool
}

func newFakeS3() *fakeS3 {
	return &fakeS3{buckets: map[string]bool{}, policies: map[string]string{}}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		writeS3Error(w, http.StatusInternalServerError, "InternalError")
		return
	}
	if f.denyAll {
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
		return
	}
	// path-style: /{bucket}/ 或 /{bucket}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	bucket := parts[0]
	isPolicy := r.URL.Query().Has("policy")
	switch {
	case r.Method == http.MethodHead && !isPolicy:
		if f.buckets[bucket] {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	case r.Method == http.MethodPut && isPolicy:
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		f.policies[bucket] = string(body)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		if f.buckets[bucket] {
			writeS3Error(w, http.StatusConflict, "BucketAlreadyOwnedByYou")
			return
		}
		f.buckets[bucket] = true
		w.WriteHeader(http.StatusOK)
	default:
		writeS3Error(w, http.StatusNotImplemented, "NotImplemented")
	}
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	type s3Error struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
	}
	out, _ := xml.Marshal(s3Error{Code: code})
	_, _ = w.Write(out)
}

func newEnsureTestStore(t *testing.T, srv *httptest.Server, publicBucket string) *S3Store {
	t.Helper()
	store, err := NewS3(S3Config{
		Endpoint:        strings.TrimPrefix(srv.URL, "http://"),
		Region:          "us-east-1",
		Bucket:          "private",
		PublicBucket:    publicBucket,
		AccessKeyID:     "root",
		SecretAccessKey: "password",
		PathStyle:       true,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return store
}

func TestEnsureBucketsCreatesMissingAndSetsPolicy(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	store := newEnsureTestStore(t, srv, "public")

	if err := store.EnsureBuckets(context.Background()); err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	if !fake.buckets["private"] || !fake.buckets["public"] {
		t.Fatalf("buckets not created: %v", fake.buckets)
	}
	policy := fake.policies["public"]
	if !strings.Contains(policy, "s3:GetObject") || !strings.Contains(policy, "arn:aws:s3:::public/*") {
		t.Fatalf("public bucket policy mismatch: %s", policy)
	}
	if _, ok := fake.policies["private"]; ok {
		t.Fatalf("private bucket must not get an anonymous policy")
	}
}

func TestEnsureBucketsIdempotentWhenExist(t *testing.T) {
	fake := newFakeS3()
	fake.buckets["private"] = true
	fake.buckets["public"] = true
	srv := httptest.NewServer(fake)
	defer srv.Close()
	store := newEnsureTestStore(t, srv, "public")

	if err := store.EnsureBuckets(context.Background()); err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	if !strings.Contains(fake.policies["public"], "s3:GetObject") {
		t.Fatalf("public policy must be (re)applied: %q", fake.policies["public"])
	}
}

func TestEnsureBucketsSkipsPublicWhenUnset(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	store := newEnsureTestStore(t, srv, "")
	if err := store.EnsureBuckets(context.Background()); err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	srv.Close()
	if len(fake.policies) != 0 {
		t.Fatalf("no policy expected, got %v", fake.policies)
	}
}

func TestEnsureBucketsSameNameBucketStillGetsPolicy(t *testing.T) {
	// 公有桶与私有桶同名：唯一的一次确保必须带匿名下载策略，
	// 否则 PublicURL 产出的未签名直链会 403。
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	store := newEnsureTestStore(t, srv, "private")
	if err := store.EnsureBuckets(context.Background()); err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	if !strings.Contains(fake.policies["private"], "s3:GetObject") {
		t.Fatalf("shared bucket must get the anonymous download policy: %v", fake.policies)
	}
}

func TestEnsureBucketsAccessDeniedDegrades(t *testing.T) {
	fake := newFakeS3()
	fake.denyAll = true
	srv := httptest.NewServer(fake)
	defer srv.Close()
	store := newEnsureTestStore(t, srv, "public")

	// 外置 S3 凭证无权限：告警放行，不阻断启动
	if err := store.EnsureBuckets(context.Background()); err != nil {
		t.Fatalf("AccessDenied must degrade to warn, got: %v", err)
	}
}

func TestEnsureBucketsTimeoutFails(t *testing.T) {
	fake := newFakeS3()
	fake.failAll = true
	srv := httptest.NewServer(fake)
	defer srv.Close()
	store := newEnsureTestStore(t, srv, "public")

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := store.EnsureBuckets(ctx)
	if err == nil {
		t.Fatal("persistent failure must return an error")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Fatalf("error should name the bucket, got: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("EnsureBuckets did not honor ctx deadline, took %v", time.Since(start))
	}
}
