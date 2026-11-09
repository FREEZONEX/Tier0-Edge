package sourcestream

import (
	"mime"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

const (
	MaxSourceHTTPHeaders        = 64
	MaxSourceHTTPHeaderName     = 128
	MaxSourceHTTPHeaderValue    = 4096
	SourceHTTPHeaderNamePattern = "^[!#$%&'*+.^_`|~0-9A-Za-z-]+$"
)

var sourceHTTPHeaderName = regexp.MustCompile(SourceHTTPHeaderNamePattern)

// ValidateSourceHTTPHeaders 在保存与执行入口共用边界，认证材料仍通过凭据入口提交。
func ValidateSourceHTTPHeaders(headers map[string]string) error {
	if len(headers) > MaxSourceHTTPHeaders {
		return invalid("http.headers", "invalid")
	}
	seen := make(map[string]bool, len(headers))
	for name, value := range headers {
		lower := strings.ToLower(name)
		if len(name) > MaxSourceHTTPHeaderName || !sourceHTTPHeaderName.MatchString(name) || seen[lower] {
			return invalid("http.headers", "invalid")
		}
		seen[lower] = true
		switch lower {
		case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key", "apikey", "x-auth-token", "x-access-token", "host", "content-length", "connection", "transfer-encoding", "te", "trailer", "upgrade", "proxy-connection", "keep-alive":
			return invalid("http.headers", "invalid")
		}
		if strings.TrimSpace(value) == "" || len(value) > MaxSourceHTTPHeaderValue || strings.Contains(value, "-----BEGIN") {
			return invalid("http.headers", "invalid")
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return invalid("http.headers", "invalid")
			}
		}
		if (lower == "accept" || lower == "content-type") && !safeSourceMIMEHeader(lower, value) {
			return invalid("http.headers", "invalid")
		}
	}
	return nil
}

func safeSourceMIMEHeader(name, value string) bool {
	parts := []string{value}
	if name == "accept" {
		parts = strings.Split(value, ",")
	}
	for _, part := range parts {
		if _, _, err := mime.ParseMediaType(strings.TrimSpace(part)); err != nil {
			return false
		}
	}
	return true
}

func applySourceHTTPHeaders(req *http.Request, def SourceDefinition) error {
	if def.HTTP == nil {
		return nil
	}
	if err := ValidateSourceHTTPHeaders(def.HTTP.Headers); err != nil {
		return err
	}
	for name, value := range def.HTTP.Headers {
		req.Header.Set(name, value)
	}
	return nil
}
