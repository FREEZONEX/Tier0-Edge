package sourcestream

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

var ErrOAuth = errors.New("SourceStream.oauthFailed")
var ErrOAuthState = errors.New("SourceStream.oauthStateInvalid")
var ErrOAuthRequired = errors.New("SourceStream.oauthRequired")

// Public connection settings only. Secrets and transient authorization state
// are encrypted with the existing Source credential key.
type HTTPConnection struct {
	Headers        map[string]string `json:"headers,omitempty"`
	Authentication string            `json:"authentication,omitempty"`
	CACertificate  string            `json:"caCertificate,omitempty"`
	OAuth          *OAuthConfig      `json:"oauth,omitempty"`
	Parameters     map[string]any    `json:"parameters,omitempty"`
	Port           int               `json:"port,omitempty"`
	Anonymous      bool              `json:"anonymous,omitempty"`
}
type OAuthConfig struct {
	GrantType            string `json:"grantType,omitempty"`
	AuthorizationURL     string `json:"authorizationUrl"`
	TokenURL             string `json:"tokenUrl"`
	ClientID             string `json:"clientId"`
	ClientAuthentication string `json:"clientAuthentication"`
	Scopes               string `json:"scopes"`
}
type OAuthCredential struct {
	ExchangeState       string        `json:"exchangeState,omitempty"`
	ClientConfiguration string        `json:"clientConfiguration"`
	AccessToken         string        `json:"accessToken,omitempty"`
	RefreshToken        string        `json:"refreshToken,omitempty"`
	ExpiresAt           int64         `json:"expiresAt,omitempty"`
	Configuration       string        `json:"configuration,omitempty"`
	Pending             *oauthPending `json:"pending,omitempty"`
}
type oauthPending struct {
	State         string `json:"state"`
	Verifier      string `json:"verifier"`
	RedirectURI   string `json:"redirectUri"`
	UserID        int64  `json:"userId"`
	ExpiresAt     int64  `json:"expiresAt"`
	Configuration string `json:"configuration"`
}

func (o OAuthConfig) validate() error {
	if (o.GrantType != "" && o.GrantType != "authorization_code" && o.GrantType != "client_credentials") || (o.GrantType != "client_credentials" && !validHTTPS(o.AuthorizationURL)) || !validHTTPS(o.TokenURL) || strings.TrimSpace(o.ClientID) == "" || len(o.ClientID) > 1024 || len(o.Scopes) > 4096 {
		return invalid("http.oauth", "invalid")
	}
	if o.GrantType == "client_credentials" && (o.ClientAuthentication == "none" || o.AuthorizationURL != "") {
		return invalid("http.oauth", "invalid")
	}
	switch o.ClientAuthentication {
	case "none", "client_secret_basic", "client_secret_post":
		return nil
	}
	return invalid("http.oauth.clientAuthentication", "unsupported")
}
func validHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}
func oauthConfiguration(d SourceDefinition) string {
	b, _ := json.Marshal(struct {
		Endpoint string
		HTTP     *HTTPConnection
	}{d.Endpoint, d.HTTP})
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func oauthRandom() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), e
}
func validOAuthRedirect(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.User == nil && u.Fragment == "" && u.RawQuery == "" && u.Path == "/uns" && u.Hostname() != "" && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")))
}

