package sourcestream

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" // Vendor compatibility only; never password storage.
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/repo"
)

var ErrVendorResponse = errors.New("SourceStream.vendorResponseFailed")

type vendorToken struct {
	SourceType    string `json:"sourceType"`
	Token         string `json:"token"`
	Refresh       string `json:"refresh,omitempty"`
	Expires       int64  `json:"expires"`
	Configuration string `json:"configuration"`
}

// A malicious endpoint cannot grow a shared Backend's cookie jar indefinitely.
// Exceeding the per-Source budget rejects additional cookies rather than evicting
// another Source's session or silently expanding memory limits.
type vendorCookieJar struct {
	mu    sync.Mutex
	jar   http.CookieJar
	sizes map[string]int
	bytes int
}

func newVendorCookieJar() http.CookieJar {
	jar, _ := cookiejar.New(nil)
	return &vendorCookieJar{jar: jar, sizes: map[string]int{}}
}
func (j *vendorCookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.jar.Cookies(u)
}
func (j *vendorCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, cookie := range cookies {
		if cookie == nil || len(cookie.Name) > 256 || len(cookie.Value) > 4096 || len(cookie.Domain)+len(cookie.Path) > 2048 {
			continue
		}
		key := u.Hostname() + "\x00" + cookie.Domain + "\x00" + cookie.Path + "\x00" + cookie.Name
		size := len(key) + len(cookie.Value)
		old, exists := j.sizes[key]
		deleting := cookie.MaxAge < 0 || !cookie.Expires.IsZero() && cookie.Expires.Before(time.Now())
		if deleting {
			delete(j.sizes, key)
			j.bytes -= old
		} else {
			if !exists && len(j.sizes) >= 64 || j.bytes-old+size > 32768 {
				continue
			}
			j.sizes[key], j.bytes = size, j.bytes-old+size
		}
		j.jar.SetCookies(u, []*http.Cookie{cookie})
	}
}

func vendorParameter(d SourceDefinition, name string) string {
	if d.HTTP == nil {
		return ""
	}
	value := d.HTTP.Parameters[name]
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func vendorSecret(c Credential, name string) string {
	if value := c.Values[name]; value != "" {
		return value
	}
	if name == "username" {
		return c.Username
	}
	if name == "password" || name == "token" {
		return c.Secret
	}
	return ""
}
func vendorMAC(secret, data string, legacy bool) string {
	if legacy {
		h := hmac.New(md5.New, []byte(secret))
		h.Write([]byte(data))
		return hex.EncodeToString(h.Sum(nil))
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
func vendorRandom() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(value[:])
}

// Auth endpoints are code-owned relative paths on the configured origin. They
// cannot redirect or borrow another Source's transport/cookies/credential.
func vendorOriginURL(d SourceDefinition, path string) (string, error) {
	base, err := url.Parse(d.Endpoint)
	if err != nil || !validHTTPBase(d.Endpoint) || !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return "", ErrHTTPRequest
	}
	base.Path = path
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func (s *Service) vendorRaw(ctx context.Context, row repo.Source, d SourceDefinition, c Credential, method, path string, query url.Values, body []byte, headers http.Header) (*HTTPResponse, error) {
	destination, err := vendorOriginURL(d, path)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(destination)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, ErrHTTPRequest
	}
	if headers != nil {
		req.Header = headers.Clone()
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Kind == "basic" {
		req.SetBasicAuth(c.Username, c.Secret)
	}
	if method != "GET" && method != "HEAD" {
		req.GetBody = nil
		req.Close = true
	}
	client, err := s.httpManager.client(row, d)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, safeHTTPFailure(ctx, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxHTTPBytes+1))
	if err != nil || len(raw) > maxHTTPBytes {
		return nil, ErrHTTPRequest
	}
	value, err := vendorJSON(raw)
	if err != nil {
		return nil, ErrVendorResponse
	}
	return &HTTPResponse{Status: res.StatusCode, Headers: res.Header, Data: value}, nil
}

func vendorJSON(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return string(raw), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return vendorNumbers(value), nil
}

// IDs above JavaScript's safe integer range and decimal values enter Goja as
// strings before any float conversion. Contract clients choose decimal math.
func vendorNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		integer, err := typed.Int64()
		if err != nil || integer > 9007199254740991 || integer < -9007199254740991 {
			return typed.String()
		}
		return integer
	case []any:
		for index, child := range typed {
			typed[index] = vendorNumbers(child)
		}
	case map[string]any:
		for key, child := range typed {
			typed[key] = vendorNumbers(child)
		}
	}
	return value
}

