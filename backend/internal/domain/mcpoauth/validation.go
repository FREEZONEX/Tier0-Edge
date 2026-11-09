// Package mcpoauth 实现仅用于 MCP 的公共客户端浏览器授权，不复用 App OAuth token。
package mcpoauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/httpx"
	"backend/internal/mcpaccess"
)

const accessTTL = 15 * time.Minute
const grantTTL = 30 * 24 * time.Hour
const requestTTL = 10 * time.Minute
const codeTTL = 5 * time.Minute

var supportedScopes = mcpaccess.Scopes()
var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var idPattern = regexp.MustCompile(`^mc(or|og|pc)_[0-9a-f]{64}$`)

type Failure struct {
	Code   string
	Status int
}

func (f *Failure) Error() string { return f.Code }
func invalid(code string) error  { return &Failure{Code: code, Status: 400} }
func unavailable() error         { return &Failure{Code: "temporarily_unavailable", Status: 503} }
func now() int64                 { return time.Now().UTC().UnixMilli() }
func secret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
func hash(v string) string   { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func has(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
func scopeList(text string) ([]string, error) {
	if len(text) > 256 {
		return nil, invalid("invalid_scope")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range strings.Fields(text) {
		if !has(supportedScopes, v) {
			return nil, invalid("invalid_scope")
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}
func subset(values, base []string) bool {
	for _, v := range values {
		if !has(base, v) {
			return false
		}
	}
	return true
}
func intersect(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if has(b, v) {
			out = append(out, v)
		}
	}
	return out
}
func subjectScopes(subject contextx.Subject, requested []string) []string {
	return intersect(requested, mcpaccess.Available(subject))
}

func canonical(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(raw, "\\\r\n\"'`$") {
		return "", invalid("invalid_target")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}
func endpoints(ctx context.Context, c config.MCPConf) (resource, issuer string, err error) {
	if c.Disabled {
		return "", "", unavailable()
	}
	resource = c.PublicURL
	if resource == "" {
		scheme, host, ok := httpx.AuthorityFrom(ctx)
		if !ok {
			return "", "", unavailable()
		}
		resource = scheme + "://" + host + httpx.ProxyPrefixFrom(ctx) + "/mcp"
	}
	resource, err = canonical(resource)
	if err != nil {
		return "", "", err
	}
	u, _ := url.Parse(resource)
	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		return "", "", &Failure{Code: "https_required", Status: 400}
	}
	if !strings.HasSuffix(u.Path, "/mcp") {
		return "", "", invalid("invalid_target")
	}
	issuer = strings.TrimSuffix(resource, "/mcp")
	return resource, issuer, nil
}
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func validRedirect(raw string) bool {
	if len(raw) > 1024 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || !has([]string{"127.0.0.1", "localhost", "::1"}, u.Hostname()) || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, "/") || strings.ContainsAny(raw, "\\\r\n\"'`$") {
		return false
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
func redirectMatches(registered []string, actual string) bool {
	if !validRedirect(actual) {
		return false
	}
	a, _ := url.Parse(actual)
	for _, raw := range registered {
		if !validRedirect(raw) {
			continue
		}
		b, _ := url.Parse(raw)
		// RFC 8252 loopback port 可变，但 host/path 必须精确匹配，禁止 suffix/wildcard。
		if a.Scheme == b.Scheme && a.Hostname() == b.Hostname() && a.Path == b.Path {
			return true
		}
	}
	return false
}
func callback(raw, state, issuer, code string, denied bool) string {
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("state", state)
	q.Set("iss", issuer)
	if denied {
		q.Set("error", "access_denied")
	} else {
		q.Set("code", code)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
