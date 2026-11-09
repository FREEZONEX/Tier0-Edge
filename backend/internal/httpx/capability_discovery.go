package httpx

import (
	"net/http"

	zerohttp "github.com/zeromicro/go-zero/rest/httpx"
)

func IsCapabilityDiscovery(r *http.Request) bool {
	return r.URL.Path == "/openapi/v2/capabilities/discover" || r.URL.Path == "/api/core/v2/capabilities/discover"
}

// Only discovery uses the frozen direct error contract. Other APIs retain their envelopes.
func WriteCapabilityDiscoveryError(w http.ResponseWriter, r *http.Request, status int, code, message string) bool {
	if !IsCapabilityDiscovery(r) {
		return false
	}
	zerohttp.WriteJson(w, status, map[string]string{"errorCode": code, "message": message})
	return true
}
