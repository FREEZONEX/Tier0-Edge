package cluster

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultTopicRoot = "cloudsync"
	usernameSep      = "&"
	edgeNodeFlag     = "edgeNode"

	kindMeta                 = "meta"
	kindData                 = "data"
	kindUNSScope             = "uns-scope"
	kindUserSnapshotRequest  = "user-snapshot/request"
	kindUserSnapshotResponse = "user-snapshot/response"
	kindUserSyncRequest      = "user-sync/request"
	kindUserSyncResult       = "user-sync/result"

	syncModeCloudEdge = "cloudEdge"
	syncModeEdgeEdge  = "edgeEdge"

	cloudSyncMetadataAPIPath        = "api/core/uns/cloud-edge-sync/meta"
	cloudSyncUserSnapshotAPIPath    = "api/core/uns/cloud-edge-sync/user-snapshot"
	localClusterUserSnapshotAPIPath = "api/core/cluster/user-snapshot"

	nodeTypeFolder int32 = 1
	nodeTypeFile   int32 = 2

	topicTypeState  int32 = 1
	topicTypeAction int32 = 2
	topicTypeMetric int32 = 3
)

type fieldDefinition struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Unit string `json:"unit,omitempty"`
}

type metaSnapshot struct {
	Version             int64      `json:"version"`
	EventID             string     `json:"eventID,omitempty"`
	FleetNodeID         string     `json:"fleetNodeID,omitempty"`
	SourceRootNodeID    int64      `json:"sourceRootNodeID,omitempty"`
	ScopeRevision       int64      `json:"scopeRevision,omitempty"`
	Baseline            bool       `json:"baseline,omitempty"`
	Complete            bool       `json:"complete,omitempty"`
	WorkspaceID         int64      `json:"workspaceID,omitempty"`
	MqttAuthID          string     `json:"mqttAuthID,omitempty"`
	DeprecatedClientKey string     `json:"connectClientKey,omitempty"`
	OriginNodeKey       string     `json:"originNodeKey,omitempty"`
	RootTargetPaths     []string   `json:"rootTargetPaths,omitempty"`
	Force               bool       `json:"force,omitempty"`
	Nodes               []metaNode `json:"nodes"`
	EmittedAt           int64      `json:"emittedAt"`
}

type metaNode struct {
	SourceID         int64             `json:"sourceID"`
	SourcePath       string            `json:"sourcePath"`
	TargetPath       string            `json:"targetPath"`
	Name             string            `json:"name"`
	DisplayName      string            `json:"displayName,omitempty"`
	Alias            string            `json:"alias,omitempty"`
	Type             int32             `json:"type"`
	TopicType        int32             `json:"topicType,omitempty"`
	Description      string            `json:"description,omitempty"`
	Fields           []fieldDefinition `json:"fields,omitempty"`
	ExtendProperties map[string]string `json:"extendProperties,omitempty"`
	EnableHistory    bool              `json:"enableHistory,omitempty"`
}

type dataMessage struct {
	Version             int64  `json:"version"`
	FleetNodeID         string `json:"fleetNodeID,omitempty"`
	SourceRootNodeID    int64  `json:"sourceRootNodeID,omitempty"`
	ScopeRevision       int64  `json:"scopeRevision,omitempty"`
	WorkspaceID         int64  `json:"workspaceID,omitempty"`
	MqttAuthID          string `json:"mqttAuthID,omitempty"`
	DeprecatedClientKey string `json:"connectClientKey,omitempty"`
	OriginNodeKey       string `json:"originNodeKey,omitempty"`
	SourcePath          string `json:"sourcePath"`
	TargetPath          string `json:"targetPath"`
	TopicType           int32  `json:"topicType,omitempty"`
	Payload             []byte `json:"payload"`
	EmittedAt           int64  `json:"emittedAt"`
}

func (m metaSnapshot) AuthID() string {
	if id := strings.TrimSpace(m.MqttAuthID); id != "" {
		return id
	}
	return strings.TrimSpace(m.DeprecatedClientKey)
}

func (m dataMessage) AuthID() string {
	if id := strings.TrimSpace(m.MqttAuthID); id != "" {
		return id
	}
	return strings.TrimSpace(m.DeprecatedClientKey)
}

