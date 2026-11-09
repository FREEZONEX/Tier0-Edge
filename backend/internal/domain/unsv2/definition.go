// Package unsv2 owns the shared internal/OpenAPI UNS contract and batch jobs.
package unsv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"backend/internal/domain/common"
	"backend/internal/domain/unsmeta"
)

const (
	SyncItems             = 200
	MaxItems              = 10000
	MaxBodyBytes          = 16 << 20
	MaxQueries            = 20
	MaxQueryResponseBytes = 8 << 20
)

var Kinds = []string{"directory", "topic", "collection", "source", "stream", "source_state", "app", "task", "source_plan", "source_version", "subscription"}

type Fault struct {
	Data           any              `json:"-"`
	RetryAdvice    *RetryAdvice     `json:"retryAdvice,omitempty"`
	Code           string           `json:"code"`
	Message        string           `json:"-"`
	QueryID        string           `json:"queryId,omitempty"`
	OpID           string           `json:"opId,omitempty"`
	Path           string           `json:"path,omitempty"`
	Retryable      bool             `json:"retryable"`
	Reason         string           `json:"reason,omitempty"`
	Repair         string           `json:"repair,omitempty"`
	ObjectKind     string           `json:"objectKind,omitempty"`
	ObjectID       string           `json:"objectId,omitempty"`
	Candidates     []map[string]any `json:"candidates,omitempty"`
	RolledBack     bool             `json:"rolledBack,omitempty"`
	ConditionIndex *int             `json:"conditionIndex,omitempty"`
}

func (f *Fault) Error() string { return f.Message }
func invalid(path, message string) error {
	return &Fault{Code: "INVALID_REQUEST", Path: path, Message: message, Reason: message, Repair: "Compare this path with the current request schema and model constraints; preview the corrected batch with dryRun=true"}
}
func problem(code, message string) error {
	return &Fault{Code: code, Message: message, Reason: message, Repair: RepairHint(code)}
}

func Decode(raw []byte, target any) error {
	if len(raw) > MaxBodyBytes {
		return problem("REQUEST_TOO_LARGE", "request exceeds 16 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		path := ""
		field := ""
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			_ = json.Unmarshal([]byte(strings.TrimPrefix(err.Error(), "json: unknown field ")), &field)
		}
		if typeError, ok := err.(*json.UnmarshalTypeError); ok {
			parts := strings.Split(typeError.Field, ".")
			field = parts[len(parts)-1]
		}
		if field != "" {
			path = diagnosticFieldPath(raw, field)
		}
		return &Fault{Code: "INVALID_REQUEST", Path: path, Message: err.Error(), Reason: "Request contains an unknown field or a value of the wrong type", Repair: "Use the current request schema; send only writable fields and string IDs"}
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return invalid("", "one JSON object required")
	}
	return nil
}

type Identity struct {
	ID  string
	Ref string
}

func (i *Identity) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	if json.Unmarshal(raw, &i.ID) == nil {
		if i.ID != "" && !unsmeta.ValidID(i.ID) {
			return fmt.Errorf("invalid ID")
		}
		return nil
	}
	var ref struct {
		Ref string `json:"ref"`
	}
	if Decode(raw, &ref) != nil || !unsmeta.ValidID(ref.Ref) {
		return fmt.Errorf("ID must be a string or {ref:opId}")
	}
	i.Ref = ref.Ref
	return nil
}
func (i Identity) MarshalJSON() ([]byte, error) {
	if i.Ref != "" {
		return json.Marshal(map[string]string{"ref": i.Ref})
	}
	if i.ID == "" {
		return []byte("null"), nil
	}
	return json.Marshal(i.ID)
}

