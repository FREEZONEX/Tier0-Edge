package gateway

import "strings"

// Gate both generated APIs (including unauthenticated worker callbacks) and
// saved proxy URLs before dispatch. These services are not initialized.
func retiredComponentPath(path string) bool {
	path = strings.ToLower(strings.TrimRight(path, "/"))
	for _, prefix := range []string{
		"/anchor", "/vision", "/agent-svc", "/aiagent", "/ai-agent",
		"/api/core/anchor", "/api/core/video", "/api/core/vision", "/api/core/ai",
		"/api/internal/ai", "/api/core/projects", "/project",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