type parsedSyncToken struct {
	Token          string
	ClientID       string
	Username       string
	SignedPassword string
	WorkspaceID    int64
	MqttAuthID     string
	EdgeNodeID     string
	FleetNodeID    string
	EdgeNodeName   string
	SyncMode       string
	HTTPBaseURL    string
	MqttURL        string
	TopicRoot      string
}

type tokenMetadata struct {
	CenterType          string `json:"centerType,omitempty"`
	NetworkBootstrapURL string `json:"networkBootstrapURL,omitempty"`
	Version             int64  `json:"version,omitempty"`
	SyncMode            string `json:"syncMode,omitempty"`
	NodeID              string `json:"nodeID,omitempty"`
	NodeName            string `json:"nodeName,omitempty"`
	Name                string `json:"name,omitempty"`
	ClientID            string `json:"clientID,omitempty"`
	Username            string `json:"username,omitempty"`
	Password            string `json:"password,omitempty"`
	HTTPBaseURL         string `json:"httpBaseURL,omitempty"`
	MqttURL             string `json:"mqttURL,omitempty"`
	MqttBrokers         string `json:"mqttBrokers,omitempty"`
	TopicRoot           string `json:"topicRoot,omitempty"`
}

func parseSyncToken(token string) (*parsedSyncToken, error) {
	raw := strings.TrimSpace(token)
	if raw == "" {
		return nil, ErrInvalid
	}
	authToken, meta := splitTokenMetadata(raw)
	if strings.TrimSpace(meta.ClientID) != "" {
		workspaceID, mqttAuthID, ok := parseTokenClientID(meta.ClientID)
		if !ok {
			return nil, ErrInvalid
		}
		username := strings.TrimSpace(meta.Username)
		if username == "" {
			username = buildEdgeNodeUsername(meta.ClientID)
		}
		password := strings.TrimSpace(meta.Password)
		if password == "" {
			password = tokenPasswordFromRaw(authToken, meta.ClientID)
		}
		return &parsedSyncToken{
			Token:          raw,
			ClientID:       strings.TrimSpace(meta.ClientID),
			Username:       username,
			SignedPassword: password,
			WorkspaceID:    workspaceID,
			MqttAuthID:     mqttAuthID,
			EdgeNodeID:     normalizeEdgeNodeID(meta.NodeID),
			EdgeNodeName:   firstNonEmpty(strings.TrimSpace(meta.NodeName), strings.TrimSpace(meta.Name)),
			SyncMode:       normalizeSyncMode(meta.SyncMode, defaultSyncModeByClientID(meta.ClientID)),
			HTTPBaseURL:    normalizeBaseURL(meta.HTTPBaseURL),
			MqttURL:        normalizeMqttBrokers(firstNonEmpty(meta.MqttURL, meta.MqttBrokers)),
			TopicRoot:      firstNonEmpty(strings.Trim(strings.TrimSpace(meta.TopicRoot), "/"), defaultTopicRoot),
		}, nil
	}
	parts := strings.Split(authToken, "&")
	if len(parts) == 2 {
		mqttAuthID := cleanTopicSegment(parts[0])
		if mqttAuthID == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, ErrInvalid
		}
		return &parsedSyncToken{
			Token:          raw,
			ClientID:       mqttAuthID,
			Username:       buildEdgeNodeUsername(mqttAuthID),
			SignedPassword: strings.TrimSpace(parts[1]),
			MqttAuthID:     mqttAuthID,
			EdgeNodeID:     normalizeEdgeNodeID(meta.NodeID),
			EdgeNodeName:   firstNonEmpty(strings.TrimSpace(meta.NodeName), strings.TrimSpace(meta.Name)),
			SyncMode:       normalizeSyncMode(meta.SyncMode, syncModeEdgeEdge),
			HTTPBaseURL:    normalizeBaseURL(meta.HTTPBaseURL),
			MqttURL:        normalizeMqttBrokers(firstNonEmpty(meta.MqttURL, meta.MqttBrokers)),
			TopicRoot:      firstNonEmpty(strings.Trim(strings.TrimSpace(meta.TopicRoot), "/"), defaultTopicRoot),
		}, nil
	}
	if len(parts) < 3 {
		mqttAuthID := cleanTopicSegment(authToken)
		if mqttAuthID == "" {
			return nil, ErrInvalid
		}
		return &parsedSyncToken{
			Token:        raw,
			ClientID:     mqttAuthID,
			Username:     buildEdgeNodeUsername(mqttAuthID),
			MqttAuthID:   mqttAuthID,
			EdgeNodeID:   normalizeEdgeNodeID(meta.NodeID),
			EdgeNodeName: firstNonEmpty(strings.TrimSpace(meta.NodeName), strings.TrimSpace(meta.Name)),
			SyncMode:     normalizeSyncMode(meta.SyncMode, syncModeEdgeEdge),
			HTTPBaseURL:  normalizeBaseURL(meta.HTTPBaseURL),
			MqttURL:      normalizeMqttBrokers(firstNonEmpty(meta.MqttURL, meta.MqttBrokers)),
			TopicRoot:    firstNonEmpty(strings.Trim(strings.TrimSpace(meta.TopicRoot), "/"), defaultTopicRoot),
		}, nil
	}
	workspaceID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || workspaceID <= 0 {
		return nil, ErrInvalid
	}
	mqttAuthID := strings.TrimSpace(parts[1])
	if mqttAuthID == "" {
		return nil, ErrInvalid
	}
	clientID := fmt.Sprintf("%d&%s", workspaceID, mqttAuthID)
	return &parsedSyncToken{
		Token:          raw,
		ClientID:       clientID,
		Username:       buildEdgeNodeUsername(clientID),
		SignedPassword: strings.Join(parts[2:], "&"),
		WorkspaceID:    workspaceID,
		MqttAuthID:     mqttAuthID,
		EdgeNodeID:     normalizeEdgeNodeID(meta.NodeID),
		EdgeNodeName:   firstNonEmpty(strings.TrimSpace(meta.NodeName), strings.TrimSpace(meta.Name)),
		SyncMode:       normalizeSyncMode(meta.SyncMode, syncModeCloudEdge),
		HTTPBaseURL:    normalizeBaseURL(meta.HTTPBaseURL),
		MqttURL:        normalizeMqttBrokers(firstNonEmpty(meta.MqttURL, meta.MqttBrokers)),
		TopicRoot:      firstNonEmpty(strings.Trim(strings.TrimSpace(meta.TopicRoot), "/"), defaultTopicRoot),
	}, nil
}