type ManagedBy struct {
	SourceID   Identity `json:"sourceId"`
	InstanceID string   `json:"instanceId"`
}
type Change struct {
	LockToken        string            `json:"lockToken,omitempty"`
	LockTokens       map[string]string `json:"lockTokens,omitempty"`
	Release          bool              `json:"release,omitempty"`
	Update           json.RawMessage   `json:"update,omitempty"`
	ArrayFilters     []any             `json:"arrayFilters,omitempty"`
	Upsert           bool              `json:"upsert,omitempty"`
	noMatch          bool
	OpID             string            `json:"opId"`
	Action           string            `json:"action"`
	Kind             string            `json:"kind,omitempty"`
	ID               Identity          `json:"id,omitempty"`
	CollectionID     Identity          `json:"collectionId,omitempty"`
	RecordID         Identity          `json:"recordId,omitempty"`
	StreamID         Identity          `json:"streamId,omitempty"`
	ExpectedRevision int64             `json:"expectedRevision,omitempty"`
	Data             json.RawMessage   `json:"data,omitempty"`
	DependsOn        []string          `json:"dependsOn,omitempty"`
	ManagedBy        *ManagedBy        `json:"managedBy,omitempty"`
	Time             int64             `json:"time,omitempty"`
	Quality          string            `json:"quality,omitempty"`
	Exists           *bool             `json:"exists,omitempty"`
	Filter           json.RawMessage   `json:"filter,omitempty"`
	Selection        string            `json:"selection,omitempty"`
	Sort             []RecordOrder     `json:"sort,omitempty"`
	ReturnDocument   string            `json:"returnDocument,omitempty"`
	Conditions       []RecordFilter    `json:"conditions,omitempty"`
	Operations       []FieldOperation  `json:"operations,omitempty"`
	Bindings         []ResultBinding   `json:"bindings,omitempty"`
	Query            *TransactionQuery `json:"query,omitempty"`
}

type TransactionQuery struct {
	Filter      json.RawMessage        `json:"filter,omitempty"`
	Sort        []RecordOrder          `json:"sort,omitempty"`
	Filters     []RecordFilter         `json:"filters,omitempty"`
	OrderBy     []RecordOrder          `json:"orderBy,omitempty"`
	Limit       int                    `json:"limit,omitempty"`
	ExpectCount *QueryCountExpectation `json:"expectCount,omitempty"`
}

type QueryCountExpectation struct {
	Min *int64 `json:"min,omitempty"`
	Max *int64 `json:"max,omitempty"`
}
type FieldOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}
type ResultBinding struct {
	Path       string `json:"path"`
	Ref        string `json:"ref"`
	ResultPath string `json:"resultPath"`
}
type Batch struct {
	AssertLocks    []RecordLockAssertion `json:"assertLocks,omitempty"`
	IdempotencyKey string                `json:"idempotencyKey"`
	Mode           string                `json:"mode,omitempty"`
	Atomic         *bool                 `json:"atomic,omitempty"`
	DryRun         bool                  `json:"dryRun,omitempty"`
	Items          []Change              `json:"items"`
}