func vendorObject(value any) map[string]any { result, _ := value.(map[string]any); return result }
func vendorString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func vendorLifetime(value any, fallback int64) int64 {
	v, err := strconv.ParseInt(vendorString(value), 10, 64)
	if err != nil || v <= 0 || v > 31536000 {
		return fallback
	}
	return v
}

// A database credential row lock serializes refresh across Backend instances.
// Tokens are re-sealed with the existing Source key; no plaintext token table.
func (s *Service) vendorAccessToken(ctx context.Context, row repo.Source, d SourceDefinition) (string, error) {
	var token string
	err := repo.NewSourceStreamRepo(ctx).ChangeCredential(ctx, row.ID, func(current repo.Source, encrypted *repo.SourceCredential) error {
		if current.ConfigRevision != row.ConfigRevision {
			return repo.ErrConflict
		}
		credential, err := s.cipher.Open(row.ID, encrypted.Ciphertext)
		if err != nil {
			return err
		}
		configuration := oauthConfiguration(d)
		var cached vendorToken
		if json.Unmarshal([]byte(credential.Values["_vendorToken"]), &cached) == nil && cached.SourceType == d.SourceType && cached.Configuration == configuration && cached.Token != "" && cached.Expires > time.Now().Add(30*time.Second).Unix() {
			token = cached.Token
			recordProgramSecretValues(ctx, map[string]string{"vendorToken": token})
			return nil
		}
		updated, err := s.vendorExchange(ctx, row, d, credential, cached)
		if err != nil {
			return err
		}
		updated.Configuration = configuration
		updated.SourceType = d.SourceType
		if updated.Expires <= time.Now().Unix() {
			return ErrCredential
		}
		encoded, err := json.Marshal(updated)
		if err != nil {
			return err
		}
		if credential.Values == nil {
			credential.Values = map[string]string{}
		}
		credential.Values["_vendorToken"] = string(encoded)
		encrypted.Ciphertext, err = s.cipher.Seal(row.ID, credential)
		if err != nil {
			return err
		}
		encrypted.UpdatedAt = time.Now().UnixMilli()
		token = updated.Token
		recordProgramSecretValues(ctx, map[string]string{"vendorToken": token, "vendorRefresh": updated.Refresh})
		return nil
	})
	return token, err
}