func splitTokenMetadata(raw string) (string, tokenMetadata) {
	parts := strings.SplitN(strings.TrimSpace(raw), "#", 2)
	if len(parts) != 2 {
		return strings.TrimSpace(raw), tokenMetadata{}
	}
	authToken := strings.TrimSpace(parts[0])
	payload := strings.TrimSpace(parts[1])
	if payload == "" {
		return authToken, tokenMetadata{}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return authToken, tokenMetadata{}
	}
	var meta tokenMetadata
	if err := json.Unmarshal(decoded, &meta); err != nil {
		return authToken, tokenMetadata{}
	}
	meta.NodeID = strings.TrimSpace(meta.NodeID)
	meta.NodeName = strings.TrimSpace(meta.NodeName)
	meta.Name = strings.TrimSpace(meta.Name)
	meta.ClientID = strings.TrimSpace(meta.ClientID)
	meta.Username = strings.TrimSpace(meta.Username)
	meta.Password = strings.TrimSpace(meta.Password)
	meta.SyncMode = normalizeSyncMode(meta.SyncMode, "")
	meta.HTTPBaseURL = strings.TrimSpace(meta.HTTPBaseURL)
	meta.MqttURL = strings.TrimSpace(meta.MqttURL)
	meta.MqttBrokers = strings.TrimSpace(meta.MqttBrokers)
	meta.TopicRoot = strings.Trim(strings.TrimSpace(meta.TopicRoot), "/")
	return authToken, meta
}

