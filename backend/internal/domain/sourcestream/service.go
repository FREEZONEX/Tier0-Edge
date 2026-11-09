package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/sourceresource"
	"backend/internal/streamexec"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrUnavailable = errors.New("SourceStream.unavailable")
var ErrForbidden = errors.New("SourceStream.forbidden")
var ErrReferenced = errors.New("SourceStream.referenced")
var ErrUnknownResult = errors.New("SourceStream.resultUnknown")
var ErrExecutionFailed = errors.New("SourceStream.executionFailed")
var ErrControlInvalidInput = errors.New("CONTROL_INVALID_INPUT")
var ErrControlOutputInvalid = errors.New("CONTROL_OUTPUT_INVALID")
var ErrRemoteControlDisabled = errors.New("EDGE_CONTROL_DISABLED")

var ErrControlInactive = errors.New("CONTROL_INACTIVE")
var ErrIdempotencyConflict = errors.New("IDEMPOTENCY_CONFLICT")

type NativeResource = sourceresource.NativeResource

type ScriptResult struct {
	Data       any           `json:"data"`
	Outputs    []TopicOutput `json:"outputs"`
	Drop       bool          `json:"drop"`
	Logs       []ScriptLog   `json:"logs"`
	DurationMs int64         `json:"durationMs"`
}
type TopicOutput struct {
	TopicID int64 `json:"topicId"`
	Data    any   `json:"data"`
}

// OutputTargets checks all destinations before any write. Individual payload
// validation and idempotent commits then happen at each Topic's sink boundary.
func (d StreamDefinition) OutputTargets(result ScriptResult) ([]TopicOutput, error) {
	if d.MetadataTarget != nil {
		if len(result.Outputs) > 0 {
			return nil, invalid("outputs", "metadataTargetExclusive")
		}
		return nil, nil
	}
	if d.Model != Publish {
		if result.Outputs != nil {
			return nil, invalid("outputs", "publishOnly")
		}
		return nil, nil
	}
	if result.Outputs == nil {
		targets := make([]TopicOutput, 0, len(d.TopicIDs))
		for _, id := range d.TopicIDs {
			targets = append(targets, TopicOutput{TopicID: id, Data: result.Data})
		}
		return targets, nil
	}
	if result.Drop && len(result.Outputs) != 0 {
		return nil, invalid("outputs", "publishOnly")
	}
	allowed := map[int64]bool{}
	for _, id := range d.TopicIDs {
		allowed[id] = true
	}
	seen := map[int64]bool{}
	for _, output := range result.Outputs {
		if !allowed[output.TopicID] || seen[output.TopicID] {
			return nil, invalid("outputs.topicId", "unknownOrDuplicate")
		}
		seen[output.TopicID] = true
	}
	return result.Outputs, nil
}

type ScriptLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}
type ScriptExecutor interface {
	Check(context.Context, ScriptDefinition) error
	Transform(context.Context, ScriptDefinition, any, map[string]any) (ScriptResult, error)
}
type ProtocolRuntime interface {
	Probe(context.Context, int64, RuntimeSource, Credential) error
	Discover(context.Context, int64, RuntimeSource, Credential, string) ([]NativeResource, error)
	Invoke(context.Context, int64, RuntimeSource, Credential, string, any) (any, error)
}
type Service struct {
	impactOnce           sync.Once
	impactKey            []byte
	impactErr            error
	piRetryAfter         sync.Map // Source ID -> upstream Retry-After deadline
	httpClient           *http.Client
	httpManager          *HTTPClientManager
	httpServers          *HTTPServerManager
	runtime              ProtocolRuntime
	executor             ScriptExecutor
	cipher               *CredentialCipher
	asyncReadyMu         sync.RWMutex
	asyncReady           func() bool
	watchState           func(WatchQuery) string
	programReader        func(context.Context, int64) (json.RawMessage, error)
	programSink          StreamSink
	controlAccess        func(context.Context, string) error
	requestAccess        func(context.Context, string, []string, []repo.ProgramRequestTarget) error
	requestSubject       func(context.Context, contextx.Subject) (contextx.Subject, error)
	programAuthority     func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error)
	remoteControlAccess  func() error
	programExecution     programExecutionLifecycle
	executionEvidence    executionEvidence
	subscriptionMu       sync.Mutex
	subscriptions        map[string]*edgeSubscription
	openProtocolMu       sync.Mutex
	openProtocolSessions map[string]*openProtocolSession
	openProtocolClosed   bool
}

// 启动时接入统一 App 授权；脚本和协议层不自行维护 App 权限副本。
func (s *Service) SetControlAuthorizer(check func(context.Context, string) error) {
	s.controlAccess = check
}

func (s *Service) SetRequestAuthorizer(check func(context.Context, string, []string, []repo.ProgramRequestTarget) error, refresh func(context.Context, contextx.Subject) (contextx.Subject, error)) {
	s.requestAccess = check
	s.requestSubject = refresh
}

// Set once at bootstrap; publications store identities, never cached rights.
func (s *Service) SetProgramAuthorityResolver(resolve func(context.Context, repo.ProgramAuthorityRef) (contextx.Subject, error)) {
	s.programAuthority = resolve
}