func (s *Service) vendorExchange(ctx context.Context, row repo.Source, d SourceDefinition, c Credential, previous vendorToken) (vendorToken, error) {
	now := time.Now().Unix()
	var response *HTTPResponse
	var err error
	switch d.SourceType {
	case "yonyou-yonbip":
		key, secret := vendorSecret(c, "appKey"), vendorSecret(c, "appSecret")
		if key == "" || secret == "" {
			return vendorToken{}, ErrCredential
		}
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte("appKey" + key + "timestamp" + timestamp))
		response, err = s.vendorRaw(ctx, row, d, c, "GET", "/open-auth/selfAppAuth/getAccessToken", url.Values{"appKey": {key}, "timestamp": {timestamp}, "signature": {base64.StdEncoding.EncodeToString(h.Sum(nil))}}, nil, nil)
		if err != nil {
			return vendorToken{}, err
		}
		obj := vendorObject(response.Data)
		data := vendorObject(obj["data"])
		if response.Status != 200 || vendorString(obj["code"]) != "00000" || vendorString(data["access_token"]) == "" {
			return vendorToken{}, ErrCredential
		}
		return vendorToken{Token: vendorString(data["access_token"]), Expires: now + vendorLifetime(data["expire"], 7200)}, nil
	case "kingdee-jdy":
		key, secret := vendorSecret(c, "appKey"), vendorSecret(c, "appSecret")
		if key == "" || secret == "" {
			return vendorToken{}, ErrCredential
		}
		query := url.Values{"app_key": {key}, "app_signature": {base64.StdEncoding.EncodeToString([]byte(vendorMAC(secret, key, false)))}}
		headers := http.Header{}
		if err = jdySignature(headers, "GET", "/jdyconnector/app_management/kingdee_auth_token", query, c); err != nil {
			return vendorToken{}, err
		}
		response, err = s.vendorRaw(ctx, row, d, c, "GET", "/jdyconnector/app_management/kingdee_auth_token", query, nil, headers)
		if err != nil {
			return vendorToken{}, err
		}
		obj := vendorObject(response.Data)
		data := vendorObject(obj["data"])
		if response.Status != 200 || vendorString(data["app-token"]) == "" && vendorString(data["app_token"]) == "" {
			return vendorToken{}, ErrCredential
		}
		token := vendorString(data["app-token"])
		if token == "" {
			token = vendorString(data["app_token"])
		}
		return vendorToken{Token: token, Expires: now + vendorLifetime(data["expires_in"], 86400)}, nil
	case "ngp":
		key, secret := vendorSecret(c, "appKey"), vendorSecret(c, "appSecret")
		refresh := previous.Refresh
		if refresh == "" {
			refresh = vendorSecret(c, "refreshToken")
		}
		if key == "" || secret == "" || refresh == "" {
			return vendorToken{}, ErrOAuthRequired
		}
		form := url.Values{"app_key": {key}, "app_secret": {secret}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}}
		response, err = s.vendorRaw(ctx, row, d, c, "POST", "/oauth2/token", nil, []byte(form.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
		if err != nil {
			return vendorToken{}, err
		}
		obj := vendorObject(response.Data)
		if response.Status != 200 || vendorString(obj["access_token"]) == "" {
			return vendorToken{}, ErrCredential
		}
		next := vendorString(obj["refresh_token"])
		if next == "" {
			next = refresh
		}
		return vendorToken{Token: vendorString(obj["access_token"]), Refresh: next, Expires: now + vendorLifetime(obj["expires_in"], 3600)}, nil
	}
	return vendorToken{}, ErrCredential
}

