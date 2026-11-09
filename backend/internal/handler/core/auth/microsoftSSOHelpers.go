package auth

import (
	"net/http"
	"net/url"
	"time"

	domainauth "backend/internal/domain/auth"
	authlogic "backend/internal/logic/core/auth"
	"backend/internal/svc"
)

const (
	microsoftLoginPath         = "/tier0-login"
	microsoftStateCookieName   = "microsoft_sso_state"
	microsoftBindingCookieName = "microsoft_sso_binding"
	microsoftSSOPath           = "/api/core/auth/sso/microsoft"
)

func redirectMicrosoftError(w http.ResponseWriter, r *http.Request, err error) {
	query := url.Values{"ssoError": []string{authlogic.MicrosoftBrowserErrorCode(err)}}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, microsoftLoginPath+"?"+query.Encode(), http.StatusFound)
}

func setMicrosoftStateCookie(w http.ResponseWriter, svcCtx *svc.ServiceContext, binding string) {
	http.SetCookie(w, &http.Cookie{
		Name: microsoftStateCookieName, Value: binding, Path: microsoftSSOPath,
		HttpOnly: true, Secure: microsoftCookieSecure(svcCtx), SameSite: http.SameSiteLaxMode,
		MaxAge: int((10 * time.Minute).Seconds()), Expires: time.Now().Add(10 * time.Minute),
	})
}

func microsoftStateBinding(r *http.Request) string {
	cookie, err := r.Cookie(microsoftStateCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func clearMicrosoftStateCookie(w http.ResponseWriter, svcCtx *svc.ServiceContext) {
	http.SetCookie(w, &http.Cookie{
		Name: microsoftStateCookieName, Path: microsoftSSOPath, HttpOnly: true,
		Secure: microsoftCookieSecure(svcCtx), SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func redirectMicrosoftCallback(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext, result domainauth.MicrosoftCallbackResult) {
	w.Header().Set("Cache-Control", "no-store")
	if result.Login != nil {
		expires, _ := time.Parse(time.RFC3339, result.Login.SessionExpireAt)
		http.SetCookie(w, &http.Cookie{
			Name: "edge_session", Value: result.Login.Token, Path: "/", Expires: expires,
			SameSite: http.SameSiteLaxMode, Secure: microsoftCookieSecure(svcCtx),
		})
		redirectURI := result.RedirectURI
		if redirectURI == "" {
			// 交给登录页按用户权限和个人配置解析默认首页，避免把 Operator
			// 固定送到无权限的 /home。显式传入的业务回跳地址仍直接保留。
			redirectURI = microsoftLoginPath
		}
		http.Redirect(w, r, redirectURI, http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: microsoftBindingCookieName, Value: result.PendingBinding, Path: microsoftSSOPath,
		HttpOnly: true, Secure: microsoftCookieSecure(svcCtx), SameSite: http.SameSiteStrictMode,
		MaxAge: int((15 * time.Minute).Seconds()), Expires: time.Now().Add(15 * time.Minute),
	})
	query := url.Values{"ssoTicket": []string{result.PendingTicket}, "ssoProvider": []string{"microsoft"}}
	http.Redirect(w, r, microsoftLoginPath+"?"+query.Encode(), http.StatusFound)
}

func microsoftBrowserBinding(r *http.Request) string {
	cookie, err := r.Cookie(microsoftBindingCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func clearMicrosoftBindingCookie(w http.ResponseWriter, svcCtx *svc.ServiceContext) {
	http.SetCookie(w, &http.Cookie{
		Name: microsoftBindingCookieName, Path: microsoftSSOPath, HttpOnly: true,
		Secure: microsoftCookieSecure(svcCtx), SameSite: http.SameSiteStrictMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func microsoftCookieSecure(svcCtx *svc.ServiceContext) bool {
	redirectURL, _ := url.Parse(svcCtx.Config.MicrosoftSSO.RedirectURL)
	return redirectURL != nil && redirectURL.Scheme == "https"
}
