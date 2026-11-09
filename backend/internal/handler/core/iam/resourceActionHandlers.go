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

func ResourceActionListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ResourceIdReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		actions, err := svcCtx.App.IAM.ResourceActions(r.Context(), req.ResourceId)
		if err != nil {
			respx.WriteError(w, logicx.Error(err))
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(actions))
	}
}

func ResourceActionCreateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return saveResourceActionHandler(svcCtx)
}

func ResourceActionUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return saveResourceActionHandler(svcCtx)
}

func saveResourceActionHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ResourceActionSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		action, err := svcCtx.App.IAM.SaveResourceAction(r.Context(), repo.ResourceAction{
			ID:          req.ActionId,
			ResourceID:  req.ResourceId,
			ActionType:  req.ActionType,
			ActionValue: req.ActionValue,
			Methods:     req.Methods,
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
		gozerohttpx.OkJson(w, respx.Envelope(action))
	}
}

func ResourceActionDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ResourceActionIdReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		if err := svcCtx.App.IAM.DeleteResourceAction(r.Context(), req.ActionId, logicx.UserID(r.Context())); err != nil {
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