// 远程 Control 总授权只在新调用准入时检查，不终止已经执行的动作。
func (s *Service) SetRemoteControlAccess(check func() error) { s.remoteControlAccess = check }

func (s *Service) checkRemoteControlAccess() error {
	if s.remoteControlAccess == nil {
		return ErrRemoteControlDisabled
	}
	return s.remoteControlAccess()
}

// Set once at bootstrap, before serving requests.
func (s *Service) SetAsyncReady(ready func() bool) {
	s.asyncReadyMu.Lock()
	s.asyncReady = ready
	s.asyncReadyMu.Unlock()
}

func (s *Service) SetWatchState(state func(WatchQuery) string) {
	if state == nil {
		s.watchState = nil
		return
	}
	s.watchState = func(query WatchQuery) string {
		switch observed := state(query); observed {
		case "ready", "starting", "source-watch-failed":
			return observed
		default:
			return "starting"
		}
	}
}

// The platform supplies data access; scripts receive only the bounded SDK.
func (s *Service) SetProgramUNS(reader func(context.Context, int64) (json.RawMessage, error), sink StreamSink) {
	s.programReader, s.programSink = reader, sink
}

func (s *Service) releaseSource(id int64) {
	s.closeOpenProtocolSource(id)
	s.closeSalesforceSource(id)
	if s.httpManager != nil {
		s.httpManager.CloseSource(id)
	}
	if s.httpServers != nil {
		s.httpServers.CloseSource(id)
	}
	if lifecycle, ok := s.runtime.(interface{ ReleaseSource(int64) }); ok {
		lifecycle.ReleaseSource(id)
	}
}

func (s *Service) Close() {
	s.closeOpenProtocol()
	s.closeSalesforce()
	s.subscriptionMu.Lock()
	for _, subscription := range s.subscriptions {
		subscription.cancel()
	}
	s.subscriptionMu.Unlock()
	s.closeProgramExecutions()
	if s.httpManager != nil {
		s.httpManager.Close()
	}
	if s.httpServers != nil {
		s.httpServers.Close()
	}
	if lifecycle, ok := s.runtime.(interface{ Close() }); ok {
		lifecycle.Close()
	}
}

func New(runtime ProtocolRuntime, executor ScriptExecutor, credentialKey string) *Service {
	c, _ := NewCredentialCipher(credentialKey)
	return &Service{runtime: runtime, executor: executor, cipher: c, httpClient: sourceHTTPClient(), httpManager: NewHTTPClientManager(), httpServers: NewHTTPServerManager()}
}

// Documents use a single contract; vendor fields belong in extensions.
func parseDocument[T any](input any) (T, string, error) {
	var result T
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 1024*1024 || string(raw) == "null" {
		return result, "", invalid("document", "invalidJSON")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return result, "", invalid("document", "objectRequired")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			return result, "", validation
		}
		return result, "", invalid("document", "invalidType")
	}
	if _, source := any(result).(SourceDefinition); source {
		var document any
		if json.Unmarshal(raw, &document) != nil || containsSecret(document) {
			return result, "", invalid("document", "credentialReferenceRequired")
		}
	}
	return result, string(raw), nil
}

func containsSecret(value any) bool {
	switch object := value.(type) {
	case map[string]any:
		for key, child := range object {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			switch normalized {
			case "password", "passwd", "secret", "clientsecret", "token", "accesstoken", "refreshtoken", "privatekey", "apikey", "authorization":
				return true
			}
			if containsSecret(child) {
				return true
			}
		}
	case []any:
		for _, child := range object {
			if containsSecret(child) {
				return true
			}
		}
	}
	return false
}
func sourceView(item repo.Source) map[string]any {
	var document any
	_ = json.Unmarshal([]byte(item.Document), &document)
	resources := []NativeResource{}
	_ = json.Unmarshal([]byte(item.Resources), &resources)
	view := map[string]any{"id": item.ID, "name": item.Name, "protocol": item.Protocol, "document": document, "revision": item.Revision, "configRevision": item.ConfigRevision, "health": item.Health, "healthMessage": item.HealthMessage, "resources": resources, "checkedAt": item.CheckedAt, "lastDataAt": item.LastDataAt, "createdAt": item.CreatedAt, "updatedAt": item.UpdatedAt}
	view["desiredState"], view["runtimeState"] = item.DesiredState, item.DesiredState
	if item.ConnectionCheck != nil {
		view["connectionCheck"] = item.ConnectionCheck
	}
	if item.DataCheck != nil {
		view["dataCheck"] = item.DataCheck
	}
	var def SourceDefinition
	if json.Unmarshal([]byte(item.Document), &def) == nil && def.Protocol == MQTT && def.ConnectionRole() == ServerRole && def.CredentialRef != "" {
		view["mqttServer"] = map[string]any{
			"topicPrefix":    MQTTServerPrefix(item.ID, def.CredentialRef),
			"usernamePrefix": fmt.Sprintf("source:%d:%s:", item.ID, def.CredentialRef),
			"clientIdPrefix": MQTTServerClientPrefix(item.ID, def.CredentialRef),
			"webSocketPath":  "/mqtt",
		}
	}
	if def.Protocol == OPCUA && def.ConnectionRole() == ServerRole && def.CredentialRef != "" {
		view["opcuaServer"] = map[string]any{
			"namespaceUri":   "urn:tier0:source-resources",
			"nodeIdPrefix":   fmt.Sprintf("source/%d/%s/", item.ID, def.CredentialRef),
			"usernamePrefix": fmt.Sprintf("source:%d:%s:", item.ID, def.CredentialRef),
			"access":         "private-listener",
		}
	}
	return view
}
func streamView(item repo.Stream) map[string]any {
	var document any
	var def StreamDefinition
	if decodeStoredProgram([]byte(item.Document), &def) == nil {
		document = def
	} else {
		_ = json.Unmarshal([]byte(item.Document), &document)
	}
	return map[string]any{"id": item.ID, "sourceId": item.SourceID, "name": item.Name, "model": item.Model, "document": document, "revision": item.Revision, "activeVersion": item.ActiveVersion, "status": item.Status, "createdAt": item.CreatedAt, "updatedAt": item.UpdatedAt}
}