func appendTokenMetadata(raw string, meta tokenMetadata) string {
	authToken := strings.TrimSpace(raw)
	if authToken == "" {
		return authToken
	}
	meta.NodeID = strings.TrimSpace(meta.NodeID)
	meta.NodeName = strings.TrimSpace(meta.NodeName)
	meta.Name = strings.TrimSpace(meta.Name)
	meta.ClientID = strings.TrimSpace(meta.ClientID)
	meta.Username = strings.TrimSpace(meta.Username)
	meta.Password = strings.TrimSpace(meta.Password)
	meta.SyncMode = normalizeSyncMode(meta.SyncMode, "")
	meta.HTTPBaseURL = normalizeBaseURL(meta.HTTPBaseURL)
	meta.MqttURL = normalizeMqttBrokers(firstNonEmpty(meta.MqttURL, meta.MqttBrokers))
	meta.MqttBrokers = normalizeMqttBrokers(meta.MqttBrokers)
	meta.TopicRoot = strings.Trim(strings.TrimSpace(meta.TopicRoot), "/")
	if meta.NodeID == "" && meta.NodeName == "" && meta.Name == "" && meta.ClientID == "" && meta.Username == "" && meta.Password == "" && meta.SyncMode == "" && meta.HTTPBaseURL == "" && meta.MqttURL == "" && meta.MqttBrokers == "" && meta.TopicRoot == "" {
		return authToken
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return authToken
	}
	return authToken + "#" + base64.RawURLEncoding.EncodeToString(payload)
}

func normalizeSyncMode(raw, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "cloudedge", "cloud-edge", "cloud_edge":
		return syncModeCloudEdge
	case "edgeedge", "edge-edge", "edge_edge":
		return syncModeEdgeEdge
	default:
		return fallback
	}
}

func (p *parsedSyncToken) isCloudEdge() bool {
	return p != nil && normalizeSyncMode(p.SyncMode, syncModeCloudEdge) == syncModeCloudEdge
}

func parseTokenClientID(clientID string) (workspaceID int64, mqttAuthID string, ok bool) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return 0, "", false
	}
	parts := strings.Split(clientID, usernameSep)
	if len(parts) >= 2 {
		workspaceID, _ = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		mqttAuthID = cleanTopicSegment(parts[1])
		return workspaceID, mqttAuthID, workspaceID > 0 && mqttAuthID != ""
	}
	mqttAuthID = cleanTopicSegment(clientID)
	return 0, mqttAuthID, mqttAuthID != ""
}

func buildEdgeNodeUsername(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return ""
	}
	return clientID + usernameSep + edgeNodeFlag
}

func defaultSyncModeByClientID(clientID string) string {
	if workspaceID, _, ok := parseTokenClientID(clientID); ok && workspaceID > 0 {
		return syncModeCloudEdge
	}
	return syncModeEdgeEdge
}

func tokenPasswordFromRaw(authToken, clientID string) string {
	authToken = strings.TrimSpace(authToken)
	clientID = strings.TrimSpace(clientID)
	if authToken == "" || clientID == "" {
		return ""
	}
	if authToken == clientID {
		return ""
	}
	if strings.HasPrefix(authToken, clientID+usernameSep) {
		return strings.TrimSpace(strings.TrimPrefix(authToken, clientID+usernameSep))
	}
	return ""
}

func normalizeEdgeNodeID(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if strings.HasPrefix(strings.ToLower(nodeID), "edge.") {
		if rawID := strings.TrimSpace(nodeID[len("edge."):]); rawID != "" {
			return rawID
		}
	}
	return nodeID
}

func buildTopic(root string, mqttAuthID, kind string) string {
	root = strings.Trim(strings.TrimSpace(root), "/")
	if root == "" {
		root = defaultTopicRoot
	}
	return fmt.Sprintf("%s/%s/%s", root, strings.TrimSpace(mqttAuthID), strings.TrimSpace(kind))
}

func buildUserSnapshotResponseTopic(root string, mqttAuthID, requestID string) string {
	requestID = cleanTopicSegment(requestID)
	if requestID == "" {
		return ""
	}
	return buildTopic(root, mqttAuthID, kindUserSnapshotResponse+"/"+requestID)
}

func cleanTopicSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "/") || strings.Contains(raw, "+") || strings.Contains(raw, "#") {
		return ""
	}
	return raw
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.Index(raw, "://"); idx >= 0 {
		// 已有 scheme：保留 scheme://，仅裁剪 host/path 尾部斜杠。
		rest := strings.TrimRight(raw[idx+3:], "/")
		if rest == "" {
			return ""
		}
		return raw[:idx+3] + rest
	}
	return "https://" + strings.TrimRight(raw, "/")
}

func metadataEndpointFromBaseURL(raw string) string {
	return endpointFromBaseURL(raw, cloudSyncMetadataAPIPath)
}

func userSnapshotEndpointFromBaseURL(raw, syncMode string) string {
	if normalizeSyncMode(syncMode, syncModeEdgeEdge) == syncModeCloudEdge {
		return endpointFromBaseURL(raw, cloudSyncUserSnapshotAPIPath)
	}
	return endpointFromBaseURL(raw, localClusterUserSnapshotAPIPath)
}

