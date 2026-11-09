package sourcestream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

const maxHTTPBytes = streamexec.MaxJSONBytes
const maxHTTPResponseBytes = streamexec.MaxProgramDataBytes

// Retain timeout/cancellation identity without exposing transport errors, URLs
// or credentials to scripts. Ordinary failures keep the existing safe code.
func safeHTTPFailure(ctx context.Context, err error) error {
	if ctx.Value(edgeResourceReadContext{}) == true && errors.Is(err, syscall.ECONNREFUSED) {
		return ErrEdgeConnectionRefused
	}
	return safeHTTPFailureWithFallback(ctx, err, ErrHTTPRequest)
}

func safeHTTPFailureWithFallback(ctx context.Context, err, fallback error) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	var timeout net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, streamexec.ErrTimeout) ||
		errors.As(err, &timeout) && timeout.Timeout() {
		return streamexec.ErrTimeout
	}
	return fallback
}

type httpClientEntry struct {
	sourceID int64
	key      string
	client   *http.Client
	used     time.Time
}

// HTTPClientManager owns a bounded set of Source-scoped transports, never
// credentials. Changed configuration/credential references retire idle sockets.
type HTTPClientManager struct {
	mu      sync.Mutex
	entries map[int64]httpClientEntry
	// A test can inject a dialer for its own real HTTP fixture. Production nil
	// uses the DNS/IP guard below; this is not exposed through configuration.
	dial    func(context.Context, string, string) (net.Conn, error)
	resolve func(context.Context, string) ([]netip.Addr, error)
}