// Display metadata never changes the connection identity. Compare full JSON
// rather than typed definitions so unknown vendor settings remain protected.
func sameSourceConfiguration(before, after string) bool {
	var a, b map[string]any
	if json.Unmarshal([]byte(before), &a) != nil || json.Unmarshal([]byte(after), &b) != nil {
		return false
	}
	for _, key := range []string{"name", "description"} {
		delete(a, key)
		delete(b, key)
	}
	return reflect.DeepEqual(a, b)
}

func (s *Service) executionView(ctx context.Context, item repo.Stream) (map[string]any, error) {
	if item.Model == string(ScriptMode) {
		return scriptExecutionView(ctx, item)
	}
	if item.Model == string(ProgramAccess) {
		return s.programV2View(ctx, item)
	}
	view := streamView(item)
	state := item.Status
	if state == "enabled" {
		r := repo.NewSourceStreamRepo(ctx)
		if item.SourceID == 0 {
			version, err := r.Version(ctx, item.ID, item.ActiveVersion)
			if err != nil {
				return nil, err
			}
			snapshots, err := version.Sources()
			if err != nil {
				return nil, err
			}
			state = "ready"
			view["dependencies"] = snapshots
			for _, snapshot := range snapshots {
				source, err := r.Source(ctx, snapshot.SourceID)
				if repo.IsSourceStreamMissing(err) {
					state = "needs-redeploy"
					break
				}
				if err != nil {
					return nil, err
				}
				if source.ConfigRevision != snapshot.ConfigRevision {
					state = "needs-redeploy"
					break
				}
				if source.DesiredState == "disabled" {
					state = "source-disabled"
					break
				}
				if source.Health != "healthy" && state == "ready" {
					state = "source-unhealthy"
				}
			}
			view["executionState"] = state
			return s.observedExecutionView(ctx, item, view)
		}
		source, err := r.Source(ctx, item.SourceID)
		if err != nil {
			return nil, err
		}
		version, err := r.Version(ctx, item.ID, item.ActiveVersion)
		if err != nil {
			return nil, err
		}
		state = "ready"
		var activeDocument StreamDefinition
		if err := json.Unmarshal([]byte(version.Document), &activeDocument); err != nil {
			return nil, err
		}
		view["activeDocument"] = activeDocument
		switch {
		case source.ConfigRevision != version.SourceRevision:
			state = "needs-redeploy"
		case source.DesiredState == "disabled":
			state = "source-disabled"
		case source.Health != "healthy":
			state = "source-unhealthy"
		}
	}
	view["executionState"] = state
	return s.observedExecutionView(ctx, item, view)
}

func (s *Service) observedExecutionView(ctx context.Context, item repo.Stream, view map[string]any) (map[string]any, error) {
	if view["executionState"] != "ready" {
		return view, nil
	}
	r := repo.NewSourceStreamRepo(ctx)
	version, err := r.Version(ctx, item.ID, item.ActiveVersion)
	if err != nil {
		return nil, err
	}
	query, watched, err := watchQuery(version)
	if err != nil {
		return nil, err
	}
	if !watched {
		return view, nil
	}
	if item.SourceID > 0 {
		source, err := r.Source(ctx, item.SourceID)
		if err != nil {
			return nil, err
		}
		var def SourceDefinition
		if json.Unmarshal([]byte(source.Document), &def) != nil {
			return nil, repo.ErrInvalidArgument
		}
		if def.MQTT != nil {
			query.QoS = def.MQTT.QoS
		}
	}
	state := "starting"
	if s.watchState != nil {
		switch observed := s.watchState(query); observed {
		case "ready", "starting", "source-watch-failed":
			state = observed
		}
	}
	view["executionState"] = state
	return view, nil
}
func (s *Service) Sources(ctx context.Context, f repo.SourceFilter) (map[string]any, error) {
	items, total, err := repo.NewSourceStreamRepo(ctx).ListSources(ctx, f)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, s.sourceHTTPView(item))
	}
	return map[string]any{"list": views, "total": total}, nil
}

