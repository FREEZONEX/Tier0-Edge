package iam

import (
	"errors"
	"net/http"

	domainiam "backend/internal/domain/iam"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func ResourceCreateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return saveResourceHandler(svcCtx)
}

func ResourceUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return saveResourceHandler(svcCtx)
}

func saveResourceHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ResourceSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		resource, err := svcCtx.App.IAM.SaveResource(r.Context(), domainiam.ResourceSaveCommand{
			ResourceID:  req.ResourceId,
			ParentID:    req.ParentId,
			ResourceKey: req.ResourceKey,
			Name:        req.Name,
			Type:        req.ResourceType,
			RoutePath:   req.RoutePath,
			UrlType:     req.UrlType,
			OpenType:    req.OpenType,
			Icon:        req.Icon,
			Sort:        req.Sort,
			Enabled:     req.Enabled,
		}, logicx.UserID(r.Context()))
		if err != nil {
			if errors.Is(err, repo.ErrSystemReadonly) {
				respx.WriteError(w, respx.NewHTTPError(http.StatusForbidden, "system seed data is read-only"))
				return
			}
			respx.WriteError(w, logicx.Error(err))
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(resource))
	}
}