func NewHTTPClientManager() *HTTPClientManager {
	return &HTTPClientManager{entries: make(map[int64]httpClientEntry)}
}
func (m *HTTPClientManager) CloseSource(id int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.entries[id]; ok {
		entry.client.CloseIdleConnections()
		delete(m.entries, id)
	}
}
func (m *HTTPClientManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, entry := range m.entries {
		entry.client.CloseIdleConnections()
		delete(m.entries, id)
	}
}
func (m *HTTPClientManager) client(source repo.Source, def SourceDefinition) (*http.Client, error) {
	if m == nil || def.HTTP == nil || source.ID <= 0 || source.ConfigRevision < 1 || def.ConnectionRole() != ClientRole || !validHTTPBase(def.Endpoint) {
		return nil, ErrHTTPRequest
	}
	config, err := json.Marshal(def.HTTP)
	if err != nil {
		return nil, ErrHTTPRequest
	}
	h := sha256.Sum256(append([]byte(def.Endpoint+"\x00"+def.SourceType+"\x00"+strconv.FormatInt(source.ConfigRevision, 10)+"\x00"+def.CredentialRef), config...))
	key := hex.EncodeToString(h[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.entries[source.ID]; ok {
		if entry.key == key {
			entry.used = time.Now()
			m.entries[source.ID] = entry
			return entry.client, nil
		}
		entry.client.CloseIdleConnections()
		delete(m.entries, source.ID)
	}
	if len(m.entries) >= 128 {
		var oldest httpClientEntry
		for _, entry := range m.entries {
			if oldest.client == nil || entry.used.Before(oldest.used) {
				oldest = entry
			}
		}
		oldest.client.CloseIdleConnections()
		delete(m.entries, oldest.sourceID)
	}
	base, _ := url.Parse(def.Endpoint)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxConnsPerHost = 8
	transport.MaxIdleConnsPerHost = 2
	transport.MaxIdleConns = 2
	transport.IdleConnTimeout = time.Minute
	transport.ResponseHeaderTimeout = streamexec.MaxProgramSourceTimeout
	transport.MaxResponseHeaderBytes = 32 * 1024
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if def.tlsConfig != nil {
		transport.TLSClientConfig = def.tlsConfig.Clone()
	}
	transport.TLSClientConfig.ServerName = ""
	if def.HTTP.CACertificate != "" {
		certificates, err := publicCertificates(def.HTTP.CACertificate, true)
		if err != nil {
			return nil, ErrCredential
		}
		roots := transport.TLSClientConfig.RootCAs
		if roots == nil {
			roots, err = x509.SystemCertPool()
			if err != nil {
				return nil, ErrHTTPRequest
			}
		} else {
			roots = roots.Clone()
		}
		for _, certificate := range certificates {
			roots.AddCert(certificate)
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	var originMu sync.Mutex
	var originAddresses map[netip.Addr]struct{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, base.Hostname()) {
			return nil, ErrHTTPRequest
		}
		expectedPort := base.Port()
		if expectedPort == "" {
			if base.Scheme == "https" {
				expectedPort = "443"
			} else {
				expectedPort = "80"
			}
		}
		if port != expectedPort {
			return nil, ErrHTTPRequest
		}
		resolve := m.resolve
		if resolve == nil {
			resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
				return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			}
		}
		ips, err := resolve(ctx, host)
		if err != nil || len(ips) == 0 || len(ips) > 64 {
			return nil, ErrHTTPRequest
		}
		for _, ip := range ips {
			if !httpSourceAddressAllowed(host, ip) {
				return nil, ErrHTTPRequest
			}
		}
		// Pin the first vetted address set for this Source configuration. A new
		// DNS destination must be explicitly retested/refreshed, not silently
		// trusted by a transport that already accepted a different origin IP.
		originMu.Lock()
		if originAddresses == nil {
			originAddresses = make(map[netip.Addr]struct{}, len(ips))
			for _, ip := range ips {
				originAddresses[ip.Unmap()] = struct{}{}
			}
		}
		for _, ip := range ips {
			if _, allowed := originAddresses[ip.Unmap()]; !allowed {
				originMu.Unlock()
				return nil, ErrHTTPRequest
			}
		}
		originMu.Unlock()
		address = net.JoinHostPort(ips[0].Unmap().String(), port)
		if m.dial != nil {
			return m.dial(ctx, network, address)
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
	}
	client := &http.Client{Transport: transport, Timeout: streamexec.MaxProgramSourceTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if def.SourceType != "" {
		client.Jar = newVendorCookieJar()
	}
	m.entries[source.ID] = httpClientEntry{sourceID: source.ID, key: key, client: client, used: time.Now()}
	return client, nil
}

func httpSourceAddressAllowed(host string, ip netip.Addr) bool {
	ip = ip.Unmap()
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip == netip.MustParseAddr("100.100.100.200") || ip == netip.MustParseAddr("fd00:ec2::254") {
		return false
	}
	// A private destination must be explicitly named in the Source origin; DNS
	// is resolved/checked at each new dial and the checked IP is dialed directly.
	return ip.IsPrivate() || publicSourceIP(ip)
}

type HTTPRequestOptions struct {
	TimeoutMs   int                 `json:"timeoutMs,omitempty"`
	Method      string              `json:"method,omitempty"`
	Path        string              `json:"path,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	Query       map[string]string   `json:"query,omitempty"`
	QueryValues map[string][]string `json:"queryValues,omitempty"`
	JSON        json.RawMessage     `json:"json,omitempty"`
	Body        *string             `json:"body,omitempty"`
	Form        map[string]string   `json:"form,omitempty"`
}
type HTTPResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Data    any                 `json:"data"`
}

func parseHTTPRequest(raw json.RawMessage) (HTTPRequestOptions, error) {
	options, _, err := parseDocument[HTTPRequestOptions](raw)
	if err != nil {
		return options, streamexec.ErrSourceDenied
	}
	if options.Method == "" {
		options.Method = http.MethodGet
	}
	if options.Method != strings.ToUpper(options.Method) {
		return options, streamexec.ErrSourceDenied
	}
	switch options.Method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		return options, streamexec.ErrSourceDenied
	}
	if options.TimeoutMs != 0 && (options.TimeoutMs < 100 || options.TimeoutMs > streamexec.MaxProgramSourceTimeoutMs) {
		return options, streamexec.ErrSourceDenied
	}
	if len(options.Headers) > 64 || len(options.Form) > 100 {
		return options, streamexec.ErrSourceDenied
	}
	if len(options.QueryValues) > 100 {
		return options, streamexec.ErrSourceDenied
	}
	for key, values := range options.QueryValues {
		if len(key) > 256 || len(values) > 100 {
			return options, streamexec.ErrSourceDenied
		}
		if _, duplicate := options.Query[key]; duplicate {
			return options, streamexec.ErrSourceDenied
		}
		for _, value := range values {
			if len(value) > 8192 {
				return options, streamexec.ErrSourceDenied
			}
		}
	}
	headerBytes := 0
	headerNames := map[string]bool{}
	for key, value := range options.Headers {
		name := strings.ToLower(key)
		if headerNames[name] {
			return options, streamexec.ErrSourceDenied
		}
		headerNames[name] = true
		headerBytes += len(key) + len(value)
		if !validHTTPHeaderName(key) || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
			return options, streamexec.ErrSourceDenied
		}
		switch strings.ToLower(key) {
		case "host", "connection", "transfer-encoding", "content-length", "proxy-authorization", "proxy-connection", "upgrade", "trailer", "te":
			return options, streamexec.ErrSourceDenied
		}
	}
	if headerBytes > 32*1024 {
		return options, streamexec.ErrSourceDenied
	}
	bodies := 0
	if len(options.JSON) > 0 {
		bodies++
	}
	if options.Body != nil {
		bodies++
	}
	if options.Form != nil {
		bodies++
	}
	if bodies > 1 || (options.Method == "GET" || options.Method == "HEAD") && bodies > 0 {
		return options, streamexec.ErrSourceDenied
	}
	if _, err = httpRequestBody(options); err != nil {
		return options, err
	}
	return options, nil
}
func validHTTPHeaderName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}
func httpRequestBody(options HTTPRequestOptions) ([]byte, error) {
	var body []byte
	switch {
	case len(options.JSON) > 0:
		if !json.Valid(options.JSON) {
			return nil, streamexec.ErrSourceDenied
		}
		body = options.JSON
	case options.Body != nil:
		body = []byte(*options.Body)
	case options.Form != nil:
		values := url.Values{}
		for key, value := range options.Form {
			if len(key) > 256 || len(value) > maxHTTPBytes {
				return nil, streamexec.ErrSourceDenied
			}
			values.Set(key, value)
		}
		body = []byte(values.Encode())
	}
	if len(body) > maxHTTPBytes {
		return nil, streamexec.ErrSourceDenied
	}
	return body, nil
}

func httpRequestURL(endpoint string, options HTTPRequestOptions) (string, error) {
	path := options.Path
	if strings.HasPrefix(path, "//") {
		return "", streamexec.ErrSourceDenied
	}
	// A leading slash is API-relative, not an escape from the Source prefix.
	path = strings.TrimPrefix(path, "/")
	parsed, err := url.Parse(path)
	if err != nil {
		return "", streamexec.ErrSourceDenied
	}
	lower := strings.ToLower(parsed.Path)
	if strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", streamexec.ErrSourceDenied
	}
	destination, err := sourceReadURL(endpoint, httpReadOptions{Path: path, Query: options.Query})
	if err != nil {
		return "", err
	}
	parsedDestination, err := url.Parse(destination)
	if err != nil {
		return "", streamexec.ErrSourceDenied
	}
	query := parsedDestination.Query()
	for key, values := range options.QueryValues {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	parsedDestination.RawQuery = query.Encode()
	return parsedDestination.String(), nil
}

// HTTPProgramCall performs no dependency authorization itself: ProgramV2Host
// supplies an admitted/frozen Source. It never persists request or response data.
func (s *Service) HTTPProgramCall(ctx context.Context, source repo.Source, def SourceDefinition, call streamexec.ProgramCall) (json.RawMessage, error) {
	return s.httpProgramCall(ctx, source, def, call, nil)
}
func (s *Service) httpProgramCall(ctx context.Context, source repo.Source, def SourceDefinition, call streamexec.ProgramCall, shared *Credential) (json.RawMessage, error) {
	if def.HTTP == nil || (def.Protocol != HTTP && def.Protocol != OAuth) {
		return nil, streamexec.ErrSourceDenied
	}
	if call.Kind == "source.parameters" {
		values := def.HTTP.Parameters
		if values == nil {
			values = map[string]any{}
		}
		return json.Marshal(values)
	}
	if call.Kind == "source.secrets" {
		if def.CredentialRef == "" {
			return json.RawMessage(`{}`), nil
		}
		currentDefinition, credential, _, err := s.connection(ctx, source.ID, nil)
		if err != nil {
			return nil, ErrCredential
		}
		if currentDefinition.CredentialRef != def.CredentialRef {
			return nil, repo.ErrConflict
		}
		values := httpCredentialValues(credential)
		if def.SourceType != "" && credential.OAuth != nil {
			delete(values, "accessToken")
			delete(values, "refreshToken")
		}
		recordHTTPCredential(ctx, credential)
		return json.Marshal(values)
	}
	if call.Kind != "source.request" || def.ConnectionRole() != ClientRole {
		return nil, streamexec.ErrSourceDenied
	}
	options, err := parseHTTPRequest(call.Options)
	if err != nil {
		return nil, err
	}
	destination, err := httpRequestURL(def.Endpoint, options)
	if err != nil {
		return nil, streamexec.ErrSourceDenied
	}
	body, err := httpRequestBody(options)
	if err != nil {
		return nil, err
	}
	timeout := streamexec.DefaultSourceCallTimeout
	if options.TimeoutMs != 0 {
		timeout = time.Duration(options.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, options.Method, destination, bytes.NewReader(body))
	if err != nil {
		return nil, ErrHTTPRequest
	}
	// This profile grants only the public status projection, even to local
	// Stream scripts using the lower-level HTTP request API.
	if def.SourceType == "plant0-simctl" && (req.Method != http.MethodGet ||
		req.URL.EscapedPath() != "/portal/api/board" || req.URL.RawQuery != "minutes=30" ||
		len(options.Headers) != 0 || len(body) != 0 || def.CredentialRef != "") {
		return nil, streamexec.ErrSourceDenied
	}
	readOnly, _ := ctx.Value(vendorReadOnlyContext{}).(bool)
	if options.Method != "GET" && options.Method != "HEAD" && options.Method != "OPTIONS" {
		// No pooled connection replay of operations with possible side effects,
		// even if a caller supplies an Idempotency-Key header. Only code-owned
		// registered read operations may retain their Source connection.
		req.Close = !readOnly
		req.GetBody = nil
	}
	if err := applySourceHTTPHeaders(req, def); err != nil {
		return nil, err
	}
	for key, value := range options.Headers {
		req.Header.Set(key, value)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	if len(options.JSON) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if options.Form != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	credential := Credential{}
	if def.CredentialRef != "" {
		if shared != nil {
			credential = *shared
		} else {
			currentDefinition, resolved, _, err := s.connection(ctx, source.ID, nil)
			if err != nil {
				return nil, ErrCredential
			}
			if currentDefinition.CredentialRef != def.CredentialRef {
				return nil, repo.ErrConflict
			}
			credential = resolved
			def.tlsConfig = currentDefinition.tlsConfig
		}
		recordHTTPCredential(ctx, credential)
	}
	// Recheck the current native Source after resolving its credential and
	// before vendor authentication can issue a request for the frozen endpoint.
	if shared == nil && source.Revision > 0 {
		current, err := repo.NewSourceStreamRepo(ctx).Source(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		if current.DesiredState == "disabled" || current.ConfigRevision != source.ConfigRevision {
			return nil, repo.ErrConflict
		}
	}
	injectedOAuthToken := ""
	if def.Protocol == OAuth {
		if req.Header.Get("Authorization") == "" {
			token, err := s.oauthAccessToken(ctx, source.ID, def)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, streamexec.ErrTimeout) || errors.Is(err, context.Canceled) {
					return nil, safeHTTPFailure(ctx, err)
				}
				return nil, ErrCredential
			}
			recordProgramSecretValues(ctx, map[string]string{"accessToken": token})
			req.Header.Set("Authorization", "Bearer "+token)
			injectedOAuthToken = token
		}
	} else if auth := def.HTTPAuthentication(); auth != "none" {
		if credential.Kind != auth && credential.Kind != "custom" && credential.Kind != "http" {
			return nil, ErrCredential
		}
		switch credential.Kind {
		case "basic":
			if req.Header.Get("Authorization") == "" && def.SourceType != "hikvision-isapi" && def.SourceType != "teamcenter" {
				req.SetBasicAuth(credential.Username, credential.Secret)
			}
		case "bearer":
			if req.Header.Get("Authorization") == "" {
				req.Header.Set("Authorization", "Bearer "+credential.Secret)
			}
		case "custom", "http": // Script chooses where its named secrets belong.
		default:
			return nil, ErrCredential
		}
	}
	if def.SourceType != "" {
		if err := s.vendorAuthorize(ctx, source, def, credential, req, body); err != nil {
			return nil, err
		}
	}
	recordProgramSecretValues(ctx, map[string]string{"authorization": req.Header.Get("Authorization")})
	client, err := s.httpManager.client(source, def)
	if err != nil {
		return nil, err
	}
	var response *http.Response
	for attempt := 0; ; attempt++ {
		response, err = client.Do(req)
		if err != nil {
			return nil, safeHTTPFailure(ctx, err)
		}
		if response.StatusCode != http.StatusUnauthorized || injectedOAuthToken == "" ||
			req.Header.Get("Authorization") != "Bearer "+injectedOAuthToken ||
			(req.Method != http.MethodGet && req.Method != http.MethodHead) {
			break
		}
		response.Body.Close()
		if err = s.rejectOAuthToken(ctx, source.ID, def, injectedOAuthToken); err != nil {
			return nil, err
		}
		if attempt == 1 {
			return nil, ErrOAuthRequired
		}
		injectedOAuthToken, err = s.oauthAccessToken(ctx, source.ID, def)
		if err != nil {
			return nil, safeHTTPFailureWithFallback(ctx, err, ErrCredential)
		}
		// Only retry this explicit rejection of our own bearer on a read. Keep
		// the exact URL/body and original deadline; never replay a transport error.
		req = req.Clone(ctx)
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+injectedOAuthToken)
		recordProgramSecretValues(ctx, map[string]string{"accessToken": injectedOAuthToken})
	}
	if response.StatusCode == http.StatusUnauthorized && def.SourceType == "hikvision-isapi" {
		challenge := response.Header.Get("WWW-Authenticate")
		response.Body.Close()
		authorization, digestErr := vendorDigest(challenge, req, credential)
		if digestErr != nil {
			return nil, digestErr
		}
		// An authentication challenge did not execute the operation. Only replay
		// this explicit challenge, never a transport timeout or accepted request.
		req = req.Clone(ctx)
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.Header.Set("Authorization", authorization)
		response, err = client.Do(req)
		if err != nil {
			return nil, safeHTTPFailure(ctx, err)
		}
	}
	canRecoverSession := (def.SourceType == "teamcenter" || def.SourceType == "kingdee-k3cloud") && (options.Method == "GET" || options.Method == "HEAD" || readOnly)
	var data []byte
	for attempt := 0; ; attempt++ {
		data, err = io.ReadAll(io.LimitReader(response.Body, maxHTTPResponseBytes+1))
		response.Body.Close()
		if err != nil {
			return nil, safeHTTPFailure(ctx, err)
		}
		if len(data) > maxHTTPResponseBytes {
			return nil, ErrHTTPRequest
		}
		if !canRecoverSession || attempt > 0 {
			break
		}
		rejected := response.StatusCode == http.StatusUnauthorized
		if (def.SourceType == "teamcenter" || def.SourceType == "kingdee-k3cloud") && response.StatusCode >= 200 && response.StatusCode < 300 {
			value, decodeErr := vendorJSON(data)
			rejected = decodeErr == nil && vendorBusinessError(def.SourceType, value) == "vendorSessionExpired"
		}
		if !rejected {
			break
		}
		// Vendor APIs may report an expired session in HTTP200. Recover only an explicit
		// authentication rejection, once, under the original request deadline.
		s.httpManager.CloseSource(source.ID)
		if err := s.vendorLogin(ctx, source, def, credential); err != nil {
			return nil, err
		}
		client, err = s.httpManager.client(source, def)
		if err != nil {
			return nil, err
		}
		req = req.Clone(ctx)
		// Client.Do attached the old jar cookies to this request. The replacement
		// Source client must supply only its newly authenticated session cookies.
		req.Header.Del("Cookie")
		req.Body = io.NopCloser(bytes.NewReader(body))
		response, err = client.Do(req)
		if err != nil {
			return nil, safeHTTPFailure(ctx, err)
		}
	}
	if ctx.Value(edgeResourceReadContext{}) == true {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
			return nil, EdgeHTTPResponseError(response.StatusCode)
		}
	}
	var value any
	if len(data) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil || !json.Valid(data) {
			value = string(data)
		}
		if def.SourceType != "" && ctx.Value(preserveVendorJSONNumbers{}) != true {
			value = vendorNumbers(value)
		}
	}
	headers := map[string][]string{}
	for key, values := range response.Header {
		switch strings.ToLower(key) {
		case "set-cookie", "authorization", "proxy-authenticate", "www-authenticate", "x-csrf-token":
			continue
		}
		headers[key] = append([]string{}, values...)
	}
	result, err := json.Marshal(HTTPResponse{Status: response.StatusCode, Headers: headers, Data: value})
	if err != nil || len(result) > maxHTTPResponseBytes {
		return nil, ErrHTTPRequest
	}
	return result, nil
}
func httpCredentialValues(credential Credential) map[string]string {
	values := map[string]string{}
	for key, value := range credential.Values {
		if strings.HasPrefix(key, "_vendor") || tlsValueKey(key) {
			continue
		}
		values[key] = value
	}
	if credential.Secret != "" {
		if _, exists := values["secret"]; !exists {
			values["secret"] = credential.Secret
		}
	}
	if credential.Username != "" {
		if _, exists := values["username"]; !exists {
			values["username"] = credential.Username
		}
	}
	if credential.OAuth != nil {
		if credential.OAuth.AccessToken != "" {
			values["accessToken"] = credential.OAuth.AccessToken
		}
		if credential.OAuth.RefreshToken != "" {
			values["refreshToken"] = credential.OAuth.RefreshToken
		}
	}
	return values
}

func recordHTTPCredential(ctx context.Context, credential Credential) {
	recordProgramSecretValues(ctx, credential.Values)
	recordProgramSecretValues(ctx, map[string]string{"secret": credential.Secret, "username": credential.Username})
	if credential.OAuth != nil {
		recordProgramSecretValues(ctx, map[string]string{"accessToken": credential.OAuth.AccessToken, "refreshToken": credential.OAuth.RefreshToken})
	}
}

type programSecretGuardKey struct{}

// This guard prevents accidental direct-value persistence, not intentional
// encoding/exfiltration by a malicious script. It is local to one execution.
type ProgramSecretGuard struct {
	mu     sync.Mutex
	values map[string]struct{}
}

func WithProgramSecretGuard(ctx context.Context) (context.Context, *ProgramSecretGuard) {
	if existing, _ := ctx.Value(programSecretGuardKey{}).(*ProgramSecretGuard); existing != nil {
		return ctx, existing
	}
	guard := &ProgramSecretGuard{values: map[string]struct{}{}}
	return context.WithValue(ctx, programSecretGuardKey{}, guard), guard
}
func recordProgramSecretValues(ctx context.Context, values map[string]string) {
	guard, _ := ctx.Value(programSecretGuardKey{}).(*ProgramSecretGuard)
	if guard == nil {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	for _, value := range values {
		if value != "" {
			guard.values[value] = struct{}{}
		}
	}
}
func (g *ProgramSecretGuard) RedactResult(result *streamexec.ProgramResult) {
	if g == nil || result == nil {
		return
	}
	g.mu.Lock()
	values := make([]string, 0, len(g.values))
	for value := range g.values {
		values = append(values, value)
	}
	g.mu.Unlock()
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	pairs := make([]string, 0, len(values)*2)
	for _, value := range values {
		pairs = append(pairs, value, "[REDACTED]")
	}
	replacer := strings.NewReplacer(pairs...)
	redact := replacer.Replace
	var walk func(any) any
	walk = func(value any) any {
		switch item := value.(type) {
		case string:
			return redact(item)
		case []any:
			for i := range item {
				item[i] = walk(item[i])
			}
			return item
		case map[string]any:
			out := make(map[string]any, len(item))
			for key, child := range item {
				out[redact(key)] = walk(child)
			}
			return out
		default:
			return item
		}
	}
	redactJSON := func(raw json.RawMessage) json.RawMessage {
		if len(raw) == 0 {
			return raw
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil || !json.Valid(raw) {
			return json.RawMessage(`null`)
		}
		out, err := json.Marshal(walk(value))
		if err != nil {
			return json.RawMessage(`null`)
		}
		return out
	}
	result.Data = redactJSON(result.Data)
	if result.Checkpoint != nil {
		result.Checkpoint.Value = redactJSON(result.Checkpoint.Value)
	}
	for i := range result.Outputs {
		result.Outputs[i].Data = redactJSON(result.Outputs[i].Data)
	}
	for i := range result.Logs {
		result.Logs[i].Message = redact(result.Logs[i].Message)
	}
	result.Error = redact(result.Error)
}
