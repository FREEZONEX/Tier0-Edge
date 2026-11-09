package mqtt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	domaincommon "backend/internal/domain/common"
	"backend/internal/infra/cache"
	"backend/internal/repo"
	"backend/internal/secrets"

	"github.com/zeromicro/go-zero/core/logx"
)

type Service struct {
	clients      *repo.ConnectClientRepo
	apiKeyAuth   apiKeyAuthenticator
	cache        openSessionCache
	emqxURL      string
	httpClient   *http.Client
	sourceAccess SourceAccess
}

type SourceAccess interface {
	AuthenticateMQTTSource(context.Context, string, string, string) (bool, error)
	AuthorizeMQTTSource(context.Context, string, string, string, string) (bool, error)
}

// Startup-only wiring: Source auth remains fail-closed when not installed.
func (s *Service) SetSourceAccess(access SourceAccess) { s.sourceAccess = access }

type ClientCommand struct {
	ID                          int64
	Name                        string
	Type                        string
	Description                 string
	UserID                      int64
	ClientIDRandomSuffixEnabled bool
}

type ClientCredential struct {
	ID                          int64
	Name                        string
	ClientID                    string
	Username                    string
	Password                    string
	ClientIDRandomSuffixEnabled bool
}

func New(ctx context.Context, apiKeyAuth apiKeyAuthenticator, cacheClient *cache.Client, emqxURL string) *Service {
	return &Service{
		clients:    repo.NewConnectClientRepo(ctx),
		apiKeyAuth: apiKeyAuth,
		cache:      cacheClient,
		emqxURL:    strings.TrimRight(strings.TrimSpace(emqxURL), "/"),
		httpClient: &http.Client{Timeout: 3 * time.Second},
	}
}

func (s *Service) Auth(ctx context.Context, username, password, clientID, peerHost string) (map[string]any, error) {
	if strings.HasPrefix(username, "source:") {
		allow := false
		var err error
		if s.sourceAccess != nil {
			allow, err = s.sourceAccess.AuthenticateMQTTSource(ctx, username, password, clientID)
		}
		return map[string]any{"allow": allow, "result": allowResult(allow), "superuser": false}, err
	}
	if allowInternalMqttUser(username, password) {
		return map[string]any{"allow": true, "result": "allow", "superuser": true}, nil
	}
	if workspaceID, isOpen := parseOpenUsername(username); isOpen {
		allow := false
		if isLegacyOpenUsername(username) {
			allow = s.authenticateLegacyOpenAPIKey(ctx, clientID, password)
		} else {
			allow = s.authenticateOpenAPIKey(ctx, workspaceID, clientID, password)
		}
		return map[string]any{"allow": allow, "result": allowResult(allow)}, nil
	}
	_, allow, err := s.clients.FindAuthorizedMqttClient(ctx, username, password, clientID)
	if err != nil {
		return nil, err
	}
	if allow {
		_ = s.clients.MarkConnectClientConnected(ctx, username, clientID, peerHost)
	}
	return map[string]any{"allow": allow, "result": allowResult(allow)}, nil
}

func (s *Service) ACL(ctx context.Context, username, clientID, topic, action string) (map[string]any, error) {
	if strings.HasPrefix(username, "source:") {
		allow := false
		var err error
		if s.sourceAccess != nil {
			allow, err = s.sourceAccess.AuthorizeMQTTSource(ctx, username, clientID, topic, action)
		}
		return map[string]any{"allow": allow, "result": allowResult(allow), "action": action}, err
	}
	// Neither an API key nor a legacy client-ID shortcut grants a Source's
	// reserved namespace. MQTT #/+ already exclude dollar-prefixed namespaces.
	if reservedSourceTopic(topic) && !allowInternalMqttUsername(username) {
		return map[string]any{"allow": false, "result": "deny", "action": action}, nil
	}
	allow := false
	if workspaceID, isOpen := parseOpenUsername(username); isOpen {
		if isLegacyOpenUsername(username) {
			allow = s.allowLegacyOpenAPIAccess(ctx, clientID, action)
		} else {
			allow = s.allowOpenAPIAccess(ctx, workspaceID, clientID, action)
		}
	} else if allowInternalMqttUsername(username) || allowInternalMqttClientID(clientID) {
		allow = true
	} else if (strings.TrimSpace(username) != "" || strings.TrimSpace(clientID) != "") && strings.TrimSpace(topic) != "" {
		client, ok, err := s.clients.FindMqttClientByIdentity(ctx, username, clientID)
		if err != nil {
			return nil, err
		}
		if ok {
			allow = true
			if client.Type == repo.ConnectClientTypeEdge {
				allow = edgeNodeTopicAllowed(client.ID, topic, action)
				if allow {
					_ = s.clients.MarkConnectClientConnected(ctx, username, clientID, "")
				}
			}
		}
	}
	if strings.HasPrefix(topic, "$SYS/") {
		allow = false
	}
	return map[string]any{"allow": allow, "result": allowResult(allow), "action": action}, nil
}