// Reject unsupported actions and ambiguous object identities before submitting
// a job. Zero revisions mean creation only; replacement must never upsert.
func (c Change) validate() error {
	if c.Action == metadataDocumentAction {
		return validateMetadataDocumentChange(c)
	}
	if err := validateTransactionFields(c); err != nil {
		return err
	}
	creation := false
	switch c.Action {
	case "meta.create", "meta.replace", "meta.patch", "meta.resourcePatch", "meta.copy", "meta.archive", "meta.restore":
		if !contains([]string{"directory", "topic", "collection", "source", "stream", "app"}, c.Kind) {
			return invalid("kind", "writable metadata kind required")
		}
		if (c.Action == "meta.copy" || c.Action == "meta.archive" || c.Action == "meta.restore") && !contains([]string{"directory", "topic", "collection"}, c.Kind) && !(c.Kind == "source" && c.Action != "meta.copy") {
			return invalid("action", "this configuration uses its domain lifecycle API")
		}
		creation = c.Action == "meta.create"
		if c.Action == "meta.copy" && c.Kind == "collection" {
			return invalid("kind", "copy duplicates directory or Topic placement only")
		}
	case "stream.publish":
		if c.Kind != "" && c.Kind != "stream" {
			return invalid("kind", "publish targets a Stream")
		}
		if c.StreamID.ID == "" && c.StreamID.Ref == "" {
			return invalid("streamId", "Stream identity required")
		}
	case "record.create", "record.replace", "record.delete", "record.restore", "record.check", "record.read", "record.query", "record.modify", "record.update", "sample.append":
		if c.Kind != "" {
			return invalid("kind", "data actions do not accept metadata kind")
		}
		if c.CollectionID.ID == "" && c.CollectionID.Ref == "" {
			return invalid("collectionId", "Collection identity required")
		}
		if c.Action == "sample.append" && c.RecordID.ID == "" && c.RecordID.Ref == "" {
			return invalid("recordId", "Record identity required")
		}
		creation = c.Action == "record.create" || c.Action == "sample.append"
	default:
		candidates := []map[string]any{}
		seen := map[string]bool{}
		for _, model := range Models() {
			for _, action := range model.Actions {
				if !seen[action] {
					seen[action] = true
					candidates = append(candidates, map[string]any{"action": action, "operation": "apply"})
				}
			}
		}
		return &Fault{Code: "UNKNOWN_ACTION", Path: "action", Reason: "This batch action is not supported", Message: "unsupported batch action", Repair: "Choose a supported action from candidates; interface operation names are not batch actions", Candidates: candidates}
	}
	if c.Action == "stream.publish" {
		if c.ExpectedRevision < 0 {
			return invalid("expectedRevision", "nonnegative optional revision required")
		}
	} else if creation {
		if c.ExpectedRevision != 0 {
			return invalid("expectedRevision", "creation revision must be zero")
		}
	} else if strings.HasPrefix(c.Action, "meta.") {
		if !unsmeta.ValidID(c.ID.ID) || c.ID.Ref != "" || c.ExpectedRevision < 0 {
			return invalid("id/expectedRevision", "existing metadata identity and nonnegative optional revision required")
		}
	} else if c.Action == "record.query" {
		// Queries identify a Collection; a result binding identifies later targets.
	} else if transactionAction(c.Action) || c.Action == "record.replace" || c.Action == "record.delete" || c.Action == "record.restore" {
		if (!unsmeta.ValidID(c.ID.ID) && !unsmeta.ValidID(c.ID.Ref) && !hasTargetBinding(c) && len(c.Filter) == 0) || c.ExpectedRevision < 0 {
			return invalid("filter/expectedRevision", "record filter or legacy identity and nonnegative optional revision required")
		}
	} else if ((!unsmeta.ValidID(c.ID.ID) || c.ID.Ref != "") && !(strings.HasPrefix(c.Action, "record.") && c.ID.Ref != "")) || c.ExpectedRevision < 1 {
		return invalid("id/expectedRevision", "existing identity and positive revision required")
	}
	if c.ManagedBy != nil && c.Action != "record.create" {
		return invalid("managedBy", "only record creation accepts a Source instance")
	}
	return nil
}

func ValidateChange(c Change) error { return c.validate() }

type WriteDelivery struct {
	State           string   `json:"state"`
	LocalCommitted  bool     `json:"localCommitted"`
	CenterCommitted bool     `json:"centerCommitted"`
	Pending         int      `json:"pending"`
	Failed          int      `json:"failed"`
	EventIDs        []string `json:"eventIds,omitempty"`
}
type Result struct {
	Delivery     *WriteDelivery          `json:"delivery,omitempty"`
	Query        *TransactionQueryResult `json:"query,omitempty"`
	Error        *Fault                  `json:"error,omitempty"`
	OpID         string                  `json:"opId"`
	Action       string                  `json:"action"`
	Kind         string                  `json:"kind,omitempty"`
	ID           string                  `json:"id"`
	CollectionID string                  `json:"collectionId,omitempty"`
	Revision     int64                   `json:"revision,omitempty"`
	Release      int                     `json:"release,omitempty"`
	SourceID     *string                 `json:"sourceId,omitempty"`
	Status       string                  `json:"status"`
	Data         json.RawMessage         `json:"data,omitempty"`
	Document     *RecordDocument         `json:"document,omitempty"`
}

