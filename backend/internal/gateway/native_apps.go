package gateway

import (
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/internal/appgateway"

	"github.com/google/uuid"
)

type NativeAppResolver interface{ Resolve(string) (int, error) }

var nativeAppTransport = &http.Transport{Proxy: nil, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 64, MaxIdleConnsPerHost: 4}

func (g *HTTPGateway) BindNativeApps(apps NativeAppResolver) { g.nativeApps = apps }

func (g *HTTPGateway) handleNativeApp(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/apps/") {
		return false
	}
	w.Header().Set("Referrer-Policy", "same-origin")
	if g.nativeApps == nil {
		http.Error(w, "Native App runtime is unavailable", http.StatusServiceUnavailable)
		return true
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/apps/"), "/", 2)
	id := parts[0]
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return true
	}
	authRequest := r.Clone(r.Context())
	authURL := *r.URL
	authURL.RawQuery = ""
	authRequest.URL = &authURL
	subject, err := g.auth.AuthenticateRequest(authRequest)
	if err != nil || subject.AuthType != "session" {
		http.Error(w, "Sign in to Tier0 to open this App", http.StatusUnauthorized)
		return true
	}
	allowed, _, err := g.permission.Allow(r.Context(), subject, "gateway", r.Method, r.URL.Path)
	if err != nil || !allowed {
		http.Error(w, "App access is forbidden", http.StatusForbidden)
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || !strings.EqualFold(u.Host, r.Host) {
			http.Error(w, "Cross-origin App access is forbidden", http.StatusForbidden)
			return true
		}
	}
	roles, err := unsv2.ResolveNativeAppRoles(r.Context(), id, subject.UserID)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, unsv2.ErrNativeAppAccess):
			http.Error(w, "App access is forbidden", http.StatusForbidden)
		default:
			http.Error(w, "App access check unavailable", http.StatusServiceUnavailable)
		}
		return true
	}
	roleKeys := make([]string, len(roles))
	for i, r := range roles {
		roleKeys[i] = r.ID
	}
	port, err := g.nativeApps.Resolve(id)
	if err != nil {
		http.Error(w, "App is not running", http.StatusServiceUnavailable)
		return true
	}
	base := "/apps/" + id
	if len(parts) == 1 {
		http.Redirect(w, r, base+"/", http.StatusTemporaryRedirect)
		return true
	}
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
	proxy := &httputil.ReverseProxy{Transport: nativeAppTransport, FlushInterval: -1}
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		req := pr.Out
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		req.Header.Del("Authorization")
		req.Header.Del("X-API-Key")
		appgateway.StripInjectedHeaders(req)
		appgateway.SetRuntimeRoleHeaders(req, roleKeys)
		identity, _ := json.Marshal(map[string]string{"userID": strconv.FormatInt(subject.UserID, 10), "userName": subject.UserName, "role": req.Header.Get("X-Tier0-Active-Role")})
		req.Header.Set("user", string(identity))
		req.Header.Del("Cookie")
		for _, cookie := range r.Cookies() {
			if cookie.Name != "edge_session" && cookie.Name != "app_auth_token" {
				req.AddCookie(cookie)
			}
		}
		req.Header.Set("X-Forwarded-Host", r.Host)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		req.Header.Set("X-Forwarded-Proto", scheme)
		req.Header.Set("X-Forwarded-Prefix", base)
		req.Header.Set("X-Forwarded-For", "")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		cookies := resp.Cookies()
		resp.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if cookie.Name == "edge_session" || cookie.Name == "app_auth_token" {
				continue
			}
			cookie.Domain = ""
			if !strings.HasPrefix(cookie.Path, base+"/") {
				cookie.Path = base + "/"
			}
			resp.Header.Add("Set-Cookie", cookie.String())
		}
		if location := resp.Header.Get("Location"); location != "" {
			u, e := url.Parse(location)
			if e == nil {
				if u.Host == target.Host {
					u.Scheme = ""
					u.Host = ""
				}
				if u.Host == "" && strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(u.Path, base+"/") && u.Path != base {
					u.Path = base + u.Path
				}
				resp.Header.Set("Location", u.String())
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "App connection unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
	return true
}
