package mqtt

import (
	"net/http"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func writeMqttDecision(w http.ResponseWriter, data any) {
	decision, _ := data.(map[string]any)
	allow, _ := decision["allow"].(bool)
	result, _ := decision["result"].(string)
	if result == "" {
		if allow {
			result = "allow"
		} else {
			result = "deny"
		}
	}
	resp := map[string]any{"result": result}
	if allow {
		superuser, _ := decision["superuser"].(bool)
		resp["is_superuser"] = superuser
		gozerohttpx.OkJson(w, resp)
		return
	}
	gozerohttpx.WriteJson(w, http.StatusForbidden, resp)
}
