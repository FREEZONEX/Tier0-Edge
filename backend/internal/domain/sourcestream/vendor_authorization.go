package sourcestream

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"backend/internal/repo"
)

// Merchant grants use the existing permissioned OAuth endpoints. The vendor's
// app_key contract is not disguised as generic OAuth client_id/PKCE support.
func (s *Service) changeNGP(ctx context.Context, id int64, fn func(repo.Source, SourceDefinition, *Credential) error) error {
	return repo.NewSourceStreamRepo(ctx).ChangeCredential(ctx, id, func(row repo.Source, encrypted *repo.SourceCredential) error {
		var d SourceDefinition
		if json.Unmarshal([]byte(row.Document), &d) != nil || d.SourceType != "ngp" || d.Protocol != HTTP || d.Validate() != nil {
			return ErrCredential
		}
		c, err := s.cipher.Open(id, encrypted.Ciphertext)
		if err != nil || c.Kind != "custom" || vendorSecret(c, "appKey") == "" || vendorSecret(c, "appSecret") == "" {
			return ErrCredential
		}
		if err = fn(row, d, &c); err != nil {
			return err
		}
		encrypted.Ciphertext, err = s.cipher.Seal(id, c)
		encrypted.UpdatedAt = time.Now().UnixMilli()
		return err
	})
}

func (s *Service) ngpAuthorizationStart(ctx context.Context, id, userID int64, redirect string) (map[string]any, error) {
	state, err := oauthRandom()
	if err != nil {
		return nil, err
	}
	var destination string
	err = s.changeNGP(ctx, id, func(_ repo.Source, d SourceDefinition, c *Credential) error {
		// The merchant must authorize this exact operator callback first.
		if configured := vendorParameter(d, "redirectUri"); configured != "" && configured != redirect {
			return invalid("redirectUri", "notRegistered")
		}
		pending := oauthPending{State: state, RedirectURI: redirect, UserID: userID, ExpiresAt: time.Now().Add(10 * time.Minute).Unix(), Configuration: oauthConfiguration(d)}
		raw, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		c.Values["_vendorPending"] = string(raw)
		delete(c.Values, "_vendorExchange")
		base, err := vendorOriginURL(d, "/oauth2/authorize")
		if err != nil {
			return err
		}
		u, _ := url.Parse(base)
		u.RawQuery = url.Values{"response_type": {"code"}, "app_key": {vendorSecret(*c, "appKey")}, "redirect_uri": {redirect}, "state": {state}}.Encode()
		destination = u.String()
		return nil
	})
	return map[string]any{"authorizationUrl": destination, "state": state}, err
}

func (s *Service) ngpAuthorizationComplete(ctx context.Context, id, userID int64, state, code string) (map[string]any, error) {
	var row repo.Source
	var d SourceDefinition
	var credential Credential
	var pending oauthPending
	err := s.changeNGP(ctx, id, func(source repo.Source, definition SourceDefinition, c *Credential) error {
		if json.Unmarshal([]byte(c.Values["_vendorPending"]), &pending) != nil || pending.UserID != userID || pending.ExpiresAt <= time.Now().Unix() || pending.Configuration != oauthConfiguration(definition) || subtle.ConstantTimeCompare([]byte(pending.State), []byte(state)) != 1 {
			return ErrOAuthState
		}
		row, d, credential = source, definition, *c
		delete(c.Values, "_vendorPending")
		c.Values["_vendorExchange"] = state
		return nil
	})
	if err != nil {
		return nil, err
	}
	form := url.Values{"app_key": {vendorSecret(credential, "appKey")}, "app_secret": {vendorSecret(credential, "appSecret")}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {pending.RedirectURI}}
	response, err := s.vendorRaw(ctx, row, d, credential, "POST", "/oauth2/token", nil, []byte(form.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	if err != nil {
		return nil, err
	}
	obj := vendorObject(response.Data)
	if response.Status != 200 || vendorString(obj["access_token"]) == "" || vendorString(obj["refresh_token"]) == "" {
		return nil, ErrOAuth
	}
	token := vendorToken{SourceType: d.SourceType, Token: vendorString(obj["access_token"]), Refresh: vendorString(obj["refresh_token"]), Expires: time.Now().Unix() + vendorLifetime(obj["expires_in"], 3600), Configuration: oauthConfiguration(d)}
	raw, err := json.Marshal(token)
	if err != nil {
		return nil, err
	}
	err = s.changeNGP(ctx, id, func(_ repo.Source, definition SourceDefinition, c *Credential) error {
		if c.Values["_vendorExchange"] != state || oauthConfiguration(definition) != pending.Configuration {
			return ErrOAuthState
		}
		delete(c.Values, "_vendorExchange")
		c.Values["_vendorToken"] = string(raw)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Source(ctx, id)
}