// RecordDocument is the public query document shape, independent of storage
// bookkeeping fields. Snapshots are retained in the same atomic write receipt.
type RecordDocument struct {
	SourceTime   *int64           `json:"sourceTime"`
	Lock         *RecordLeaseView `json:"lock,omitempty"`
	ID           string           `json:"id"`
	CollectionID string           `json:"collectionId"`
	Data         json.RawMessage  `json:"data"`
	Revision     int64            `json:"revision"`
	CreatedAt    int64            `json:"createdAt"`
	UpdatedAt    int64            `json:"updatedAt"`
	SourceID     *string          `json:"sourceId"`
}

type RecordLeaseView struct {
	Label     string `json:"label"`
	ExpiresAt int64  `json:"expiresAt"`
	Fence     int64  `json:"fence"`
	Since     int64  `json:"since"`
}
type Outcome struct {
	NextIndex    *int            `json:"nextIndex,omitempty"`
	UnknownUntil *int            `json:"unknownUntil,omitempty"`
	Summary      *OutcomeSummary `json:"summary,omitempty"`
	Status       string          `json:"status"`
	Replayed     bool            `json:"replayed"`
	JobID        string          `json:"jobId,omitempty"`
	Results      []Result        `json:"results,omitempty"`
}

type OutcomeSummary struct {
	Total       int `json:"total"`
	Succeeded   int `json:"succeeded"`
	Failed      int `json:"failed"`
	NotExecuted int `json:"notExecuted"`
	Unknown     int `json:"unknown"`
}

type Header struct {
	SchemaVersion int    `json:"schemaVersion"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
}

func (h Header) validate() error {
	if h.SchemaVersion != 1 || strings.TrimSpace(h.Name) != h.Name || h.Name == "" || common.ExceedNameLimit(h.Name) || common.ExceedDescLimit(h.Description) {
		return invalid("data", "schemaVersion=1 and valid name/description required")
	}
	return nil
}

type Processor struct {
	Language  string `json:"language"`
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeoutMs"`
}
type Output struct {
	RecordSchema *unsmeta.Schema `json:"recordSchema"`
	SampleSchema *unsmeta.Schema `json:"sampleSchema,omitempty"`
}

type StreamDefinition struct {
	SupportedProtocols []string        `json:"supportedProtocols"`
	ParametersSchema   *unsmeta.Schema `json:"parametersSchema"`
	Processor          Processor       `json:"processor"`
	Output             Output          `json:"output"`
}
type Release struct {
	PublishedAt int64            `json:"publishedAt"`
	Definition  StreamDefinition `json:"definition"`
}
type StreamData struct {
	Header
	ExecutionMode string             `json:"executionMode,omitempty"`
	Draft         *StreamDefinition  `json:"draft"`
	Releases      map[string]Release `json:"releases,omitempty"`
}

