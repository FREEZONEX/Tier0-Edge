package auth

import (
	"net/http"

	"backend/internal/svc"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

// 每个提供方封装自己的 OIDC、Cookie、注册与绑定逻辑；公共路由只分发已启用的适配器。
type externalSSOAdapter struct {
	enabled                                            bool
	start, callback, pending, username, complete, link http.HandlerFunc
}

func externalSSOHandler(svcCtx *svc.ServiceContext, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if svcCtx == nil || !svcCtx.Config.ExternalLogin.Enabled || svcCtx.App == nil || svcCtx.App.Auth == nil {
			http.NotFound(w, r)
			return
		}
		adapters := map[string]externalSSOAdapter{
			"microsoft": {
				enabled: svcCtx.App.Auth.MicrosoftSSOEnabled(),
				start:   MicrosoftSSOStartHandler(svcCtx), callback: MicrosoftSSOCallbackHandler(svcCtx),
				pending: MicrosoftSSOPendingHandler(svcCtx), username: MicrosoftSSOUsernameAvailabilityHandler(svcCtx),
				complete: MicrosoftSSOCompleteHandler(svcCtx), link: MicrosoftSSOLinkHandler(svcCtx),
			},
		}
		adapter, ok := adapters[pathvar.Vars(r)["provider"]]
		if !ok || !adapter.enabled {
			http.NotFound(w, r)
			return
		}
		handler := map[string]http.HandlerFunc{
			"start": adapter.start, "callback": adapter.callback, "pending": adapter.pending,
			"username": adapter.username, "complete": adapter.complete, "link": adapter.link,
		}[action]
		if handler == nil {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	}
}

func ExternalSSOStartHandler(c *svc.ServiceContext) http.HandlerFunc {
	return externalSSOHandler(c, "start")
}
func ExternalSSOCallbackHandler(c *svc.ServiceContext) http.HandlerFunc {
	return externalSSOHandler(c, "callback")
}
func ExternalSSOPendingHandler(c *svc.ServiceContext) http.HandlerFunc {
	return externalSSOHandler(c, "pending")
}
func ExternalSSOUsernameAvailabilityHandler(c *svc.ServiceContext) http.HandlerFunc {
	return externalSSOHandler(c, "username")
}
func ExternalSSOCompleteHandler(c *svc.ServiceContext) http.HandlerFunc {
	return externalSSOHandler(c, "complete")
}
func ExternalSSOLinkHandler(c *svc.ServiceContext) http.HandlerFunc {
	return externalSSOHandler(c, "link")
}