func (s *Service) sourceHTTPView(item repo.Source) map[string]any {
	view := sourceView(item)
	var def SourceDefinition
	if json.Unmarshal([]byte(item.Document), &def) == nil && def.Protocol == HTTP && def.ConnectionRole() == ServerRole && def.HTTP != nil {
		view["httpServer"] = s.httpServers.View(item.ID, def.HTTP.Port)
	}
	return view
}
func (s *Service) Source(ctx context.Context, id int64) (map[string]any, error) {
	item, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return nil, err
	}
	view := s.sourceHTTPView(item)
	if item.Protocol == string(HTTP) {
		var def SourceDefinition
		if json.Unmarshal([]byte(item.Document), &def) == nil && def.CredentialRef != "" {
			stored, err := repo.NewSourceStreamRepo(ctx).Credential(ctx, item.ID, def.CredentialRef)
			if err != nil {
				return nil, err
			}
			credential, err := s.cipher.Open(item.ID, stored.Ciphertext)
			if err != nil {
				return nil, err
			}
			keys := make([]string, 0, len(credential.Values))
			for key := range credential.Values {
				if !strings.HasPrefix(key, "_vendor") {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			view["credentialKeys"] = keys
			if def.SourceType == "ngp" {
				view["oauthStatus"] = "required"
				var token vendorToken
				if json.Unmarshal([]byte(credential.Values["_vendorToken"]), &token) == nil && token.Configuration == oauthConfiguration(def) && token.Token != "" && (token.Expires > time.Now().Unix() || token.Refresh != "") {
					view["oauthStatus"] = "authorized"
				}
			}
		}
	}
	if item.Protocol == string(OAuth) {
		view["oauthStatus"] = "required"
		d, c, _, err := s.connection(ctx, id, nil)
		if err == nil && c.OAuth != nil && c.OAuth.Configuration == oauthConfiguration(d) && c.OAuth.AccessToken != "" {
			view["oauthStatus"] = "authorized"
			if c.OAuth.ExpiresAt > 0 && c.OAuth.ExpiresAt <= time.Now().Unix() && c.OAuth.RefreshToken == "" {
				view["oauthStatus"] = "required"
			}
		}
	}
	return view, nil
}

func (s *Service) CreateSource(ctx context.Context, input any, revision int64) (map[string]any, error) {
	if revision != 0 {
		return nil, repo.ErrConflict
	}
	def, raw, err := parseDocument[SourceDefinition](input)
	if err != nil {
		return nil, err
	}
	if err = def.Validate(); err != nil {
		return nil, err
	}
	if def.CredentialRef != "" {
		return nil, invalid("credentialRef", "setAfterCreate")
	}
	if refs, _ := def.tlsReferences(); refs != nil {
		return nil, invalid("extensions.tls", "setAfterCreate")
	}
	var item repo.Source
	r := repo.NewSourceStreamRepo(ctx)
	if def.Protocol == HTTP && def.ConnectionRole() == ServerRole {
		first, last, rangeErr := HTTPServerPortRange()
		if rangeErr != nil {
			return nil, rangeErr
		}
		err = r.WithHTTPSourcePort(ctx, 0, def.HTTP.Port, first, last, func(tx *gorm.DB, port int) error {
			def.HTTP.Port = port
			encoded, marshalErr := json.Marshal(def)
			if marshalErr != nil {
				return marshalErr
			}
			item, marshalErr = repo.NewSourceStreamRepo(tx).CreateSource(ctx, repo.Source{Name: def.Name, Protocol: string(def.Protocol), Document: string(encoded)})
			return marshalErr
		})
	} else {
		item, err = r.CreateSource(ctx, repo.Source{Name: def.Name, Protocol: string(def.Protocol), Document: raw})
	}
	if err != nil {
		return nil, err
	}
	return s.sourceHTTPView(item), nil
}

func sourceImpact(ctx context.Context, r *repo.SourceStreamRepo, id int64) (map[string]any, error) {
	source, err := r.Source(ctx, id)
	if err != nil {
		return nil, err
	}
	streams, topics, err := r.SourceDependencies(ctx, id)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(streams))
	hash := sha256.New()
	fmt.Fprintf(hash, "%d:%d;", id, source.Revision)
	for _, stream := range streams {
		views = append(views, streamView(stream))
		fmt.Fprintf(hash, "%d:%d:%d:%s;", stream.ID, stream.Revision, stream.ActiveVersion, stream.Status)
	}
	return map[string]any{"sourceId": id, "count": int64(len(streams)), "streams": views, "topics": topics, "impactRevision": hex.EncodeToString(hash.Sum(nil))}, nil
}
func (s *Service) Impact(ctx context.Context, id int64) (map[string]any, error) {
	return sourceImpact(ctx, repo.NewSourceStreamRepo(ctx), id)
}
func checkImpact(ctx context.Context, tx *gorm.DB, id int64, confirmed string) error {
	if scope, ok := ctx.Value(impactScopeKey{}).(sourceImpactScope); ok {
		return scope.verify(ctx, tx, id, confirmed)
	}
	// ctx 携带权限身份；查询使用当前锁定事务。
	impact, err := sourceImpact(ctx, repo.NewSourceStreamRepo(tx), id)
	if err != nil {
		return err
	}
	count := impact["count"].(int64)
	if (count > 0 || confirmed != "") && confirmed != impact["impactRevision"].(string) {
		return repo.ErrConflict
	}
	return nil
}
func (s *Service) UpdateSource(ctx context.Context, id, revision int64, input any, confirmed string) (map[string]any, error) {
	def, raw, err := parseDocument[SourceDefinition](input)
	if err != nil {
		return nil, err
	}
	if err = def.Validate(); err != nil {
		return nil, err
	}
	connectionChanged := false
	item, err := repo.NewSourceStreamRepo(ctx).ChangeSource(ctx, id, revision, func(tx *gorm.DB, item *repo.Source) error {
		if !sameSourceConfiguration(item.Document, raw) || confirmed != "" {
			if err := checkImpact(ctx, tx, id, confirmed); err != nil {
				return err
			}
		}
		var old SourceDefinition
		if json.Unmarshal([]byte(item.Document), &old) != nil {
			return repo.ErrInvalidArgument
		}
		if old.Protocol != def.Protocol || old.CredentialRef != def.CredentialRef {
			return invalid("document", "immutableProtocolOrCredential")
		}
		if _, _, err := s.resolveSourceTLS(repo.WithUnsTransaction(ctx, tx), id, def, Credential{}); err != nil {
			return err
		}
		if old.ConnectionRole() != def.ConnectionRole() {
			return invalid("role", "immutable")
		}
		if def.Protocol == HTTP && def.ConnectionRole() == ServerRole && old.HTTP.Port != def.HTTP.Port {
			return invalid("http.port", "immutable")
		}
		if !sameSourceConfiguration(item.Document, raw) {
			connectionChanged = true
			item.ConfigRevision++
			item.Health = "unverified"
			item.HealthMessage = ""
			item.CheckedAt = 0
			item.Resources = "[]"
		}
		item.Name = def.Name
		item.Document = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	if connectionChanged {
		s.refreshSource(ctx, id)
	}
	return sourceView(item), nil
}
func (s *Service) DeleteSource(ctx context.Context, id, revision int64, confirmed string) (map[string]any, error) {
	_, err := repo.NewSourceStreamRepo(ctx).ChangeSource(ctx, id, revision, func(tx *gorm.DB, item *repo.Source) error {
		if err := checkImpact(ctx, tx, id, confirmed); err != nil {
			return err
		}
		item.DeletedAt = time.Now().UnixMilli()
		return nil
	})
	if err == nil {
		s.refreshSource(ctx, id)
	}
	return map[string]any{"id": id}, err
}
func (s *Service) connection(ctx context.Context, id int64, input any) (SourceDefinition, Credential, int64, error) {
	var def SourceDefinition
	var revision int64
	if id > 0 {
		item, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		if err != nil {
			return def, Credential{}, 0, err
		}
		if item.DesiredState == "disabled" {
			return def, Credential{}, 0, invalid("desiredState", "sourceDisabled")
		}
		revision = item.Revision
		if err = json.Unmarshal([]byte(item.Document), &def); err != nil {
			return def, Credential{}, 0, err
		}
	} else {
		parsed, _, err := parseDocument[SourceDefinition](input)
		if err != nil {
			return def, Credential{}, 0, err
		}
		def = parsed
	}
	if err := def.Validate(); err != nil {
		return def, Credential{}, 0, err
	}
	credential := Credential{}
	if def.CredentialRef != "" {
		item, err := repo.NewSourceStreamRepo(ctx).Credential(ctx, id, def.CredentialRef)
		if err != nil {
			return def, credential, 0, ErrCredential
		}
		credential, err = s.cipher.Open(id, item.Ciphertext)
		if err != nil {
			return def, credential, 0, err
		}
	}
	def, credential, err := s.resolveSourceTLS(ctx, id, def, credential)
	return def, credential, revision, err
}
func (s *Service) probeData(ctx context.Context, id int64, input any, timeoutMs int) (map[string]any, error) {
	if timeoutMs < 100 || timeoutMs > 10000 {
		return nil, invalid("timeoutMs", "outOfRange")
	}
	def, credential, revision, err := s.connection(ctx, id, input)
	if err != nil {
		return nil, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	if def.Protocol == OpenProtocol || def.Protocol == EdgeSQL || def.Protocol == FileRead {
		if id == 0 {
			return nil, invalid("sourceId", "saveBeforeTest")
		}
		source, sourceErr := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		if sourceErr != nil {
			return nil, sourceErr
		}
		if source.Revision != revision {
			return nil, repo.ErrConflict
		}
		resources := declaredResources(def, source)
		if len(resources) == 0 {
			return nil, invalid("resourceRef", "undeclared")
		}
		if potentiallyWritingSQL(def) {
			err = probeNamedSQL(operationCtx, def, credential)
		} else {
			_, err = s.extendedPage(operationCtx, source, def, resources[0].ResourceRef, "", 1, 0, timeoutMs, true)
		}
	} else if def.Protocol == HTTP && def.ConnectionRole() == ServerRole {
		err = s.probeHTTPServer(id, def, credential)
	} else if def.SourceType != "" {
		if id == 0 {
			return nil, invalid("sourceId", "saveBeforeTest")
		}
		s.httpManager.CloseSource(id)
		_, err = s.vendorMetadata(operationCtx, id, def)
	} else if def.Protocol == HTTP {
		if id == 0 {
			return nil, invalid("sourceId", "saveBeforeTest")
		}
		source, sourceErr := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		if sourceErr != nil {
			return nil, sourceErr
		}
		// An explicit connection test refreshes a Source's pinned DNS snapshot.
		s.httpManager.CloseSource(id)
		var response json.RawMessage
		response, err = s.HTTPProgramCall(operationCtx, source, def, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"method":"GET","path":""}`)})
		if err == nil {
			var result struct {
				Status int `json:"status"`
			}
			if json.Unmarshal(response, &result) != nil {
				err = ErrUnavailable
			} else if result.Status == http.StatusUnauthorized || result.Status == http.StatusForbidden {
				err = ErrCredential
			} else if result.Status < 200 || result.Status >= 300 {
				err = ErrVendorResponse
			}
		}
	} else if def.Protocol == OAuth {
		_, err = s.readHTTPSource(operationCtx, id, def, json.RawMessage(`{"path":""}`))
	} else {
		if s.runtime == nil {
			return nil, ErrUnavailable
		}
		err = s.runtime.Probe(operationCtx, id, def.Connection(), credential)
	}
	health := "healthy"
	if err != nil {
		health = "unhealthy"
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"health": health, "checkedAt": time.Now().UnixMilli()}, nil
}
func (s *Service) Discover(ctx context.Context, id, revision int64, parent string) (map[string]any, error) {
	def, credential, actual, err := s.connection(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	if actual != revision {
		return nil, repo.ErrConflict
	}
	if def.Protocol == OpenProtocol || def.Protocol == EdgeSQL || def.Protocol == FileRead {
		source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		if err != nil {
			return nil, err
		}
		if source.Revision != revision {
			return nil, repo.ErrConflict
		}
		items := []NativeResource{}
		for _, resource := range declaredResources(def, source) {
			schema, err := json.Marshal(resource.OutputSchema)
			if err != nil {
				return nil, err
			}
			items = append(items, NativeResource{ID: resource.ResourceRef, Name: resource.Name, Schema: schema, Metadata: resource.Metadata})
		}
		return map[string]any{"list": items, "total": len(items)}, nil
	}
	if def.Protocol == HTTP && def.SourceType == "" {
		// HTTP operations are declared by Stream code, not guessed by discovery.
		return map[string]any{"list": []NativeResource{}, "total": 0}, nil
	}
	if s.runtime == nil && def.SourceType == "" {
		return nil, ErrUnavailable
	}
	discoveryTimeout := 10 * time.Second
	if def.Protocol == OPCUA {
		// Allow the managed Node discovery's 30-second budget plus persistence;
		// this management operation does not extend Stream execution limits.
		discoveryTimeout = 35 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	var items []NativeResource
	if def.SourceType != "" {
		items, err = s.vendorDiscover(ctx, id, def)
	} else {
		items, err = s.runtime.Discover(ctx, id, def.Connection(), credential, parent)
	}
	if err != nil {
		return nil, err
	}
	// 增量 OPC UA 浏览与已选范围并存，原生资源 ID 是后续 Stream 引用的依据。
	source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return nil, err
	}
	var previous []NativeResource
	if json.Unmarshal([]byte(source.Resources), &previous) != nil {
		return nil, repo.ErrInvalidArgument
	}
	seen := map[string]NativeResource{}
	for _, item := range previous {
		seen[item.ID] = item
	}
	for _, item := range items {
		seen[item.ID] = item
	}
	merged := make([]NativeResource, 0, len(seen))
	for _, item := range seen {
		merged = append(merged, item)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	resources := string(raw)
	if err = repo.NewSourceStreamRepo(ctx).ObserveSource(ctx, id, revision, "healthy", "", &resources); err != nil {
		return nil, err
	}
	return map[string]any{"list": items, "total": len(items)}, nil
}
func (s *Service) SaveCredential(ctx context.Context, id, revision int64, credential Credential, confirmed string) (map[string]any, error) {
	for key := range credential.Values {
		if strings.HasPrefix(key, "_vendor") {
			return nil, ErrCredential
		}
	}
	sealed, err := s.cipher.Seal(id, credential)
	if err != nil {
		return nil, err
	}
	ref := uuid.NewString()
	item, err := repo.NewSourceStreamRepo(ctx).ChangeSource(ctx, id, revision, func(tx *gorm.DB, item *repo.Source) error {
		if err := checkImpact(ctx, tx, id, confirmed); err != nil {
			return err
		}
		var document map[string]any
		if err := json.Unmarshal([]byte(item.Document), &document); err != nil {
			return err
		}
		if document["protocol"] == string(OpenProtocol) {
			return ErrCredential
		}
		if (document["protocol"] == string(OAuth)) != (credential.Kind == "oauth2") {
			return ErrCredential
		}
		if document["protocol"] == string(HTTP) {
			var d SourceDefinition
			if json.Unmarshal([]byte(item.Document), &d) != nil || d.HTTP == nil ||
				(credential.Kind != "custom" && credential.Kind != "http" && d.HTTPAuthentication() != credential.Kind) {
				return ErrCredential
			}
			if d.ConnectionRole() == ServerRole && !d.HTTP.Anonymous && credential.Secret == "" && credential.Values["token"] == "" {
				return ErrCredential
			}
		} else if document["protocol"] == string(FileRead) {
			var d SourceDefinition
			if json.Unmarshal([]byte(item.Document), &d) != nil || !fileCredentialAllowed(d, credential) {
				return ErrCredential
			}
		} else if onlyTLSValues(credential) && (credential.Kind == "basic" || credential.Kind == "custom") &&
			(document["protocol"] == string(MQTT) || document["protocol"] == string(OPCUA) || document["protocol"] == string(EdgeSQL) && credential.Kind == "basic") {
			// TLS material remains in the same Source-owned encrypted envelope.
		} else if document["protocol"] == string(MQTT) && credential.Kind == "custom" {
			var d SourceDefinition
			if json.Unmarshal([]byte(item.Document), &d) != nil {
				return ErrCredential
			}
			if _, err := MQTTClientTLS(d, credential); err != nil {
				return err
			}
		} else if credential.Kind == "bearer" || credential.Kind == "custom" || credential.Kind == "http" || len(credential.Values) > 0 {
			return ErrCredential
		}
		if credential.Kind == "oauth2" {
			var d SourceDefinition
			if json.Unmarshal([]byte(item.Document), &d) != nil || d.HTTP == nil || d.HTTP.OAuth == nil {
				return ErrCredential
			}
			if d.HTTP.OAuth.ClientAuthentication != "none" && credential.Secret == "" {
				return ErrCredential
			}
			credential.OAuth.ClientConfiguration = oauthConfiguration(d)
			sealed, err = s.cipher.Seal(id, credential)
			if err != nil {
				return err
			}
		}
		if document["protocol"] == string(OPCUA) && document["role"] == string(ServerRole) &&
			(credential.Kind != "basic" || len(credential.Username) > 128 || len(credential.Secret) > 4096) {
			return ErrCredential
		}
		if err = repo.NewSourceStreamRepo(tx).SaveCredential(ctx, repo.SourceCredential{ID: ref, SourceID: id, Ciphertext: sealed, UpdatedAt: time.Now().UnixMilli()}); err != nil {
			return err
		}
		var definition SourceDefinition
		if json.Unmarshal([]byte(item.Document), &definition) != nil {
			return ErrCredential
		}
		if err := provisionTLSReferences(&definition, definition.CredentialRef, ref, credential); err != nil {
			return err
		}
		if _, _, err := s.resolveSourceTLS(repo.WithUnsTransaction(ctx, tx), id, definition, credential); err != nil {
			return err
		}
		document["credentialRef"] = ref
		if len(definition.Extensions) > 0 {
			document["extensions"] = definition.Extensions
		}
		raw, err := json.Marshal(document)
		if err != nil {
			return err
		}
		item.Document = string(raw)
		item.ConfigRevision++
		item.Health = "unverified"
		item.HealthMessage = ""
		item.Resources = "[]"
		item.CheckedAt = 0
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.refreshSource(ctx, id)
	return sourceView(item), nil
}
func (s *Service) Streams(ctx context.Context, f repo.StreamFilter) (map[string]any, error) {
	items, total, err := repo.NewSourceStreamRepo(ctx).ListStreams(ctx, f)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		view, err := s.executionView(ctx, item)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return map[string]any{"list": views, "total": total}, nil
}
func (s *Service) Stream(ctx context.Context, id int64) (map[string]any, error) {
	item, err := repo.NewSourceStreamRepo(ctx).Stream(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.executionView(ctx, item)
}
func authorizeTopics(ctx context.Context, def StreamDefinition) error {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return ErrForbidden
	}
	key := "uns.read"
	if def.Model == Publish {
		key = "uns.write"
	}
	if !contextx.HasResource(subject, key) {
		return ErrForbidden
	}
	if def.MetadataTarget != nil {
		return unsmeta.ValidateMapping(ctx, *def.MetadataTarget)
	}
	for _, id := range def.TopicIDs {
		node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
		if err != nil {
			return err
		}
		if node.Type != 2 {
			return invalid("topicIds", "topicRequired")
		}
	}
	return nil
}
func (s *Service) SaveStream(ctx context.Context, id, revision int64, input any) (map[string]any, error) {
	def, raw, err := parseDocument[StreamDefinition](input)
	if err != nil {
		return nil, err
	}
	if err = def.ValidateDraft(); err != nil {
		return nil, err
	}
	return s.saveProgramV2(ctx, id, revision, def, raw)
}

func (s *Service) Validate(ctx context.Context, input any, sourceRevision int64, deployment bool) (StreamDefinition, string, error) {
	def, raw, err := parseDocument[StreamDefinition](input)
	if err != nil {
		return def, "", err
	}
	if err = def.ValidateDraft(); err != nil {
		return def, "", err
	}
	_, _, err = s.compileProgramV2(ctx, def, deployment)
	return def, raw, err
}

func (s *Service) Test(ctx context.Context, input, payload any, sourceRevision int64, phase string) (map[string]any, error) {
	return s.TestEntry(ctx, input, payload, sourceRevision, phase, "")
}

func (s *Service) TestEntry(ctx context.Context, input, payload any, sourceRevision int64, phase, entryID string) (map[string]any, error) {
	def, _, err := s.Validate(ctx, input, sourceRevision, false)
	if err != nil {
		return nil, err
	}
	if phase == "fetch" {
		ctx = context.WithValue(ctx, programFetchPreviewKey{}, true)
	} else if phase != "input" && phase != "request" && phase != "response" {
		return nil, invalid("phase", "unsupported")
	}
	return s.testProgramV2(ctx, def, payload, entryID)
}

func (s *Service) Deploy(ctx context.Context, id, revision, sourceRevision, versionID int64) (map[string]any, error) {
	r := repo.NewSourceStreamRepo(ctx)
	item, err := r.Stream(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.Revision != revision {
		return nil, repo.ErrConflict
	}
	raw := item.Document
	if versionID > 0 {
		version, err := r.Version(ctx, id, versionID)
		if err != nil {
			return nil, err
		}
		raw = version.Document
	}
	var def StreamDefinition
	if err := decodeStoredProgram([]byte(raw), &def); err != nil {
		return nil, err
	}
	return s.deployProgramV2(ctx, item, revision, versionID, def, raw)
}

func (s *Service) Disable(ctx context.Context, id, revision int64, remove bool) (map[string]any, error) {
	item, err := repo.NewSourceStreamRepo(ctx).DisableStream(ctx, id, revision, remove)
	if err != nil {
		return nil, err
	}
	if remove {
		return streamView(item), nil
	}
	return s.executionView(ctx, item)
}
func (s *Service) Versions(ctx context.Context, id int64, page, size int) (map[string]any, error) {
	items, total, err := repo.NewSourceStreamRepo(ctx).Versions(ctx, id, page, size)
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, item := range items {
		view, err := programVersionView(item)
		if err != nil {
			return nil, err
		}
		release, err := repo.NewSourceStreamRepo(ctx).ReleaseNumber(ctx, id, item.ID)
		if err != nil {
			return nil, err
		}
		view["release"] = release
		list = append(list, view)
	}
	return map[string]any{"list": list, "total": total}, nil
}
func runView(run repo.StreamRun) map[string]any {
	var rawSummary any
	_ = json.Unmarshal([]byte(run.Summary), &rawSummary)
	summary, _ := rawSummary.(map[string]any)
	view := map[string]any{"id": run.ID, "sourceId": run.SourceID, "streamId": run.StreamID, "versionId": run.VersionID, "entryId": run.EntryID, "model": run.Model, "status": run.Status, "summary": rawSummary, "durationMs": run.DurationMs, "messageId": run.MessageID, "createdAt": run.CreatedAt}
	if diagnostic := publicRunError(run.Status, summary); diagnostic != nil {
		view["error"] = diagnostic
		if code, ok := summary["hostErrorCode"].(string); ok {
			if observed := projectRunError(code); observed != nil {
				observed.Repair += " This host failure was observed during the run and may have been caught by the script; it is not proof of the final cause."
				view["diagnostics"] = map[string]any{"hostError": observed}
			}
		}
	}
	return view
}

func (s *Service) Runs(ctx context.Context, f repo.StreamRunFilter) (map[string]any, error) {
	items, total, err := repo.NewSourceStreamRepo(ctx).Runs(ctx, f)
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, item := range items {
		view, err := runReleaseView(ctx, item)
		if err != nil {
			return nil, err
		}
		list = append(list, view)
	}
	return map[string]any{"list": list, "total": total}, nil
}
func (s *Service) Run(ctx context.Context, id string) (map[string]any, error) {
	item, err := repo.NewSourceStreamRepo(ctx).Run(ctx, id)
	if err != nil {
		return nil, err
	}
	return runReleaseView(ctx, item)
}

func runReleaseView(ctx context.Context, item repo.StreamRun) (map[string]any, error) {
	view := runView(item)
	if item.Model == string(ProgramAccess) && item.VersionID > 0 {
		release, err := repo.NewSourceStreamRepo(ctx).ReleaseNumber(ctx, item.StreamID, item.VersionID)
		if err != nil && !repo.IsSourceStreamMissing(err) {
			return nil, err
		}
		if err == nil {
			view["release"] = release
		}
	}
	return view, nil
}
