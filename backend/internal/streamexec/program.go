package streamexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"backend/internal/domain/common"
	"backend/internal/operationcontract"
	"backend/internal/unsref"

	"github.com/dop251/goja"
)

const DefaultProgramTimeoutMs = 1000
const MaxProgramTimeoutMs = 60000
const MaxProgramEntries = 64
const MaxProgramCalls = 64
const MaxPendingProgramCalls = 16

// ProgramManifest is derived by executing synchronous registration, never by scanning code.
type ProgramManifest struct {
	Sources []ProgramSource `json:"sources"`
	Topics  []ProgramTopic  `json:"topics"`
	Entries []ProgramEntry  `json:"entries"`
}

// Version selectors are rejected, including on frozen manifests. This decoder
// does not convert historical releases or select another executor.
func (m *ProgramManifest) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrInput
	}
	type manifest ProgramManifest
	var value manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return ErrInput
	}
	*m = ProgramManifest(value)
	return nil
}

type ProgramSource struct {
	ID        string   `json:"id"`
	Resources []string `json:"resources"`
}

// Entry keys are opaque compiled references, never untyped incoming Topic IDs.
func (m ProgramManifest) MatchesTopicReference(key, topicID string, metadata bool) bool {
	for _, ref := range m.Topics {
		if ref.Valid() && ref.Readable() && ref.CollectionID == "" && ref.Key() == key && ref.TopicID == topicID && ref.Canonical() == metadata {
			return true
		}
	}
	return false
}

type ProgramTopic struct {
	TopicID      string `json:"topicId,omitempty"`
	CollectionID string `json:"collectionId,omitempty"`
	Access       string `json:"access,omitempty"`
	Space        string `json:"space,omitempty"`
}

// Canonical UUID references never fall back to the legacy numeric namespace.
func CanonicalProgramID(id string) bool {
	return unsref.CanonicalUUID(id)
}
func (t ProgramTopic) Key() string {
	if t.CollectionID != "" {
		return "collection:" + t.CollectionID
	}
	if t.Canonical() && !CanonicalProgramID(t.TopicID) {
		return "metadata-topic:" + t.TopicID
	}
	return t.TopicID
}
func (t ProgramTopic) Canonical() bool {
	return t.Space == "metadata" || t.CollectionID != "" || t.Space == "" && unsref.DefaultMetadataTopic(t.TopicID)
}

// Redundant space declarations must not change immutable registration contracts
// or create a second handle to the same unambiguous resource.
func (t ProgramTopic) Normalized() ProgramTopic {
	if t.Space == "metadata" && (t.CollectionID != "" || unsref.DefaultMetadataTopic(t.TopicID)) || t.Space == "legacy" && validProgramID(t.TopicID) {
		t.Space = ""
	}
	return t
}
func (t ProgramTopic) Readable() bool {
	return t.Access == "" || t.Access == "read" || t.Access == "read-write"
}
func (t ProgramTopic) Writable() bool {
	return t.Access == "" || t.Access == "write" || t.Access == "read-write"
}
func (t ProgramTopic) ValidIdentity() bool {
	if t.Space != "" && t.Space != "metadata" && t.Space != "legacy" || t.TopicID != "" && t.CollectionID != "" {
		return false
	}
	if t.CollectionID != "" {
		if t.Space == "legacy" || !unsref.ValidMetadataID(t.CollectionID) {
			return false
		}
	} else if t.Canonical() {
		if !unsref.ValidMetadataID(t.TopicID) {
			return false
		}
	} else {
		if !validProgramID(t.TopicID) {
			return false
		}
	}
	return true
}
func (t ProgramTopic) Valid() bool {
	if !t.ValidIdentity() {
		return false
	}
	if t.Canonical() {
		return t.Access == "read" || t.Access == "write" || t.Access == "read-write"
	}
	return t.Access == "" || t.Access == "read" || t.Access == "write" || t.Access == "read-write"
}

type ProgramEntry struct {
	ID           string                      `json:"id"`
	Kind         string                      `json:"kind"`
	IntervalMs   int                         `json:"intervalMs,omitempty"`
	SourceID     string                      `json:"sourceId,omitempty"`
	ResourceRefs []string                    `json:"resourceRefs,omitempty"`
	TopicIDs     []string                    `json:"topicIds,omitempty"`
	QoS          int                         `json:"qos,omitempty"`
	Method       string                      `json:"method,omitempty"`
	Path         string                      `json:"path,omitempty"`
	Control      *operationcontract.Resource `json:"control,omitempty"`
	Request      *operationcontract.Resource `json:"request,omitempty"`
}

// Operation 不改写历史清单的字段形状；新发布使用 request，旧发布仍可读取。
func (e ProgramEntry) Operation() *operationcontract.Resource {
	if e.Request != nil {
		return e.Request
	}
	return e.Control
}

const implicitRequestContract = "tier0-implicit-request/v1"

func (e ProgramEntry) ImplicitRequest() bool {
	return e.Request != nil && e.Request.ContractVersion == implicitRequestContract
}

// Each Enterprise Backend owns a separate operation catalog. Fleet retains its
// node/resource authorization; this code is a stable identity, not a grant.
func ImplicitRequestDefinition(streamID int64, entryID string, timeoutMs int) (*operationcontract.Resource, error) {
	if streamID <= 0 || strings.TrimSpace(entryID) == "" || len(entryID) > 128 || timeoutMs <= 0 {
		return nil, ErrInput
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("enterprise:%d:%s", streamID, entryID)))
	definition := implicitRequestShape(entryID, timeoutMs)
	definition.Code = fmt.Sprintf("stream.%x", digest[:16])
	return definition, definition.ValidateRequest()
}

