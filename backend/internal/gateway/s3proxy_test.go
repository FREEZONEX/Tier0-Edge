package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestS3Proxy(t *testing.T, upstream *httptest.Server) *S3Proxy {
	t.Helper()
	p := NewS3Proxy("s3", upstream.URL, "tier0-files-private", "tier0-files-public")
	if p == nil {
		t.Fatal("NewS3Proxy returned nil")
	}
	return p
}

func TestTryProxyOptionsPreflight(t *testing.T) {
	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newTestS3Proxy(t, upstream)
	req := httptest.NewRequest(http.MethodOptions, "/tier0-files-private/2/assets/x.zip", nil)
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()

	if !p.TryProxy(rec, req) {
		t.Fatal("TryProxy should handle OPTIONS for bucket prefix")
	}
	if upstreamHit {
		t.Fatal("preflight must not be forwarded to the object store")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	h := rec.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != s3CorsAllowMethods {
		t.Fatalf("Allow-Methods = %q", got)
	}
	if got := h.Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Fatalf("Allow-Headers should echo requested headers, got %q", got)
	}
	if got := h.Get("Access-Control-Expose-Headers"); got != s3CorsExposeHeaders {
		t.Fatalf("Expose-Headers = %q", got)
	}
}

func TestTryProxyProxiedResponseCarriesCorsAndEtag(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newTestS3Proxy(t, upstream)
	req := httptest.NewRequest(http.MethodPut, "/tier0-files-private/2/assets/x.zip", nil)
	rec := httptest.NewRecorder()

	if !p.TryProxy(rec, req) {
		t.Fatal("TryProxy should proxy PUT for bucket prefix")
	}
	h := rec.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
	if got := h.Get("Access-Control-Expose-Headers"); got != s3CorsExposeHeaders {
		t.Fatalf("Expose-Headers = %q, want %q", got, s3CorsExposeHeaders)
	}
	if got := h.Get("ETag"); got != `"abc123"` {
		t.Fatalf("upstream ETag should be preserved, got %q", got)
	}
}

func TestTryProxyNonMatchingPathAndMethod(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	p := newTestS3Proxy(t, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/core/assets", nil)
	if p.TryProxy(rec, req) {
		t.Fatal("non-bucket path must not be handled")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/tier0-files-private/2/assets/x.zip", nil)
	if p.TryProxy(rec, req) {
		t.Fatal("non-S3 method must not be handled")
	}
}

func TestNewS3ProxyNilCases(t *testing.T) {
	if NewS3Proxy("local", "http://rustfs:9000", "b") != nil {
		t.Fatal("non-s3 driver must return nil")
	}
	if NewS3Proxy("s3", "", "b") != nil {
		t.Fatal("empty endpoint must return nil")
	}
	if NewS3Proxy("s3", "http://rustfs:9000") != nil {
		t.Fatal("no buckets must return nil")
	}
}

func TestMatchesPrefix(t *testing.T) {
	u, _ := url.Parse("http://rustfs:9000")
	p := &S3Proxy{target: u, prefixes: []string{"/private/", "/public/"}}
	if !p.matchesPrefix("/private/a/b") || !p.matchesPrefix("/public/x") {
		t.Fatal("matching prefixes not detected")
	}
	if p.matchesPrefix("/private") || p.matchesPrefix("/other/x") {
		t.Fatal("non-matching prefixes detected")
	}
}
