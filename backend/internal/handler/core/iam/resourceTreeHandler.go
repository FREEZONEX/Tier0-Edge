// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	respx "backend/internal/httpx"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"

	"backend/internal/logic/core/iam"
	"backend/internal/svc"
)

func ResourceTreeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := iam.NewResourceTreeLogic(r.Context(), svcCtx)
		resp, err := l.ResourceTree()
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