func implicitRequestShape(entryID string, timeoutMs int) *operationcontract.Resource {
	name := []rune(strings.TrimSpace(entryID))
	if len(name) > common.NameMaxLen {
		name = name[:common.NameMaxLen]
	}
	return &operationcontract.Resource{
		Name: strings.TrimSpace(string(name)), ContractVersion: implicitRequestContract,
		InputSchema: json.RawMessage(`{}`), OutputSchema: json.RawMessage(`{}`),
		SideEffect: "write", Idempotency: "required", TimeoutMs: min(timeoutMs, operationcontract.MaxRequestTimeoutMs),
	}
}

// Only new releases gain implicit contracts. Never mutate a draft or an old release.
func WithImplicitRequests(manifest ProgramManifest, streamID int64, timeoutMs int) (ProgramManifest, error) {
	if streamID <= 0 || timeoutMs <= 0 || ValidateOperationResources(manifest.Entries) != nil {
		return ProgramManifest{}, ErrInput
	}
	result := manifest
	result.Entries = append([]ProgramEntry(nil), manifest.Entries...)
	for i := range result.Entries {
		entry := &result.Entries[i]
		if entry.Kind != "request" || entry.Operation() != nil {
			continue
		}
		definition, err := ImplicitRequestDefinition(streamID, entry.ID, timeoutMs)
		if err != nil {
			return ProgramManifest{}, err
		}
		entry.Request = definition
	}
	if ValidateOperationResources(result.Entries) != nil || ValidateImplicitRequests(result, streamID, timeoutMs) != nil {
		return ProgramManifest{}, ErrInput
	}
	return result, nil
}

// Verify the identity against the owning release before admitting a frozen contract.
func ValidateImplicitRequests(manifest ProgramManifest, streamID int64, timeoutMs int) error {
	for _, entry := range manifest.Entries {
		if entry.Request == nil || entry.Request.ContractVersion != implicitRequestContract {
			continue
		}
		expected, err := ImplicitRequestDefinition(streamID, entry.ID, timeoutMs)
		if err != nil || entry.Kind != "request" || !sameRequestDefinition(entry.Request, expected) {
			return ErrInput
		}
	}
	return nil
}

func sameRequestDefinition(actual, expected *operationcontract.Resource) bool {
	return actual != nil && expected != nil && actual.Code == expected.Code && actual.Name == expected.Name &&
		actual.Description == expected.Description && actual.ContractVersion == expected.ContractVersion &&
		actual.SideEffect == expected.SideEffect && actual.Idempotency == expected.Idempotency && actual.TimeoutMs == expected.TimeoutMs &&
		bytes.Equal(bytes.TrimSpace(actual.InputSchema), expected.InputSchema) && bytes.Equal(bytes.TrimSpace(actual.OutputSchema), expected.OutputSchema)
}

// Restore only server-generated contracts from a trusted frozen manifest. The
// full manifest comparison still checks all entries and declared dependencies.
func RestoreImplicitRequests(actual ProgramManifest, frozen ProgramManifest, timeoutMs int) (ProgramManifest, error) {
	result := actual
	result.Entries = append([]ProgramEntry(nil), actual.Entries...)
	for i := range result.Entries {
		entry := &result.Entries[i]
		if entry.Request != nil && entry.Request.ContractVersion == implicitRequestContract {
			return ProgramManifest{}, ErrInput
		}
		if entry.Kind != "request" || entry.Operation() != nil {
			continue
		}
		for _, prior := range frozen.Entries {
			definition := prior.Request
			if prior.ID != entry.ID || prior.Kind != "request" || definition == nil || definition.ContractVersion != implicitRequestContract {
				continue
			}
			// The runtime has no Stream ID. The Backend validates ownership; here
			// enforce the generated shape and timeout before comparing manifests.
			expected := implicitRequestShape(entry.ID, timeoutMs)
			if definition.ValidateRequest() != nil || len(definition.Code) != len("stream.")+32 || !strings.HasPrefix(definition.Code, "stream.") {
				return ProgramManifest{}, ErrInput
			}
			for _, c := range strings.TrimPrefix(definition.Code, "stream.") {
				if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
					return ProgramManifest{}, ErrInput
				}
			}
			expected.Code = definition.Code
			if !sameRequestDefinition(definition, expected) {
				return ProgramManifest{}, ErrInput
			}
			copyDefinition := *definition
			entry.Request = &copyDefinition
			break
		}
	}
	return result, nil
}

func (m ProgramManifest) ReadOnlyRequest(entryID string) bool {
	for _, entry := range m.Entries {
		if entry.ID == entryID {
			return entry.Request != nil && entry.Request.SideEffect == "read"
		}
	}
	return false
}

func (m ProgramManifest) ValidateRequestEffects(entryID string, result ProgramResult) error {
	if m.ReadOnlyRequest(entryID) && (len(result.Outputs) > 0 || result.Checkpoint != nil) {
		return ErrOutput
	}
	return nil
}