func reservedSourceTopic(topic string) bool {
	if strings.HasPrefix(topic, "$share/") {
		parts := strings.SplitN(topic, "/", 3)
		if len(parts) == 3 {
			topic = parts[2]
		}
	} else {
		topic = strings.TrimPrefix(topic, "$queue/")
	}
	return topic == "$tier0" || strings.HasPrefix(topic, "$tier0/")
}

func edgeNodeTopicAllowed(authID int64, topic, action string) bool {
	if authID == 0 {
		return false
	}
	topic = strings.Trim(strings.TrimSpace(topic), "/")
	authKey := strconv.FormatInt(authID, 10)
	action = strings.ToLower(strings.TrimSpace(action))
	prefix := "fleet/v1/" + authKey + "/"
	if !strings.HasPrefix(topic, prefix) {
		return false
	}
	kind := strings.TrimPrefix(topic, prefix)
	switch action {
	case "publish":
		switch kind {
		case "hello", "heartbeat", "snapshot", "commandAck", "commandResult", "disconnect":
			return true
		}
	case "subscribe":
		switch kind {
		case "helloAck", "error", "snapshotRequest", "command", "disconnect":
			return true
		}
	}
	return false
}

func (s *Service) ListClients(ctx context.Context, clientType, keyword string) (map[string]any, error) {
	items, err := s.clients.ListConnectClients(ctx, repo.ConnectClientFilter{Type: clientTypeValue(clientType, true), Keyword: keyword})
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(items))
	for _, item := range items {
		list = append(list, clientResp(item))
	}
	return map[string]any{"list": list, "data": list, "total": len(list)}, nil
}

func (s *Service) ExternalConnectedCount(ctx context.Context) (int64, error) {
	return s.clients.CountConnectedExternalClients(ctx)
}

