package unsv2

import (
	"backend/internal/domain/sourcestream"
	"encoding/json"
	"net/url"
	"strings"
)

// Acquisition is configuration, never an arbitrary invocation payload.
type Acquisition struct {
	Mode               string                             `json:"mode"`
	PollIntervalMs     int                                `json:"pollIntervalMs,omitempty"`
	TimeoutMs          int                                `json:"timeoutMs"`
	Retry              AcquisitionRetry                   `json:"retry"`
	HTTP               *HTTPAcquisition                   `json:"http,omitempty"`
	NativeResourceRefs []string                           `json:"nativeResourceRefs,omitempty"`
	Collection         *sourcestream.CollectionDefinition `json:"collection,omitempty"`
}
type AcquisitionRetry struct {
	MaxAttempts int `json:"maxAttempts"`
	BackoffMs   int `json:"backoffMs"`
}
type HTTPAcquisition struct {
	Path       string            `json:"path"`
	Query      map[string]string `json:"query,omitempty"`
	ItemsPath  string            `json:"itemsPath"`
	Pagination HTTPPagination    `json:"pagination"`
}
type HTTPPagination struct {
	Mode            string `json:"mode"`
	PageParameter   string `json:"pageParameter,omitempty"`
	SizeParameter   string `json:"sizeParameter,omitempty"`
	PageSize        int    `json:"pageSize,omitempty"`
	StartPage       int    `json:"startPage,omitempty"`
	CursorParameter string `json:"cursorParameter,omitempty"`
	InitialCursor   string `json:"initialCursor,omitempty"`
	NextCursorPath  string `json:"nextCursorPath,omitempty"`
	MaxPages        int    `json:"maxPages"`
}

func acquisition(data map[string]any, protocol string, enabled bool) (Acquisition, error) {
	var a Acquisition
	if len(data) == 0 && !enabled {
		return a, nil
	} // retained inactive drafts
	raw, _ := json.Marshal(data)
	if err := Decode(raw, &a); err != nil {
		return a, err
	}
	if a.TimeoutMs < 100 || a.TimeoutMs > 30000 || a.Retry.MaxAttempts < 1 || a.Retry.MaxAttempts > 5 || a.Retry.BackoffMs < 100 || a.Retry.BackoffMs > 30000 {
		return a, invalid("acquisition", "timeoutMs 100..30000; retry.maxAttempts 1..5 and backoffMs 100..30000 required")
	}
	if protocol == "http-api" || protocol == "oauth2" {
		if a.Mode != "poll" || a.PollIntervalMs < 1000 || a.PollIntervalMs > 86400000 || a.HTTP == nil || a.Collection != nil || len(a.NativeResourceRefs) > 0 {
			return a, invalid("acquisition", "HTTP requires mode=poll, pollIntervalMs 1000..86400000 and http; no native configuration")
		}
		h := a.HTTP
		p := h.Pagination
		u, err := url.Parse(h.Path)
		if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(h.Path, "\\") || strings.HasPrefix(h.Path, "/") || len(h.Path) > 4096 {
			return a, invalid("acquisition.http.path", "relative API path required")
		}
		for _, part := range strings.Split(u.Path, "/") {
			if part == ".." || part == "." {
				return a, invalid("acquisition.http.path", "path traversal forbidden")
			}
		}
		if len(h.Query) > 100 || len(h.ItemsPath) > 512 || p.MaxPages < 1 || p.MaxPages > 100 {
			return a, invalid("acquisition.http", "maxPages 1..100 required")
		}
		validParam := func(s string) bool { return s != "" && len(s) <= 128 && !strings.ContainsAny(s, "\r\n\x00") }
		switch p.Mode {
		case "none":
			if p.MaxPages != 1 {
				return a, invalid("acquisition.http.pagination", "none requires maxPages=1")
			}
		case "page":
			if !validParam(p.PageParameter) || !validParam(p.SizeParameter) || p.PageParameter == p.SizeParameter || p.PageSize < 1 || p.PageSize > 1000 || p.StartPage < 1 {
				return a, invalid("acquisition.http.pagination", "pageParameter, sizeParameter, pageSize 1..1000 and startPage>=1 required")
			}
		case "cursor":
			if !validParam(p.CursorParameter) || p.NextCursorPath == "" || len(p.NextCursorPath) > 512 || len(p.InitialCursor) > 8192 {
				return a, invalid("acquisition.http.pagination", "cursorParameter and nextCursorPath required")
			}
		default:
			return a, invalid("acquisition.http.pagination.mode", "none, page or cursor required")
		}
	} else {
		if a.Mode != "watch" || a.HTTP != nil || a.PollIntervalMs != 0 || a.Collection == nil || len(a.NativeResourceRefs) == 0 || len(a.NativeResourceRefs) > 128 {
			return a, invalid("acquisition", "native protocols require mode=watch, nativeResourceRefs and collection")
		}
	}
	return a, nil
}
