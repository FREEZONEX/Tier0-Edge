package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrHTTPRequest = errors.New("SourceStream.httpFailed")

// OAuth's separately declared token origin may be private, just like a Source
// origin. The scope is attached only to the token exchange, never to script
// requests, and pins the first vetted DNS set for all dials of that exchange.
type oauthTokenDialScopeKey struct{}
type oauthTokenDialScope struct {
	host, port string
	mu         sync.Mutex
	addresses  map[netip.Addr]struct{}
}

func oauthTokenContext(ctx context.Context, rawURL string) (context.Context, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, ErrOAuth
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	return context.WithValue(ctx, oauthTokenDialScopeKey{}, &oauthTokenDialScope{host: u.Hostname(), port: port}), nil
}

func (scope *oauthTokenDialScope) check(host, port string, ips []netip.Addr) bool {
	if !strings.EqualFold(host, scope.host) || port != scope.port || len(ips) == 0 || len(ips) > 64 {
		return false
	}
	for _, ip := range ips {
		if !httpSourceAddressAllowed(host, ip) {
			return false
		}
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.addresses == nil {
		scope.addresses = make(map[netip.Addr]struct{}, len(ips))
		for _, ip := range ips {
			scope.addresses[ip.Unmap()] = struct{}{}
		}
	}
	for _, ip := range ips {
		if _, ok := scope.addresses[ip.Unmap()]; !ok {
			return false
		}
	}
	return true
}

// Resolve at dial time and dial the checked IP, preventing DNS rebinding.
// No environment proxy or redirects may forward a Source's bearer token.
func sourceHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, ErrHTTPRequest
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil || len(ips) == 0 {
			return nil, ErrHTTPRequest
		}
		if scope, ok := ctx.Value(oauthTokenDialScopeKey{}).(*oauthTokenDialScope); ok {
			if !scope.check(host, port, ips) {
				return nil, ErrHTTPRequest
			}
		} else {
			for _, ip := range ips {
				if !publicSourceIP(ip) {
					return nil, ErrHTTPRequest
				}
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func publicSourceIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

type httpReadOptions struct {
	Path  string            `json:"path"`
	Query map[string]string `json:"query,omitempty"`
}

func sourceReadURL(endpoint string, options httpReadOptions) (string, error) {
	if len(options.Path) > 4096 || len(options.Query) > 100 {
		return "", invalid("read", "invalidHTTPOptions")
	}
	base, e := url.Parse(strings.TrimRight(endpoint, "/") + "/")
	if e != nil {
		return "", ErrHTTPRequest
	}
	// Relative paths preserve the configured API prefix and origin.
	p, e := url.Parse(options.Path)
	if e != nil || p.IsAbs() || p.Host != "" || p.User != nil || p.RawQuery != "" || p.Fragment != "" || strings.HasPrefix(p.Path, "/") || strings.Contains(p.Path, "\\") {
		return "", invalid("read", "invalidHTTPOptions")
	}
	for _, part := range strings.Split(p.Path, "/") {
		if part == ".." || part == "." {
			return "", invalid("read", "invalidHTTPOptions")
		}
	}
	dest := base.ResolveReference(p)
	if dest.Scheme != base.Scheme || dest.Host != base.Host || !strings.HasPrefix(dest.Path, base.Path) {
		return "", ErrForbidden
	}
	q := dest.Query()
	for k, v := range options.Query {
		if len(k) > 256 || len(v) > 8192 {
			return "", invalid("read", "invalidHTTPOptions")
		}
		q.Set(k, v)
	}
	dest.RawQuery = q.Encode()
	return dest.String(), nil
}
func (s *Service) readHTTPSource(ctx context.Context, id int64, d SourceDefinition, raw json.RawMessage) (any, error) {
	options, _, err := parseDocument[httpReadOptions](raw)
	if err != nil {
		return nil, err
	}
	destination, err := sourceReadURL(d.Endpoint, options)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, destination, nil)
	if err != nil {
		return nil, ErrHTTPRequest
	}
	var accessToken string
	if d.Protocol == OAuth {
		accessToken, err = s.oauthAccessToken(ctx, id, d)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	} else if d.HTTP.Authentication != "none" {
		_, credential, _, err := s.connection(ctx, id, nil)
		if err != nil {
			return nil, err
		}
		if credential.Kind != d.HTTP.Authentication {
			return nil, ErrCredential
		}
		switch credential.Kind {
		case "basic":
			req.SetBasicAuth(credential.Username, credential.Secret)
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+credential.Secret)
		default:
			return nil, ErrCredential
		}
	}
	req.Header.Set("Accept", "application/json")
	if err := applySourceHTTPHeaders(req, d); err != nil {
		return nil, err
	}
	client, cleanup, err := s.httpClientFor(d)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var response *http.Response
	for attempt := 0; ; attempt++ {
		response, err = client.Do(req)
		if err != nil {
			return nil, safeHTTPFailure(ctx, err)
		}
		if response.StatusCode != http.StatusUnauthorized || d.Protocol != OAuth {
			break
		}
		response.Body.Close()
		if err = s.rejectOAuthToken(ctx, id, d, accessToken); err != nil {
			return nil, err
		}
		if attempt == 1 {
			return nil, ErrOAuthRequired
		}
		accessToken, err = s.oauthAccessToken(ctx, id, d)
		if err != nil {
			return nil, err
		}
		// Only this idempotent GET is replayed, using the same URL and deadline.
		req = req.Clone(ctx)
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	defer response.Body.Close()
	if response.StatusCode == 401 {
		if d.Protocol == OAuth {
			return nil, ErrOAuthRequired
		}
		return nil, ErrCredential
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrHTTPRequest
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTTPResponseBytes+1))
	if err != nil {
		return nil, safeHTTPFailure(ctx, err)
	}
	if len(body) > maxHTTPResponseBytes {
		return nil, ErrHTTPRequest
	}
	if response.StatusCode == 204 {
		return nil, nil
	}
	var result any
	if json.Unmarshal(body, &result) != nil {
		return nil, ErrHTTPRequest
	}
	return result, nil
}
