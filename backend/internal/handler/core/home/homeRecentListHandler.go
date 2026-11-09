// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package home

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	respx "backend/internal/httpx"
	"backend/internal/logic/core/home"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func HomeRecentListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HomeRecentReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		isShowInRecent, err := parseIsShowInRecent(r.URL.Query().Get("isShowInRecent"))
		if err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, err.Error()))
			return
		}

		l := home.NewHomeRecentListLogic(r.Context(), svcCtx)
		resp, err := l.HomeRecentList(&req, isShowInRecent)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}

func parseIsShowInRecent(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || (parsed != 1 && parsed != 2) {
		return 0, fmt.Errorf("isShowInRecent must be 1 or 2")
	}
	return parsed, nil
}