func (s *Service) GenerateClient(ctx context.Context, cmd ClientCommand) (map[string]any, error) {
	item, err := s.createClient(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return clientResp(item), nil
}

func (s *Service) GenerateDeviceClient(ctx context.Context, cmd ClientCommand) (ClientCredential, error) {
	cmd.Type = ""
	item, err := s.createClient(ctx, cmd)
	if err != nil {
		return ClientCredential{}, err
	}
	return ClientCredential{
		ID:                          item.ID,
		Name:                        item.Name,
		ClientID:                    item.ClientID,
		Username:                    item.Username,
		Password:                    item.Password,
		ClientIDRandomSuffixEnabled: item.ClientIDRandomSuffixEnabled,
	}, nil
}

func (s *Service) createClient(ctx context.Context, cmd ClientCommand) (repo.ConnectClient, error) {
	name := strings.TrimSpace(cmd.Name)
	description := strings.TrimSpace(cmd.Description)
	if name == "" ||
		domaincommon.ExceedMaxLength(name, domaincommon.ConnectionNameMaxLen) ||
		domaincommon.ExceedDescLimit(description) {
		return repo.ConnectClient{}, ErrInvalid
	}
	req := repo.ConnectClient{
		Type:                        creatableClientTypeValue(cmd.Type),
		Name:                        name,
		Description:                 description,
		ClientIDRandomSuffixEnabled: cmd.ClientIDRandomSuffixEnabled,
	}
	req.CreatedBy = cmd.UserID
	item, err := s.clients.CreateConnectClient(ctx, req)
	if err != nil {
		return repo.ConnectClient{}, err
	}
	return item, nil
}

func (s *Service) ResetPassword(ctx context.Context, id, userID int64) (map[string]any, error) {
	item, err := s.clients.ResetConnectClientPassword(ctx, id, userID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return clientResp(item), nil
}

func (s *Service) SetAuth(ctx context.Context, id, userID int64, enabled bool) (map[string]any, error) {
	status := repo.AuthStatusDisabled
	if enabled {
		status = repo.AuthStatusActive
	}
	item, err := s.clients.SetConnectClientAuthStatus(ctx, id, userID, status)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return clientResp(item), nil
}

func (s *Service) DeleteClient(ctx context.Context, id, userID int64) (map[string]any, error) {
	if err := s.clients.DeleteConnectClient(ctx, id, userID); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

func (s *Service) DeleteDeviceClient(ctx context.Context, id, userID int64) (bool, error) {
	if id <= 0 {
		return false, ErrInvalid
	}
	item, deleted, err := s.clients.DeleteConnectClientByType(ctx, id, userID, repo.ConnectClientTypeDevice)
	if err != nil {
		return false, err
	}
	if !deleted {
		return true, nil
	}
	s.kickClient(ctx, item.ClientID)
	return true, nil
}

func (s *Service) kickClient(ctx context.Context, clientID string) {
	clientID = strings.TrimSpace(clientID)
	if s == nil || s.emqxURL == "" || clientID == "" || s.httpClient == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.emqxURL+"/api/v5/clients/"+url.PathEscape(clientID), nil)
	if err != nil {
		logx.WithContext(ctx).Errorf("build emqx kick request clientID=%s: %v", clientID, err)
		return
	}
	apiKey, apiSecret := secrets.EMQXAPIKey()
	req.SetBasicAuth(apiKey, apiSecret)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		logx.WithContext(ctx).Errorf("kick emqx client clientID=%s: %v", clientID, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		logx.WithContext(ctx).Errorf("kick emqx client clientID=%s status=%d", clientID, resp.StatusCode)
	}
}

func allowResult(allow bool) string {
	if allow {
		return "allow"
	}
	return "deny"
}

func allowInternalMqttUser(username, password string) bool {
	return secrets.AllowInternalMQTTUser(username, password)
}

func allowInternalMqttUsername(username string) bool {
	switch strings.TrimSpace(username) {
	case "backend", "sourceflow", "eventflow":
		return true
	default:
		return false
	}
}

func allowInternalMqttClientID(clientID string) bool {
	clientID = strings.TrimSpace(clientID)
	for _, prefix := range []string{"node-red-", "eventflow-", "nodered-", "edge_server_"} {
		if strings.HasPrefix(clientID, prefix) {
			return true
		}
	}
	return false
}

func clientResp(item repo.ConnectClient) map[string]any {
	return map[string]any{
		"id":                          item.ID,
		"name":                        item.Name,
		"connectStatus":               item.ConnectStatus,
		"authStatus":                  item.AuthStatus,
		"description":                 item.Description,
		"password":                    item.Password,
		"clientId":                    item.ClientID,
		"username":                    item.Username,
		"type":                        clientTypeName(item.Type),
		"typeCode":                    item.Type,
		"ip":                          item.IP,
		"connectedTime":               item.ConnectedTime,
		"clientIdRandomSuffixEnabled": item.ClientIDRandomSuffixEnabled,
		"createdTime":                 item.CreatedTime,
		"updatedTime":                 item.UpdatedTime,
	}
}

func clientTypeValue(value string, allowEmpty bool) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		if allowEmpty {
			return repo.ConnectClientTypeDevice
		}
		return repo.ConnectClientTypeDevice
	case "all", "*":
		if allowEmpty {
			return 0
		}
		return repo.ConnectClientTypeDevice
	case "device", "default", "1":
		return repo.ConnectClientTypeDevice
	case "flow", "2":
		return repo.ConnectClientTypeFlow
	case "edge", "edgeNode", "edgenode", "3":
		return repo.ConnectClientTypeEdge
	case "system", "4":
		return repo.ConnectClientTypeSystem
	default:
		return repo.ConnectClientTypeDevice
	}
}

func creatableClientTypeValue(value string) int {
	if clientTypeValue(value, false) == repo.ConnectClientTypeSystem {
		return repo.ConnectClientTypeSystem
	}
	return repo.ConnectClientTypeDevice
}

func clientTypeName(value int) string {
	switch value {
	case repo.ConnectClientTypeFlow:
		return "flow"
	case repo.ConnectClientTypeEdge:
		return "edgeNode"
	case repo.ConnectClientTypeSystem:
		return "system"
	default:
		return "device"
	}
}

func normalizeNotFound(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

var (
	ErrInvalid  = errors.New("invalid mqtt client")
	ErrNotFound = errors.New("mqtt client not found")
)