func endpointFromBaseURL(raw, path string) string {
	baseURL := normalizeBaseURL(raw)
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func normalizeMqttBrokers(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if broker := normalizeMqttBroker(part); broker != "" {
			out = append(out, broker)
		}
	}
	return strings.Join(out, ",")
}

func normalizeMqttBroker(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	if strings.Contains(raw, ":") {
		scheme := "tcp"
		if port := raw[strings.LastIndex(raw, ":")+1:]; port == "8883" {
			scheme = "ssl"
		}
		return scheme + "://" + raw
	}
	return "ssl://" + raw + ":8883"
}

func (p *parsedSyncToken) resolveHTTPMetadataEndpoint() string {
	if p == nil {
		return ""
	}
	if p.isCloudEdge() {
		return metadataEndpointFromBaseURL(p.HTTPBaseURL)
	}
	return endpointFromBaseURL(p.HTTPBaseURL, localClusterUserSnapshotAPIPath)
}

func (p *parsedSyncToken) resolveHTTPUNSScopeEndpoint() string {
	if p == nil || p.isCloudEdge() {
		return ""
	}
	return endpointFromBaseURL(p.HTTPBaseURL, localClusterUserSnapshotAPIPath)
}

func (p *parsedSyncToken) resolveHTTPUserSnapshotEndpoint() string {
	if p == nil {
		return ""
	}
	return userSnapshotEndpointFromBaseURL(p.HTTPBaseURL, p.SyncMode)
}

func (p *parsedSyncToken) resolveMqttBrokers(fallbackBrokers string) string {
	if p == nil {
		return normalizeMqttBrokers(fallbackBrokers)
	}
	if brokers := normalizeMqttBrokers(p.MqttURL); brokers != "" {
		return brokers
	}
	return normalizeMqttBrokers(fallbackBrokers)
}

func cleanPath(p string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}

func joinPath(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := cleanPath(part); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return strings.Join(out, "/")
}

func enumName(topicType int32) string {
	switch topicType {
	case topicTypeState:
		return "State"
	case topicTypeAction:
		return "Action"
	case topicTypeMetric:
		return "Metric"
	default:
		return ""
	}
}

func topicTypeByEnumName(name string) int32 {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "state":
		return topicTypeState
	case "action":
		return topicTypeAction
	case "metric":
		return topicTypeMetric
	default:
		return 0
	}
}

func normalizeEnumSegments(p string) string {
	parts := strings.Split(cleanPath(p), "/")
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if topicType := topicTypeByEnumName(part); topicType != 0 {
			part = enumName(topicType)
		}
		if len(out) > 0 && out[len(out)-1] == part && topicTypeByEnumName(part) != 0 {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

func inferTopicTypeFromPath(p string) int32 {
	parts := strings.Split(cleanPath(p), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if topicType := topicTypeByEnumName(parts[i]); topicType != 0 {
			return topicType
		}
	}
	return 0
}

func ensureFileEnumParent(targetPath string, topicType int32) string {
	targetPath = normalizeEnumSegments(targetPath)
	if targetPath == "" {
		return ""
	}
	enum := enumName(topicType)
	if enum == "" {
		return targetPath
	}
	parts := strings.Split(cleanPath(targetPath), "/")
	if len(parts) == 0 {
		return targetPath
	}
	if len(parts) >= 2 && topicTypeByEnumName(parts[len(parts)-2]) != 0 {
		parts[len(parts)-2] = enum
		return strings.Join(parts, "/")
	}
	parent := strings.Join(parts[:len(parts)-1], "/")
	return joinPath(parent, enum, parts[len(parts)-1])
}

func trimLeadingDuplicateEnum(rootTargetName, suffix string) string {
	rootParts := strings.Split(cleanPath(rootTargetName), "/")
	suffixParts := strings.Split(cleanPath(suffix), "/")
	if len(rootParts) == 0 || len(suffixParts) == 0 {
		return suffix
	}
	rootEnum := topicTypeByEnumName(rootParts[len(rootParts)-1])
	if rootEnum == 0 || rootEnum != topicTypeByEnumName(suffixParts[0]) {
		return suffix
	}
	return strings.Join(suffixParts[1:], "/")
}