// 宿主和隔离执行器共用只读准入规则；未知调用默认拒绝。
func ReadOnlyProgramCall(call ProgramCall) bool {
	switch call.Kind {
	case "source.read", "source.parameters", "source.secrets", "uns.read", "uns.query", "checkpoint.get":
		return true
	case "source.request":
		var options struct {
			Method string `json:"method"`
		}
		return json.Unmarshal(call.Options, &options) == nil && (options.Method == "GET" || options.Method == "HEAD")
	}
	return false
}

// ValidateOperationResources is used both after isolated registration and when
// the trusted host reloads a stored manifest. Only manual request entries expose
// operations; Source HTTP routes keep their separate protocol contract.
func ValidateOperationResources(entries []ProgramEntry) error {
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Control != nil && entry.Request != nil {
			return operationcontract.ErrContract
		}
		resource := entry.Operation()
		if resource == nil {
			continue
		}
		var err error
		if entry.Request != nil {
			err = resource.ValidateRequest()
		} else {
			err = resource.Validate()
		}
		if entry.Kind != "request" || err != nil || seen[resource.Code] {
			return operationcontract.ErrContract
		}
		seen[resource.Code] = true
	}
	return nil
}

// NormalizeOperationResources gives embedded Schemas stable bytes before a
// manifest is frozen or compared. PostgreSQL JSONB changes whitespace/key order;
// that representation change must not invalidate an otherwise identical plan.
func NormalizeOperationResources(entries []ProgramEntry) error {
	if err := ValidateOperationResources(entries); err != nil {
		return err
	}
	for i := range entries {
		if entries[i].Operation() == nil {
			continue
		}
		resource := *entries[i].Operation()
		for _, schema := range []*json.RawMessage{&resource.InputSchema, &resource.OutputSchema} {
			var value any
			decoder := json.NewDecoder(bytes.NewReader(*schema))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return operationcontract.ErrContract
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return operationcontract.ErrContract
			}
			*schema = raw
		}
		if entries[i].Request != nil {
			entries[i].Request = &resource
		} else {
			entries[i].Control = &resource
		}
	}
	return nil
}

type ProgramRequest struct {
	SourceAliases    map[string]ProgramSource `json:"sourceAliases,omitempty"`
	Code             string                   `json:"code"`
	Input            json.RawMessage          `json:"input"`
	Context          json.RawMessage          `json:"context"`
	TimeoutMs        int                      `json:"timeoutMs"`
	CheckOnly        bool                     `json:"checkOnly"`
	EntryID          string                   `json:"entryId,omitempty"`
	ExpectedManifest *ProgramManifest         `json:"expectedManifest,omitempty"`
}
type ProgramOutput struct {
	TopicID      string          `json:"topicId"`
	CollectionID string          `json:"collectionId,omitempty"`
	Space        string          `json:"space,omitempty"`
	Data         json.RawMessage `json:"data"`
}
type ProgramResult struct {
	Manifest   ProgramManifest    `json:"manifest"`
	Data       json.RawMessage    `json:"data,omitempty"`
	Outputs    []ProgramOutput    `json:"outputs"`
	Logs       []Log              `json:"logs"`
	DurationMs int64              `json:"durationMs"`
	Error      string             `json:"error,omitempty"`
	Checkpoint *ProgramCheckpoint `json:"checkpoint,omitempty"`
}

// The trusted host attaches this buffered change after isolated execution.
type ProgramCheckpoint struct {
	Revision int64           `json:"revision"`
	Value    json.RawMessage `json:"value"`
}

const MaxCheckpointBytes = 16 * 1024

