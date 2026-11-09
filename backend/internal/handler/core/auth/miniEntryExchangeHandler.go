package auth

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/auth"
	"backend/internal/svc"
	"backend/internal/types"
	"fmt"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"mime"
	"net/http"
	"net/url"
)

func MiniEntryExchangeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Only the same-origin HTTPS bridge may redeem. Do not accept cross-origin
		// form submissions even when a deployment has permissive CORS settings.
		origin, err := url.Parse(r.Header.Get("Origin"))
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || origin.Scheme != "https" || origin.Host != r.Host || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || media != "application/json" {
			respx.WriteError(w, respx.NewHTTPError(403, "invalid origin"))
			return
		}
		var req types.MiniEntryExchangeReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(400, "invalid request"))
			return
		}
		grant, expires, err := auth.NewMiniEntryLogic(r.Context(), svcCtx).Exchange(req.Ticket)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		// Existing Enterprise web auth reads edge_session with js-cookie. Preserve
		// that contract; the token is never put in a URL or the JSON response.
		http.SetCookie(w, &http.Cookie{Name: "edge_session", Value: grant.Token, Path: "/", Secure: true, SameSite: http.SameSiteLaxMode, Expires: expires})
		gozerohttpx.OkJson(w, respx.Envelope(map[string]any{"redirect": fmt.Sprintf("/mini-app/%d/%d", grant.ProjectID, grant.AppID)}))
	}
}
