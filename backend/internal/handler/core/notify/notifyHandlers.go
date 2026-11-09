// Thin HTTP adapter maintained alongside the notification API contract.
package notify

import (
	"backend/internal/httpx"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func parse[T any](w http.ResponseWriter, r *http.Request) (*T, bool) {
	var req T
	if err := gozerohttpx.Parse(r, &req); err != nil {
		httpx.WriteError(w, httpx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
		return nil, false
	}
	return &req, true
}
