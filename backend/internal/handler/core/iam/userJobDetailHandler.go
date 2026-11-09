// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package iam

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"backend/internal/infra/eventstream"
	iamlogic "backend/internal/logic/core/iam"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func UserJobDetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UserJobReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		if req.Stream {
			streamUserJobEvents(w, r, svcCtx, req.JobKey)
			return
		}

		l := iamlogic.NewUserJobDetailLogic(r.Context(), svcCtx)
		resp, err := l.UserJobDetail(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func streamUserJobEvents(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext, jobKey string) {
	userID := logicx.UserID(r.Context())
	_, err := svcCtx.App.IAM.GetUserJob(r.Context(), jobKey, userID)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, logicx.Error(err))
		return
	}
	match := func(event eventstream.Event) bool {
		return event.Type == "iam.user.job" && eventstream.ContainsKey(event.Keys, "job:"+jobKey)
	}
	ch, cancel := eventstream.Subscribe(match)
	defer cancel()
	current, err := svcCtx.App.IAM.GetUserJob(r.Context(), jobKey, userID)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, logicx.Error(err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	if userJobTerminal(current) {
		writeUserJobEvent(w, flusher, current)
		return
	}
	_, _ = fmt.Fprint(w, ": connected\n\n")
	if flusher != nil {
		flusher.Flush()
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeUserJobEvent(w, flusher, event.Data)
			if userJobTerminal(event.Data) {
				return
			}
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func writeUserJobEvent(w http.ResponseWriter, flusher http.Flusher, data map[string]any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	if flusher != nil {
		flusher.Flush()
	}
}

func userJobTerminal(data map[string]any) bool {
	status, _ := data["status"].(string)
	return status == "succeeded" || status == "failed" || status == "canceled"
}