type Binding struct {
	Name          string         `json:"name"`
	StreamID      string         `json:"streamId"`
	StreamRelease int            `json:"streamRelease"`
	Enabled       bool           `json:"enabled"`
	Acquisition   map[string]any `json:"acquisition"`
	Parameters    map[string]any `json:"parameters"`
	Target        struct {
		CollectionID string `json:"collectionId"`
	} `json:"target"`
	Mapping unsmeta.Mapping `json:"mapping"`
	TopicID string          `json:"topicId,omitempty"`
}
type SourceData struct {
	Header
	Adapter string `json:"adapter"`
	Config  struct {
		Connection map[string]any     `json:"connection"`
		Streams    map[string]Binding `json:"streams,omitempty"`
	} `json:"config"`
	ConfigRevision int64 `json:"configRevision,omitempty"`
}
type Grant struct {
	Alias        string   `json:"alias"`
	CollectionID string   `json:"collectionId"`
	Permissions  []string `json:"permissions"`
}
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Member struct {
	UserID  string   `json:"userId"`
	RoleIDs []string `json:"roleIds"`
}
type AppData struct {
	Header
	StreamInteraction   *AppStreamPolicy `json:"streamInteraction,omitempty"`
	Entry               *AppEntry        `json:"entry,omitempty"`
	Roles               []Role           `json:"roles"`
	Members             []Member         `json:"members"`
	Collections         []Grant          `json:"collections"`
	PlatformPermissions []string         `json:"platformPermissions,omitempty"`
	Navigation          struct {
		TopicIDs []string `json:"topicIds"`
	} `json:"navigation"`
	Status string `json:"status,omitempty"`
}

// AppStreamPolicy is independent of the App's Collection/data status. Only
// server-managed native installations may carry this policy; public generic
// App writes cannot create, replace or erase it.
type AppStreamPolicy struct {
	MessageTopics       []AppStreamMessageTopicPermission `json:"messageTopics,omitempty"`
	Version             string                            `json:"version"`
	Status              string                            `json:"status"`
	PlatformPermissions []string                          `json:"platformPermissions"`
}

type AppStreamMessageTopicPermission struct {
	Topic     string `json:"topic"`
	Publish   bool   `json:"publish"`
	Subscribe bool   `json:"subscribe"`
}

// AppEntry contains portable paths only. Deployment addresses are never stored in metadata.
type AppEntry struct {
	Environment string `json:"environment,omitempty"`
	BasePath    string `json:"basePath,omitempty"`
	Path        string `json:"path"`
}

type Pagination struct {
	Page   int    `json:"page,omitempty"`
	Size   int    `json:"size,omitempty"`
	Search string `json:"search,omitempty"`
}
type TimeRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}
type History struct {
	Pagination
	TimeRange *TimeRange `json:"timeRange,omitempty"`
}
type Selected struct {
	RecordID string   `json:"recordId"`
	History  *History `json:"history,omitempty"`
}
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Query struct {
	QueryID string `json:"queryId"`
	Pagination
	CursorOptions
	Mode                string           `json:"mode,omitempty"`
	Kinds               []string         `json:"kinds,omitempty"`
	IDs                 []string         `json:"ids,omitempty"`
	IncludeArchived     bool             `json:"includeArchived,omitempty"`
	ParentID            *string          `json:"parentId,omitempty"`
	IncludeAncestors    bool             `json:"includeAncestors,omitempty"`
	CollectionID        string           `json:"collectionId,omitempty"`
	RecordIDs           []string         `json:"recordIds,omitempty"`
	TopicID             string           `json:"topicId,omitempty"`
	Include             []string         `json:"include,omitempty"`
	Records             *Pagination      `json:"records,omitempty"`
	SelectedRecord      *Selected        `json:"selectedRecord,omitempty"`
	TimeRange           *TimeRange       `json:"timeRange,omitempty"`
	FieldPath           []string         `json:"fieldPath,omitempty"`
	MaxPoints           int              `json:"maxPoints,omitempty"`
	IncludeLatestSample bool             `json:"includeLatestSample,omitempty"`
	Targets             []Target         `json:"targets,omitempty"`
	RelationKinds       []string         `json:"relationKinds,omitempty"`
	JobIDs              []string         `json:"jobIds,omitempty"`
	Filters             []RecordFilter   `json:"filters,omitempty"`
	OrderBy             []RecordOrder    `json:"orderBy,omitempty"`
	Relational          *RelationalQuery `json:"relational,omitempty"`
}

type RecordFilter struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}
type RecordOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}
type Queries struct {
	Queries []Query `json:"queries"`
}