// OAuth writes share the Source row lock across instances. Token refresh and
// one-time authorization state do not change the user configuration revision.
func (s *Service) changeOAuth(ctx context.Context, id int64, fn func(SourceDefinition, *Credential) error) error {
	return repo.NewSourceStreamRepo(ctx).ChangeCredential(ctx, id, func(row repo.Source, encrypted *repo.SourceCredential) error {
		var d SourceDefinition
		if json.Unmarshal([]byte(row.Document), &d) != nil || d.Protocol != OAuth || d.HTTP == nil || d.HTTP.OAuth == nil {
			return ErrCredential
		}
		if err := d.Validate(); err != nil {
			return err
		}
		c, e := s.cipher.Open(id, encrypted.Ciphertext)
		if e != nil {
			return e
		}
		if c.Kind != "oauth2" || c.OAuth == nil {
			return ErrOAuthRequired
		}
		if e = fn(d, &c); e != nil {
			return e
		}
		encrypted.Ciphertext, e = s.cipher.Seal(id, c)
		encrypted.UpdatedAt = time.Now().UnixMilli()
		return e
	})
}
func (s *Service) OAuthStart(ctx context.Context, id int64, redirect string) (map[string]any, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID <= 0 || !contextx.HasResource(subject, "uns.source.credentials") {
		return nil, ErrForbidden
	}
	if !validOAuthRedirect(redirect) {
		return nil, invalid("redirectUri", "invalid")
	}
	if item, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id); err == nil {
		var d SourceDefinition
		if json.Unmarshal([]byte(item.Document), &d) == nil && d.SourceType == "ngp" {
			return s.ngpAuthorizationStart(ctx, id, subject.UserID, redirect)
		}
	}
	state, e := oauthRandom()
	if e != nil {
		return nil, e
	}
	verifier, e := oauthRandom()
	if e != nil {
		return nil, e
	}
	var destination string
	e = s.changeOAuth(ctx, id, func(d SourceDefinition, c *Credential) error {
		o := d.HTTP.OAuth
		if o.GrantType == "client_credentials" {
			return invalid("http.oauth.grantType", "unsupported")
		}
		if c.OAuth.ClientConfiguration != oauthConfiguration(d) {
			return ErrOAuthRequired
		}
		if o.ClientAuthentication != "none" && c.Secret == "" {
			return ErrCredential
		}
		c.OAuth.ExchangeState = ""
		c.OAuth.Pending = &oauthPending{State: state, Verifier: verifier, RedirectURI: redirect, UserID: subject.UserID, ExpiresAt: time.Now().Add(10 * time.Minute).Unix(), Configuration: oauthConfiguration(d)}
		u, _ := url.Parse(o.AuthorizationURL)
		q := u.Query()
		challenge := sha256.Sum256([]byte(verifier))
		q.Set("response_type", "code")
		q.Set("client_id", o.ClientID)
		q.Set("redirect_uri", redirect)
		q.Set("scope", o.Scopes)
		q.Set("state", state)
		q.Set("code_challenge_method", "S256")
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		u.RawQuery = q.Encode()
		destination = u.String()
		return nil
	})
	return map[string]any{"authorizationUrl": destination, "state": state}, e
}
func (s *Service) OAuthComplete(ctx context.Context, id int64, state, code string) (map[string]any, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID <= 0 || !contextx.HasResource(subject, "uns.source.credentials") {
		return nil, ErrForbidden
	}
	if len(code) > 8192 || len(state) != 43 || code == "" {
		return nil, ErrOAuthState
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if item, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id); err == nil {
		var d SourceDefinition
		if json.Unmarshal([]byte(item.Document), &d) == nil && d.SourceType == "ngp" {
			return s.ngpAuthorizationComplete(ctx, id, subject.UserID, state, code)
		}
	}
	var definition SourceDefinition
	var secret string
	var pending oauthPending
	err := s.changeOAuth(ctx, id, func(d SourceDefinition, c *Credential) error {
		p := c.OAuth.Pending
		if p == nil || p.UserID != subject.UserID || p.ExpiresAt <= time.Now().Unix() || p.Configuration != oauthConfiguration(d) || subtle.ConstantTimeCompare([]byte(p.State), []byte(state)) != 1 {
			return ErrOAuthState
		}
		definition = d
		secret = c.Secret
		pending = *p
		c.OAuth.Pending = nil
		c.OAuth.ExchangeState = state
		return nil
	})
	if err != nil {
		return nil, err
	}
	token, err := s.oauthToken(ctx, definition, secret, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {pending.RedirectURI}, "code_verifier": {pending.Verifier}})
	if err != nil {
		return nil, err
	}
	err = s.changeOAuth(ctx, id, func(d SourceDefinition, c *Credential) error {
		if c.OAuth.ExchangeState != state || oauthConfiguration(d) != pending.Configuration {
			return ErrOAuthState
		}
		c.OAuth = token
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Source(ctx, id)
}
func (s *Service) oauthToken(ctx context.Context, d SourceDefinition, secret string, form url.Values) (*OAuthCredential, error) {
	o := d.HTTP.OAuth
	form.Set("client_id", o.ClientID)
	if o.ClientAuthentication == "client_secret_post" {
		form.Set("client_secret", secret)
	}
	tokenCtx, e := oauthTokenContext(ctx, o.TokenURL)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(tokenCtx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, ErrOAuth
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if o.ClientAuthentication == "client_secret_basic" {
		req.SetBasicAuth(url.QueryEscape(o.ClientID), url.QueryEscape(secret))
	}
	client, cleanup, e := s.httpClientFor(d)
	if e != nil {
		return nil, e
	}
	defer cleanup()
	// Token-only private-origin permissions must never seed the shared HTTP
	// connection pool: a later unscoped request could otherwise bypass dialing.
	if transport, ok := client.Transport.(*http.Transport); ok {
		isolated := transport.Clone()
		defer isolated.CloseIdleConnections()
		copyClient := *client
		copyClient.Transport = isolated
		client = &copyClient
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, safeHTTPFailureWithFallback(ctx, e, ErrOAuth)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrOAuth
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, 128*1024+1))
	if e != nil {
		return nil, safeHTTPFailureWithFallback(ctx, e, ErrOAuth)
	}
	if len(body) > 128*1024 {
		return nil, ErrOAuth
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &token) != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") || len(token.AccessToken) > 65536 || len(token.RefreshToken) > 65536 || strings.ContainsAny(token.AccessToken, "\r\n") || token.ExpiresIn < 0 || token.ExpiresIn > 315360000 {
		return nil, ErrOAuth
	}
	result := &OAuthCredential{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, Configuration: oauthConfiguration(d), ClientConfiguration: oauthConfiguration(d)}
	if token.ExpiresIn > 0 {
		result.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
	}
	return result, nil
}