func jdySignature(headers http.Header, method, path string, query url.Values, c Credential) error {
	client, secret := vendorSecret(c, "clientId"), vendorSecret(c, "clientSecret")
	if client == "" || secret == "" {
		return ErrCredential
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	value, err := rand.Int(rand.Reader, big.NewInt(9000000000))
	if err != nil {
		return ErrCredential
	}
	nonce := strconv.FormatInt(value.Int64()+1000000000, 10)
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parameters := []string{}
	for _, key := range keys {
		for _, value := range query[key] {
			parameters = append(parameters, key+"="+strings.ReplaceAll(url.QueryEscape(strings.ReplaceAll(url.QueryEscape(value), "+", "%20")), "+", "%20"))
		}
	}
	canonical := method + "\n" + url.QueryEscape(path) + "\n" + strings.Join(parameters, "&") + "\n" + "x-api-nonce:" + nonce + "\nx-api-timestamp:" + timestamp + "\n"
	headers.Set("X-Api-ClientID", client)
	headers.Set("X-Api-Auth-Version", "2.0")
	headers.Set("X-Api-TimeStamp", timestamp)
	headers.Set("X-Api-Nonce", nonce)
	headers.Set("X-Api-SignHeaders", "X-Api-TimeStamp,X-Api-Nonce")
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Api-Signature", base64.StdEncoding.EncodeToString([]byte(vendorMAC(secret, canonical, false))))
	return nil
}

func (s *Service) vendorAuthorize(ctx context.Context, row repo.Source, d SourceDefinition, c Credential, req *http.Request, body []byte) error {
	switch d.SourceType {
	case "plex-mes":
		if key := vendorSecret(c, "apiKey"); key != "" {
			req.Header.Set("X-Plex-Connect-Api-Key", key)
		} else {
			return ErrCredential
		}
		if tenant := vendorParameter(d, "tenantId"); tenant != "" {
			req.Header.Set("X-Plex-Connect-Tenant-Id", tenant)
		}
		if pcn := vendorParameter(d, "pcn"); pcn != "" {
			req.Header.Set("X-Plex-Connect-Customer-Id", pcn)
		}
	case "yonyou-yonbip":
		token, err := s.vendorAccessToken(ctx, row, d)
		if err != nil {
			return err
		}
		query := req.URL.Query()
		query.Set("access_token", token)
		req.URL.RawQuery = query.Encode()
	case "kingdee-jdy":
		token, err := s.vendorAccessToken(ctx, row, d)
		if err != nil {
			return err
		}
		req.Header.Set("app-token", token)
		req.Header.Set("X-GW-Router-Addr", vendorParameter(d, "domain"))
		return jdySignature(req.Header, req.Method, req.URL.Path, req.URL.Query(), c)
	case "ngp":
		token, err := s.vendorAccessToken(ctx, row, d)
		if err != nil {
			return err
		}
		query := req.URL.Query()
		if query.Get("method") == "" {
			query.Set("method", "ngp.ptype.list")
		}
		query.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		query.Set("app_key", vendorSecret(c, "appKey"))
		query.Set("token", token)
		keys := make([]string, 0, len(query))
		for key := range query {
			if key != "sign" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		var text strings.Builder
		for _, key := range keys {
			text.WriteString(key)
			text.WriteString(query.Get(key))
		}
		text.Write(body)
		query.Set("sign", vendorMAC(vendorSecret(c, "appSecret"), text.String(), true))
		req.URL.RawQuery = query.Encode()
	case "teamcenter", "kingdee-k3cloud":
		return s.vendorLogin(ctx, row, d, c)
	case "sap-odata":
		endpoint, err := url.Parse(d.Endpoint)
		if err == nil && endpoint != nil && (req.Method == http.MethodGet || req.Method == http.MethodHead) &&
			req.URL.Path == strings.TrimRight(endpoint.Path, "/")+"/$metadata" {
			req.Header.Set("Accept", "application/xml")
		} else {
			req.Header.Set("Accept", "application/json;IEEE754Compatible=true")
		}
		if req.Method != "GET" && req.Method != "HEAD" && req.Method != "OPTIONS" {
			head, err := http.NewRequestWithContext(ctx, "HEAD", strings.TrimRight(d.Endpoint, "/")+"/", nil)
			if err != nil {
				return ErrHTTPRequest
			}
			head.Header.Set("X-CSRF-Token", "Fetch")
			head.Header.Set("Accept", "application/json")
			if c.Kind == "basic" {
				head.SetBasicAuth(c.Username, c.Secret)
			} else {
				head.Header.Set("Authorization", req.Header.Get("Authorization"))
			}
			client, err := s.httpManager.client(row, d)
			if err != nil {
				return err
			}
			response, err := client.Do(head)
			if err != nil {
				return safeHTTPFailure(ctx, err)
			}
			response.Body.Close()
			token := response.Header.Get("X-CSRF-Token")
			if response.StatusCode < 200 || response.StatusCode >= 300 || token == "" {
				return ErrCredential
			}
			req.Header.Set("X-CSRF-Token", token)
			recordProgramSecretValues(ctx, map[string]string{"csrf": token})
		}
	}
	return nil
}

func (s *Service) vendorLogin(ctx context.Context, row repo.Source, d SourceDefinition, c Credential) error {
	client, err := s.httpManager.client(row, d)
	if err != nil {
		return err
	}
	u, _ := url.Parse(d.Endpoint)
	if client.Jar != nil && len(client.Jar.Cookies(u)) > 0 {
		return nil
	}
	var payload any
	var path string
	if d.SourceType == "teamcenter" {
		path = "/" + strings.Trim(strings.TrimPrefix(u.Path, "/"), "/") + "/JsonRestServices/Core-2011-06-Session/login"
		payload = map[string]any{"header": map[string]any{}, "body": map[string]any{"credentials": map[string]string{"user": vendorSecret(c, "username"), "password": vendorSecret(c, "password"), "group": vendorParameter(d, "group"), "role": vendorParameter(d, "role"), "locale": vendorParameter(d, "locale"), "descrimator": ""}}}
	} else {
		prefix := strings.TrimRight(u.Path, "/")
		lcid, err := strconv.Atoi(vendorParameter(d, "lcid"))
		if err != nil || lcid <= 0 {
			return ErrCredential
		}
		path = prefix + "/Kingdee.BOS.WebApi.ServicesStub.AuthService.ValidateUser.common.kdsvc"
		payload = map[string]any{"parameters": []any{vendorParameter(d, "acctId"), vendorParameter(d, "user"), vendorSecret(c, "password"), lcid}}
		if vendorSecret(c, "password") == "" && vendorSecret(c, "appSecret") != "" {
			path = prefix + "/Kingdee.BOS.WebApi.ServicesStub.AuthService.LoginByAppSecret.common.kdsvc"
			payload = map[string]any{"parameters": []any{vendorParameter(d, "acctId"), vendorParameter(d, "user"), vendorParameter(d, "appId"), vendorSecret(c, "appSecret"), lcid}}
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ErrCredential
	}
	response, err := s.vendorRaw(ctx, row, d, Credential{}, "POST", path, nil, encoded, nil)
	if err != nil {
		return err
	}
	if response.Status < 200 || response.Status >= 300 {
		return ErrCredential
	}
	object := vendorObject(response.Data)
	if d.SourceType == "kingdee-k3cloud" && vendorString(object["LoginResultType"]) != "1" {
		return ErrCredential
	}
	if d.SourceType == "teamcenter" && vendorBusinessError(d.SourceType, object) != "" {
		return ErrCredential
	}
	if client.Jar == nil || len(client.Jar.Cookies(u)) == 0 {
		return ErrCredential
	}
	return nil
}

// RFC 7616 compatibility for the ISAPI MD5/qop=auth profile. Nonce counts are
// fresh for each new challenge; no stale Authorization is reused across calls.
func vendorDigest(challenge string, req *http.Request, c Credential) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return "", ErrCredential
	}
	fields := map[string]string{}
	remaining := strings.TrimSpace(challenge[7:])
	for len(remaining) > 0 {
		remaining = strings.TrimLeft(remaining, " ,\t")
		index := strings.IndexByte(remaining, '=')
		if index < 1 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(remaining[:index]))
		remaining = strings.TrimSpace(remaining[index+1:])
		value := ""
		if strings.HasPrefix(remaining, "\"") {
			remaining = remaining[1:]
			end := strings.IndexByte(remaining, '"')
			if end < 0 {
				return "", ErrCredential
			}
			value = remaining[:end]
			remaining = remaining[end+1:]
		} else {
			end := strings.IndexByte(remaining, ',')
			if end < 0 {
				value = remaining
				remaining = ""
			} else {
				value = remaining[:end]
				remaining = remaining[end+1:]
			}
		}
		fields[key] = value
	}
	if fields["realm"] == "" || fields["nonce"] == "" || fields["algorithm"] != "" && !strings.EqualFold(fields["algorithm"], "MD5") {
		return "", ErrCredential
	}
	qop := false
	for _, item := range strings.Split(fields["qop"], ",") {
		qop = qop || strings.TrimSpace(item) == "auth"
	}
	if !qop {
		return "", ErrCredential
	}
	cnonce := vendorRandom()
	if cnonce == "" {
		return "", ErrCredential
	}
	hash := func(value string) string { sum := md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }
	uri := req.URL.RequestURI()
	nc := "00000001"
	response := hash(hash(c.Username+":"+fields["realm"]+":"+c.Secret) + ":" + fields["nonce"] + ":" + nc + ":" + cnonce + ":auth:" + hash(req.Method+":"+uri))
	quote := func(value string) string { return strconv.Quote(value) }
	result := "Digest username=" + quote(c.Username) + ", realm=" + quote(fields["realm"]) + ", nonce=" + quote(fields["nonce"]) + ", uri=" + quote(uri) + ", response=" + quote(response) + ", algorithm=MD5, qop=auth, nc=" + nc + ", cnonce=" + quote(cnonce)
	if opaque := fields["opaque"]; opaque != "" {
		result += ", opaque=" + quote(opaque)
	}
	return result, nil
}
