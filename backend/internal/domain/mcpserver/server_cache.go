package mcpserver

import (
	"backend/internal/contextx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"sort"
	"time"
)

type cachedServer struct {
	server *mcp.Server
	used   time.Time
}

// Authentication/permission refresh still runs before this factory. Cache keys
// include the entire current subject, so role/scope/grant changes never reuse a
// broader registration. Handlers receive the current request context, never the
// context of the request that built the tool set. Bounded independently of users.
func (s *Service) server(r *http.Request) *mcp.Server {
	if !permitted(r.Context(), "uns.read") {
		return nil
	}
	subject, _ := contextx.SubjectFrom(r.Context())
	subject.MCPScopes = append([]string(nil), subject.MCPScopes...)
	sort.Strings(subject.MCPScopes)
	subject.UIActions = append([]string(nil), subject.UIActions...)
	sort.Strings(subject.UIActions)
	raw, _ := json.Marshal(struct {
		Subject  contextx.Subject
		CodeMode bool
	}{subject, r.URL.Query().Get("codemode") != "false"})
	sum := sha256.Sum256(raw)
	key := hex.EncodeToString(sum[:])
	now := time.Now()
	s.serversMu.Lock()
	defer s.serversMu.Unlock()
	if s.servers == nil {
		s.servers = map[string]cachedServer{}
	}
	if entry, ok := s.servers[key]; ok && now.Sub(entry.used) < 5*time.Minute {
		entry.used = now
		s.servers[key] = entry
		return entry.server
	}
	if len(s.servers) >= 32 {
		oldest := ""
		var age time.Time
		for k, entry := range s.servers {
			if oldest == "" || entry.used.Before(age) {
				oldest = k
				age = entry.used
			}
		}
		delete(s.servers, oldest)
	}
	server := s.buildServer(r)
	s.servers[key] = cachedServer{server: server, used: now}
	return server
}