// Persist rejection separately from refresh: a failed token exchange must not
// roll back invalidation and leave an unusable credential marked authorized.
func (s *Service) rejectOAuthToken(ctx context.Context, id int64, expected SourceDefinition, rejected string) error {
	return s.changeOAuth(ctx, id, func(d SourceDefinition, c *Credential) error {
		configuration := oauthConfiguration(d)
		if configuration != oauthConfiguration(expected) || c.OAuth.ClientConfiguration != configuration {
			return ErrOAuthRequired
		}
		// Another request may already have rotated the token under the row lock.
		if c.OAuth.AccessToken == rejected {
			c.OAuth.AccessToken = ""
			c.OAuth.ExpiresAt = 0
		}
		return nil
	})
}

func (s *Service) oauthAccessToken(ctx context.Context, id int64, expected SourceDefinition) (string, error) {
	var access string
	err := s.changeOAuth(ctx, id, func(d SourceDefinition, c *Credential) error {
		token := c.OAuth
		configuration := oauthConfiguration(d)
		if configuration != oauthConfiguration(expected) || token.ClientConfiguration != configuration {
			return ErrOAuthRequired
		}
		expired := token.ExpiresAt > 0 && token.ExpiresAt <= time.Now().Add(30*time.Second).Unix()
		if token.Configuration != configuration || token.AccessToken == "" || expired {
			form := url.Values{}
			if d.HTTP.OAuth.GrantType == "client_credentials" {
				if c.Secret == "" {
					return ErrCredential
				}
				form.Set("grant_type", "client_credentials")
				if d.HTTP.OAuth.Scopes != "" {
					form.Set("scope", d.HTTP.OAuth.Scopes)
				}
			} else {
				if token.Configuration != configuration || token.RefreshToken == "" {
					return ErrOAuthRequired
				}
				form.Set("grant_type", "refresh_token")
				form.Set("refresh_token", token.RefreshToken)
			}
			updated, e := s.oauthToken(ctx, d, c.Secret, form)
			if e != nil {
				return e
			}
			if updated.RefreshToken == "" && d.HTTP.OAuth.GrantType != "client_credentials" {
				updated.RefreshToken = token.RefreshToken
			}
			updated.Pending = token.Pending
			updated.ExchangeState = token.ExchangeState
			c.OAuth = updated
		}
		access = c.OAuth.AccessToken
		return nil
	})
	return access, err
}