// ProgramCall contains stable references and JSON, never protocol clients.
// Authorized secrets are resolved by the host per execution, not in manifests.
type ProgramCall struct {
	ID           int             `json:"id"`
	Kind         string          `json:"kind"`
	SourceID     string          `json:"sourceId,omitempty"`
	ResourceRef  string          `json:"resourceRef,omitempty"`
	TopicID      string          `json:"topicId,omitempty"`
	CollectionID string          `json:"collectionId,omitempty"`
	Space        string          `json:"space,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
}
type ProgramHost func(context.Context, ProgramCall) (json.RawMessage, error)

func (c ProgramCall) UNSReference() ProgramTopic {
	return ProgramTopic{TopicID: c.TopicID, CollectionID: c.CollectionID, Space: c.Space}
}

func (o ProgramOutput) UNSReference() ProgramTopic {
	return ProgramTopic{TopicID: o.TopicID, CollectionID: o.CollectionID, Space: o.Space}
}

// AllowsProgramCall checks compiled references only; the host still checks actual operations and authorization.
func AllowsProgramCall(manifest ProgramManifest, call ProgramCall) bool {
	if call.ID < 1 || call.ID > MaxProgramCalls ||
		(len(call.Input) != 0 && !validProgramJSON(call.Input)) || (len(call.Options) != 0 && !validProgramJSON(call.Options)) {
		return false
	}
	if call.CollectionID != "" && call.Kind != "uns.read" && call.Kind != "uns.query" {
		return false
	}
	if call.Space != "" && call.Kind != "uns.read" && call.Kind != "uns.query" {
		return false
	}
	if call.Kind == "checkpoint.get" || call.Kind == "checkpoint.set" {
		return len(manifest.Entries) > 0 && call.SourceID == "" && call.ResourceRef == "" && call.TopicID == "" && call.CollectionID == "" && len(call.Options) == 0 &&
			(call.Kind == "checkpoint.get" && len(call.Input) == 0 || call.Kind == "checkpoint.set" && len(call.Input) > 0 && len(call.Input) <= MaxCheckpointBytes)
	}
	if call.Kind == "uns.read" || call.Kind == "uns.query" {
		if call.SourceID != "" || call.ResourceRef != "" || len(call.Input) != 0 || !call.UNSReference().ValidIdentity() {
			return false
		}
		if call.Kind == "uns.query" && (len(call.Options) == 0 || !call.UNSReference().Canonical()) {
			return false
		}
		for _, topic := range manifest.Topics {
			if topic.Valid() && topic.Readable() && topic.Key() == call.UNSReference().Key() && topic.Canonical() == call.UNSReference().Canonical() {
				return true
			}
		}
		return false
	}
	if call.Kind == "source.parameters" || call.Kind == "source.secrets" || call.Kind == "source.request" {
		return allowsProgramHTTPCall(manifest, call)
	}
	if call.Kind != "source.read" && call.Kind != "source.write" && call.Kind != "source.invoke" && call.Kind != "source.publish" {
		return false
	}
	if call.TopicID != "" || call.CollectionID != "" || (call.Kind == "source.read" && len(call.Input) != 0) || (call.Kind != "source.read" && len(call.Input) == 0) {
		return false
	}
	for _, source := range manifest.Sources {
		if source.ID == call.SourceID {
			for _, ref := range source.Resources {
				if ref == call.ResourceRef {
					return true
				}
			}
		}
	}
	return false
}

type programCompletion struct {
	resolve func(interface{}) error
	reject  func(interface{}) error
	data    json.RawMessage
	err     error
}
type programVM struct {
	sourceAliases   map[string]ProgramSource
	vm              *goja.Runtime
	ctx             context.Context
	parse           goja.Callable
	stringify       goja.Callable
	jsonObject      *goja.Object
	host            ProgramHost
	result          ProgramResult
	handlers        map[string]goja.Callable
	sources         map[string]ProgramSource
	topics          map[string]bool
	sourceHandles   map[*goja.Object]string
	topicHandles    map[*goja.Object]string
	topicObjects    map[string]*goja.Object
	completions     chan programCompletion
	registering     bool
	calls           int
	pending         int
	bufferBytes     int
	logBytes        int
	unhandled       map[*goja.Promise]bool
	readOnlyRequest bool
	effectDenied    bool
}

// ExecuteProgram must run inside a hard-memory-limited executor slot, not the Backend.
// All Goja operations, including Promise settlement, stay on this goroutine.
func ExecuteProgram(parent context.Context, request ProgramRequest, host ProgramHost) (ProgramResult, error) {
	started := time.Now()
	empty := ProgramResult{}
	if len(request.Code) == 0 || len(request.Code) > MaxCodeBytes || request.TimeoutMs < 1 || request.TimeoutMs > MaxProgramTimeoutMs {
		return empty, ErrInput
	}
	program, err := goja.Compile("stream-program.js", "(function(module, exports) { 'use strict';\n"+request.Code+"\n})", true)
	if err != nil {
		return empty, ErrSyntax
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(request.TimeoutMs)*time.Millisecond)
	defer cancel()
	vm := goja.New()
	vm.SetMaxCallStackSize(512)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ErrTimeout)
		case <-done:
		}
	}()
	jsonObject := vm.Get("JSON").ToObject(vm)
	parse, _ := goja.AssertFunction(jsonObject.Get("parse"))
	stringify, _ := goja.AssertFunction(jsonObject.Get("stringify"))
	freeze, _ := goja.AssertFunction(vm.Get("Object").ToObject(vm).Get("freeze"))
	runner := &programVM{sourceAliases: request.SourceAliases, vm: vm, ctx: ctx, parse: parse, stringify: stringify, jsonObject: jsonObject, host: host,
		handlers: map[string]goja.Callable{}, sources: map[string]ProgramSource{}, topics: map[string]bool{},
		sourceHandles: map[*goja.Object]string{}, topicHandles: map[*goja.Object]string{}, topicObjects: map[string]*goja.Object{},
		completions: make(chan programCompletion, MaxPendingProgramCalls), registering: true, unhandled: map[*goja.Promise]bool{},
		result: ProgramResult{Manifest: ProgramManifest{Sources: []ProgramSource{}, Topics: []ProgramTopic{}, Entries: []ProgramEntry{}}, Outputs: []ProgramOutput{}, Logs: []Log{}}}
	vm.SetPromiseRejectionTracker(func(p *goja.Promise, operation goja.PromiseRejectionOperation) {
		if operation == goja.PromiseRejectionReject {
			runner.unhandled[p] = true
		} else {
			delete(runner.unhandled, p)
		}
	})
	// Registration must be reproducible. Restore clocks/randomness only for event handling.
	originalDate := vm.Get("Date")
	mathObject := vm.Get("Math").ToObject(vm)
	originalRandom := mathObject.Get("random")
	blocked := func(goja.FunctionCall) goja.Value {
		panic(vm.NewTypeError("clock/random is not available during registration"))
	}
	_ = vm.Set("Date", blocked)
	_ = mathObject.Set("random", blocked)
	module := vm.NewObject()
	exports := vm.NewObject()
	_ = module.Set("exports", exports)
	runner.installCrypto()
	value, err := vm.RunProgram(program)
	if err != nil {
		return empty, executionError(ctx)
	}
	loader, ok := goja.AssertFunction(value)
	if !ok {
		return empty, ErrExecution
	}
	if _, err = loader(goja.Undefined(), module, exports); err != nil {
		return empty, executionError(ctx)
	}
	register, ok := goja.AssertFunction(module.Get("exports"))
	if !ok {
		return empty, ErrExecution
	}
	value, err = register(goja.Undefined(), runner.api())
	if err != nil {
		return empty, executionError(ctx)
	}
	if _, async := value.Export().(*goja.Promise); async || len(runner.result.Manifest.Entries) == 0 || len(runner.unhandled) != 0 {
		return empty, ErrExecution
	}
	if NormalizeOperationResources(runner.result.Manifest.Entries) != nil {
		return empty, ErrInput
	}
	// The script cannot claim the reserved server-generated contract version.
	frozen := ProgramManifest{}
	if request.ExpectedManifest != nil {
		frozen = *request.ExpectedManifest
	}
	runner.result.Manifest, err = RestoreImplicitRequests(runner.result.Manifest, frozen, request.TimeoutMs)
	if err != nil {
		return empty, err
	}
	if manifest, err := json.Marshal(runner.result.Manifest); err != nil || len(manifest) > MaxJSONBytes {
		return empty, ErrOutput
	}
	if request.ExpectedManifest != nil {
		expected, err := json.Marshal(request.ExpectedManifest)
		actual, _ := json.Marshal(runner.result.Manifest)
		if err != nil || string(expected) != string(actual) {
			return empty, ErrInput
		}
	}
	if request.CheckOnly {
		runner.result.DurationMs = time.Since(started).Milliseconds()
		return runner.result, nil
	}
	handler := runner.handlers[request.EntryID]
	if handler == nil || !validProgramJSON(request.Input) {
		return empty, ErrInput
	}
	var resource *operationcontract.Resource
	for _, entry := range runner.result.Manifest.Entries {
		if entry.ID == request.EntryID {
			resource = entry.Operation()
			break
		}
	}
	executionCtx := ctx
	runner.readOnlyRequest = runner.result.Manifest.ReadOnlyRequest(request.EntryID)
	if resource != nil {
		if resource.ValidateInput(request.Input) != nil {
			return empty, ErrInput
		}
		operationCtx, cancelOperation := context.WithTimeout(ctx, time.Duration(resource.TimeoutMs)*time.Millisecond)
		defer cancelOperation()
		executionCtx = operationCtx
		runner.ctx = operationCtx
		go func() {
			select {
			case <-operationCtx.Done():
				vm.Interrupt(ErrTimeout)
			case <-done:
			}
		}()
	}
	if len(request.Context) == 0 {
		request.Context = json.RawMessage(`{}`)
	}
	if !validProgramJSON(request.Context) {
		return empty, ErrInput
	}
	runner.registering = false
	_ = vm.Set("Date", originalDate)
	_ = mathObject.Set("random", originalRandom)
	contextValue, err := runner.decode(request.Context)
	if err != nil {
		return empty, ErrInput
	}
	contextObject, ok := contextValue.(*goja.Object)
	if !ok || contextObject.ClassName() != "Object" {
		return empty, ErrInput
	}
	input, err := runner.decode(request.Input)
	if err != nil {
		return empty, ErrInput
	}
	_ = contextObject.Set("input", input)
	_ = contextObject.Set("entryId", request.EntryID)
	for _, entry := range runner.result.Manifest.Entries {
		if entry.ID == request.EntryID && entry.Kind == "http-request" {
			object, ok := input.(*goja.Object)
			if !ok || object.ClassName() != "Object" {
				return empty, ErrInput
			}
			// The Gateway provides the trusted envelope; code never invents route data.
			_ = contextObject.Set("request", input)
			break
		}
	}
	if _, err = freeze(goja.Undefined(), contextObject); err != nil {
		return empty, ErrExecution
	}
	value, err = handler(goja.Undefined(), contextObject)
	if err != nil {
		return empty, executionError(executionCtx)
	}
	promise, asynchronous := value.Export().(*goja.Promise)
	for runner.pending > 0 || (asynchronous && promise.State() == goja.PromiseStatePending) {
		select {
		case <-executionCtx.Done():
			return empty, ErrTimeout
		case completion := <-runner.completions:
			runner.pending--
			if completion.err != nil {
				err = completion.reject(vm.NewTypeError("host operation failed"))
			} else {
				var decoded goja.Value
				decoded, err = runner.decode(completion.data)
				if err == nil {
					err = completion.resolve(decoded)
				} else {
					err = completion.reject(vm.NewTypeError("invalid host JSON response"))
				}
			}
			if err != nil {
				return empty, executionError(executionCtx)
			}
		}
	}
	if executionCtx.Err() != nil {
		return empty, ErrTimeout
	}
	if len(runner.unhandled) > 0 || runner.effectDenied {
		return empty, ErrExecution
	}
	if asynchronous {
		if promise.State() != goja.PromiseStateFulfilled {
			return empty, ErrExecution
		}
		value = promise.Result()
	}
	if !goja.IsUndefined(value) {
		raw, err := runner.encodeData(value, MaxProgramDataBytes)
		if err != nil {
			return empty, ErrOutput
		}
		runner.result.Data = raw
	}
	if resource != nil && resource.ValidateOutput(runner.result.Data) != nil {
		// Buffered UNS writes are not returned for commit. Source operations
		// may already have occurred; the host must never auto-retry this error.
		return empty, ErrOutput
	}
	runner.result.DurationMs = time.Since(started).Milliseconds()
	if encoded, err := json.Marshal(runner.result); err != nil || len(encoded) > MaxProgramDataBytes {
		return empty, ErrOutput
	}
	return runner.result, nil
}

func validProgramJSON(raw json.RawMessage) bool {
	return len(raw) > 0 && len(raw) <= MaxJSONBytes && json.Valid(raw)
}
func validProgramID(id string) bool {
	return unsref.ValidLegacyID(id)
}
func (p *programVM) fail(message string) { panic(p.vm.NewTypeError(message)) }
func (p *programVM) decode(raw json.RawMessage) (goja.Value, error) {
	return p.parse(p.jsonObject, p.vm.ToValue(string(raw)))
}
func (p *programVM) encode(value goja.Value) (json.RawMessage, error) {
	return p.encodeData(value, MaxJSONBytes)
}
func (p *programVM) encodeData(value goja.Value, limit int) (json.RawMessage, error) {
	encoded, err := p.stringify(p.jsonObject, value)
	if err != nil || goja.IsUndefined(encoded) {
		return nil, ErrOutput
	}
	raw := json.RawMessage(encoded.String())
	if len(raw) == 0 || len(raw) > limit || !json.Valid(raw) {
		return nil, ErrOutput
	}
	return raw, nil
}
func (p *programVM) declaration(value goja.Value, target interface{}) {
	if !p.registering {
		p.fail("declarations are only available during registration")
	}
	raw, err := p.encode(value)
	if err != nil {
		p.fail("invalid declaration")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		p.fail("invalid declaration fields")
	}
}
func (p *programVM) api() *goja.Object {
	api := p.vm.NewObject()
	sources := p.vm.NewObject()
	_ = sources.Set("use", func(call goja.FunctionCall) goja.Value {
		var source ProgramSource
		if alias, ok := call.Argument(0).Export().(string); ok {
			bound, exists := p.sourceAliases[alias]
			if !exists {
				p.fail("undeclared Source alias")
			}
			source = ProgramSource{ID: bound.ID, Resources: append([]string{}, bound.Resources...)}
		} else {
			p.declaration(call.Argument(0), &source)
		}
		_, exists := p.sources[source.ID]
		if !validProgramID(source.ID) || source.Resources == nil || len(source.Resources) > 1000 || (!exists && len(p.sources) >= 64) || len(p.sourceHandles) >= 1000 {
			p.fail("invalid Source declaration")
		}
		seen := map[string]bool{}
		for _, ref := range source.Resources {
			if strings.TrimSpace(ref) == "" || len(ref) > 2048 || seen[ref] {
				p.fail("invalid Source resource")
			}
			seen[ref] = true
		}
		if previous, exists := p.sources[source.ID]; exists {
			// Separate registration blocks share one dependency, while each
			// handle's I/O retains its own explicitly declared resource set.
			merged := previous
			merged.Resources = append([]string{}, previous.Resources...)
			registered := map[string]bool{}
			for _, ref := range previous.Resources {
				registered[ref] = true
			}
			for _, ref := range source.Resources {
				if !registered[ref] {
					merged.Resources = append(merged.Resources, ref)
					registered[ref] = true
				}
			}
			if len(merged.Resources) > 1000 {
				p.fail("Source resource limit exceeded")
			}
			p.sources[source.ID] = merged
			for i := range p.result.Manifest.Sources {
				if p.result.Manifest.Sources[i].ID == source.ID {
					p.result.Manifest.Sources[i] = merged
					break
				}
			}
		} else {
			p.sources[source.ID] = source
			p.result.Manifest.Sources = append(p.result.Manifest.Sources, source)
		}
		p.checkManifestSize()
		handle := p.vm.NewObject()
		p.sourceHandles[handle] = source.ID
		p.installHTTPSource(handle, source.ID)
		for _, operation := range []string{"read", "write", "invoke", "publish"} {
			operation := operation
			_ = handle.Set(operation, func(call goja.FunctionCall) goja.Value {
				if p.registering {
					p.fail("Source I/O is not available during registration")
				}
				ref := call.Argument(0).String()
				if !seen[ref] {
					p.fail("undeclared Source resource")
				}
				request := ProgramCall{Kind: "source." + operation, SourceID: source.ID, ResourceRef: ref}
				var err error
				optionIndex := 1
				if operation != "read" {
					request.Input, err = p.encode(call.Argument(1))
					optionIndex = 2
					if err != nil {
						p.fail("invalid Source input")
					}
				}
				if !goja.IsUndefined(call.Argument(optionIndex)) {
					request.Options, err = p.encode(call.Argument(optionIndex))
					if err != nil {
						p.fail("invalid Source options")
					}
				}
				return p.dispatch(request)
			})
		}
		return handle
	})
	_ = api.Set("sources", sources)
	uns := p.vm.NewObject()
	_ = uns.Set("use", func(call goja.FunctionCall) goja.Value {
		var topic ProgramTopic
		p.declaration(call.Argument(0), &topic)
		if !topic.Valid() {
			p.fail("invalid Topic declaration")
		}
		topic = topic.Normalized()
		key := topic.Key()
		if handle := p.topicObjects[key]; handle != nil {
			for _, declared := range p.result.Manifest.Topics {
				if declared.Key() == key && declared != topic {
					p.fail("conflicting UNS declaration")
				}
			}
			return handle
		}
		if len(p.topics) >= 1000 {
			p.fail("Topic declaration limit exceeded")
		}
		p.topics[key] = topic.Readable() && topic.CollectionID == ""
		p.result.Manifest.Topics = append(p.result.Manifest.Topics, topic)
		p.checkManifestSize()
		handle := p.vm.NewObject()
		p.topicHandles[handle] = key
		p.topicObjects[key] = handle
		_ = handle.Set("read", func(call goja.FunctionCall) goja.Value {
			if !topic.Readable() {
				p.fail("UNS read not declared")
			}
			request := ProgramCall{Kind: "uns.read", TopicID: topic.TopicID, CollectionID: topic.CollectionID, Space: topic.Space}
			if !goja.IsUndefined(call.Argument(0)) {
				var err error
				request.Options, err = p.encode(call.Argument(0))
				if err != nil {
					p.fail("invalid Topic options")
				}
			}
			return p.dispatch(request)
		})
		_ = handle.Set("query", func(call goja.FunctionCall) goja.Value {
			if !topic.Readable() || !topic.Canonical() {
				p.fail("UNS query requires a canonical readable reference")
			}
			if len(call.Arguments) != 1 || goja.IsUndefined(call.Argument(0)) || goja.IsNull(call.Argument(0)) {
				p.fail("UNS query requires options")
			}
			options, err := p.encode(call.Argument(0))
			if err != nil {
				p.fail("invalid UNS query options")
			}
			return p.dispatch(ProgramCall{Kind: "uns.query", TopicID: topic.TopicID, CollectionID: topic.CollectionID, Space: topic.Space, Options: options})
		})
		_ = handle.Set("write", func(call goja.FunctionCall) goja.Value {
			if p.readOnlyRequest {
				p.effectDenied = true
				p.fail("read-only request cannot write UNS data")
			}
			if !topic.Writable() {
				p.fail("UNS write not declared")
			}
			if p.registering {
				p.fail("Topic writes are not available during registration")
			}
			raw, err := p.encode(call.Argument(0))
			if err != nil || len(p.result.Outputs) >= 1000 || p.bufferBytes+len(raw) > MaxProgramDataBytes {
				p.fail("Topic output buffer exceeded")
			}
			p.bufferBytes += len(raw)
			p.result.Outputs = append(p.result.Outputs, ProgramOutput{TopicID: topic.TopicID, CollectionID: topic.CollectionID, Space: topic.Space, Data: raw})
			promise, resolve, _ := p.vm.NewPromise()
			_ = resolve(p.vm.ToValue(map[string]interface{}{"stage": "buffered", "topicId": topic.TopicID, "collectionId": topic.CollectionID}))
			return p.vm.ToValue(promise)
		})
		return handle
	})
	_ = api.Set("uns", uns)
	stream := p.vm.NewObject()
	checkpoint := p.vm.NewObject()
	_ = checkpoint.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 0 {
			p.fail("checkpoint.get does not accept arguments")
		}
		return p.dispatch(ProgramCall{Kind: "checkpoint.get"})
	})
	_ = checkpoint.Set("set", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 1 {
			p.fail("checkpoint.set requires one JSON value")
		}
		raw, err := p.encode(call.Argument(0))
		if err != nil || len(raw) > MaxCheckpointBytes {
			p.fail("invalid checkpoint value")
		}
		return p.dispatch(ProgramCall{Kind: "checkpoint.set", Input: raw})
	})
	_ = stream.Set("checkpoint", checkpoint)
	for name, kind := range map[string]string{
		"onRequest": "request", "onMessage": "source-event", "onSource": "source-event",
		"onEvent": "uns-event", "onTopic": "uns-event", "onTimer": "interval", "every": "interval",
	} {
		kind := kind
		_ = stream.Set(name, func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) != 2 {
				p.fail("entry requires options and handler")
			}
			var entry ProgramEntry
			p.declaration(p.entryOptions(call.Argument(0)), &entry)
			if entry.Kind != "" {
				p.fail("entry kind is defined by registration method")
			}
			entry.Kind = kind
			if kind == "request" && (entry.SourceID != "" || entry.Method != "" || entry.Path != "") {
				options, _ := call.Argument(0).(*goja.Object)
				handle, ok := options.Get("source").(*goja.Object)
				if !ok || p.sourceHandles[handle] != entry.SourceID {
					p.fail("HTTP entry requires a declared Source handle")
				}
				entry.Kind = "http-request"
			}
			if !p.validEntry(entry) {
				p.fail("invalid entry declaration")
			}
			handler, ok := goja.AssertFunction(call.Argument(1))
			if !ok {
				p.fail("entry requires a function")
			}
			p.handlers[entry.ID] = handler
			p.result.Manifest.Entries = append(p.result.Manifest.Entries, entry)
			p.checkManifestSize()
			return goja.Undefined()
		})
	}
	_ = api.Set("stream", stream)
	log := p.vm.NewObject()
	for _, level := range []string{"info", "warn", "error", "debug"} {
		level := level
		_ = log.Set(level, func(call goja.FunctionCall) goja.Value {
			if p.registering {
				p.fail("logs are not available during registration")
			}
			message := call.Argument(0).String()
			if len(message) > 2048 || len(p.result.Logs) >= 100 || p.logBytes+len(message) > 64*1024 {
				p.fail("log limit exceeded")
			}
			p.logBytes += len(message)
			p.result.Logs = append(p.result.Logs, Log{Level: level, Message: message})
			return goja.Undefined()
		})
	}
	_ = api.Set("log", log)
	return api
}

// Registration can use actual SDK handles, not serialized connection objects.
func (p *programVM) entryOptions(value goja.Value) *goja.Object {
	if !p.registering {
		p.fail("entries are only available during registration")
	}
	options, ok := value.(*goja.Object)
	if !ok || options.ClassName() != "Object" {
		p.fail("entry options must be an object")
	}
	copy := p.vm.NewObject()
	for _, key := range options.Keys() {
		item := options.Get(key)
		switch key {
		case "source":
			handle, ok := item.(*goja.Object)
			id := p.sourceHandles[handle]
			if !ok || id == "" || (options.Get("sourceId") != nil && !goja.IsUndefined(options.Get("sourceId"))) {
				p.fail("source must be a declared SDK handle")
			}
			_ = copy.Set("sourceId", id)
		case "topics":
			array, ok := item.(*goja.Object)
			if !ok || array.ClassName() != "Array" || (options.Get("topicIds") != nil && !goja.IsUndefined(options.Get("topicIds"))) {
				p.fail("topics must be declared SDK handles")
			}
			length := array.Get("length").ToInteger()
			if length < 1 || length > 1000 {
				p.fail("invalid entry topics")
			}
			ids := make([]string, 0, length)
			for i := int64(0); i < length; i++ {
				handle, ok := array.Get(strconv.FormatInt(i, 10)).(*goja.Object)
				id := p.topicHandles[handle]
				if !ok || id == "" {
					p.fail("topic must be a declared SDK handle")
				}
				ids = append(ids, id)
			}
			_ = copy.Set("topicIds", ids)
		default:
			_ = copy.Set(key, item)
		}
	}
	return copy
}

func (p *programVM) checkManifestSize() {
	if raw, err := json.Marshal(p.result.Manifest); err != nil || len(raw) > MaxJSONBytes {
		p.fail("program manifest limit exceeded")
	}
}
func (p *programVM) validEntry(entry ProgramEntry) bool {
	if strings.TrimSpace(entry.ID) == "" || len(entry.ID) > 128 || p.handlers[entry.ID] != nil || len(p.handlers) >= MaxProgramEntries {
		return false
	}
	if entry.Kind != "http-request" && (entry.Method != "" || entry.Path != "") {
		return false
	}
	switch entry.Kind {
	case "interval":
		return entry.IntervalMs >= 100 && entry.IntervalMs <= 86400000 && entry.SourceID == "" && len(entry.ResourceRefs) == 0 && len(entry.TopicIDs) == 0 && entry.QoS == 0
	case "request":
		return entry.IntervalMs == 0 && entry.SourceID == "" && len(entry.ResourceRefs) == 0 && len(entry.TopicIDs) == 0 && entry.QoS == 0
	case "http-request":
		_, ok := p.sources[entry.SourceID]
		return ok && ValidateHTTPRoute(entry.Method, entry.Path) && entry.IntervalMs == 0 && len(entry.ResourceRefs) == 0 && len(entry.TopicIDs) == 0 && entry.QoS == 0
	case "source-event":
		source, ok := p.sources[entry.SourceID]
		if !ok || entry.IntervalMs != 0 || len(entry.ResourceRefs) == 0 || len(entry.TopicIDs) != 0 || entry.QoS < 0 || entry.QoS > 2 {
			return false
		}
		allowed := map[string]bool{}
		for _, ref := range source.Resources {
			allowed[ref] = true
		}
		for _, ref := range entry.ResourceRefs {
			if !allowed[ref] {
				return false
			}
			delete(allowed, ref)
		}
		return true
	case "uns-event":
		if entry.IntervalMs != 0 || entry.SourceID != "" || len(entry.ResourceRefs) != 0 || len(entry.TopicIDs) == 0 || entry.QoS != 0 {
			return false
		}
		seen := map[string]bool{}
		for _, id := range entry.TopicIDs {
			if !p.topics[id] || seen[id] {
				return false
			}
			seen[id] = true
		}
		return true
	}
	return false
}
func (p *programVM) dispatch(call ProgramCall) goja.Value {
	if p.readOnlyRequest && !ReadOnlyProgramCall(call) {
		p.effectDenied = true
		p.fail("read-only request cannot perform side effects")
	}
	if p.registering {
		p.fail("host I/O is not available during registration")
	}
	if p.host == nil || p.calls >= MaxProgramCalls || p.pending >= MaxPendingProgramCalls {
		p.fail("host operation unavailable or limit exceeded")
	}
	p.calls++
	p.pending++
	call.ID = p.calls
	promise, resolve, reject := p.vm.NewPromise()
	go func() {
		completion := programCompletion{resolve: resolve, reject: reject}
		func() {
			defer func() {
				if recover() != nil {
					completion.err = errors.New("host operation panic")
				}
			}()
			completion.data, completion.err = p.host(p.ctx, call)
		}()
		if completion.err == nil && (len(completion.data) == 0 || len(completion.data) > MaxProgramDataBytes || !json.Valid(completion.data)) {
			completion.err = ErrOutput
		}
		select {
		case p.completions <- completion:
		case <-p.ctx.Done():
		}
	}()
	return p.vm.ToValue(promise)
}
