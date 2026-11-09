package iam

import (
	"errors"
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func RoleCreateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RoleSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		role, err := svcCtx.App.IAM.CreateRole(r.Context(), req.Name, logicx.UserID(r.Context()))
		if err != nil {
			respx.WriteError(w, logicx.Error(err))
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(map[string]any{
			"id":              role.ID,
			"roleId":          role.ID,
			"name":            role.Name,
			"roleName":        role.Name,
			"code":            role.Code,
			"roleCode":        role.Code,
			"defaultHomePage": role.DefaultHomePage,
			"resourceList":    role.ResourceList,
		}))
	}
}

func RoleUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RoleSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		if err := svcCtx.App.IAM.UpdateRole(r.Context(), req.Id, req.Name, roleURIs(req.AllowResourceList), req.DefaultHomePage, logicx.UserID(r.Context())); err != nil {
			if errors.Is(err, repo.ErrSystemReadonly) {
				respx.WriteError(w, respx.NewHTTPError(http.StatusForbidden, "system seed data is read-only"))
				return
			}
			respx.WriteError(w, logicx.Error(err))
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(map[string]any{"updated": true}))
	}
}

func RoleDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RoleSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		if err := svcCtx.App.IAM.DeleteRole(r.Context(), req.Id, logicx.UserID(r.Context())); err != nil {
			if errors.Is(err, repo.ErrSystemReadonly) {
				respx.WriteError(w, respx.NewHTTPError(http.StatusForbidden, "system seed data is read-only"))
				return
			}
			respx.WriteError(w, logicx.Error(err))
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(map[string]any{"deleted": true}))
	}
}

func roleURIs(resources []types.RoleResourceReq) []string {
	out := make([]string, 0, len(resources))
	for _, item := range resources {
		if item.Uri != "" {
			out = append(out, item.Uri)
		}
	}
	return out
}
