package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	domainlicense "backend/internal/domain/license"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
	"github.com/dop251/goja/parser"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func require(ctx context.Context, keys ...string) error {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return problem("UNAUTHENTICATED", "authentication required")
	}
	for _, key := range keys {
		if !domainlicense.Default().AllowResource(key) {
			return problem("FORBIDDEN", "product, License or installation capability denied: "+key)
		}
		if !contextx.HasResource(subject, key) {
			return problem("FORBIDDEN", "missing resource permission: "+key)
		}
	}
	return nil
}
func mutationPermissions(ctx context.Context, c Change) error {
	if c.Action == metadataDocumentAction {
		return metadataDocumentPermissions(ctx, c)
	}
	// Replacement historically required only write access. A requested
	// snapshot additionally reads existing data and must retain that grant on
	// receipt replay and job inspection as well as the initial execution.
	if c.ReturnDocument != "" && !transactionAction(c.Action) {
		if err := recordReadPermission(ctx, c.CollectionID.ID); err != nil {
			return err
		}
	}
	if transactionAction(c.Action) {
		if !(c.Action == "record.update" && len(c.Update) > 0 && c.ReturnDocument == "") {
			if err := recordReadPermission(ctx, c.CollectionID.ID); err != nil {
				return err
			}
		}
		if c.Action != "record.modify" && c.Action != "record.update" {
			return nil
		}
	}
	if subject, ok := contextx.SubjectFrom(ctx); ok && subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only" && appIdentity(ctx) == "" {
		return problem("FORBIDDEN", "read-only API keys cannot mutate UNS resources")
	}
	if c.Action == "meta.resourcePatch" {
		var patch resourcePatch
		if err := Decode(c.Data, &patch); err != nil {
			return err
		}
		operation, ok := FindResourceOperation(patch.Operation)
		if !ok || operation.Mutation != "meta.resourcePatch" || operation.Resource != c.Kind || !ResourceAllowed(ctx, operation) {
			return problem("FORBIDDEN", "resource patch operation is not authorized")
		}
		if operation.Name == "appStreamPolicyUpdate" {
			return appStreamPolicyManager(ctx)
		}
	}
	if subject, ok := contextx.SubjectFrom(ctx); ok && !mcpaccess.AllowedMutation(subject, c.Action, c.Kind, c.ManagedBy != nil) {
		return problem("FORBIDDEN", "MCP operation scope was not granted for this change")
	}
	if err := appMutationAccess(ctx, c); err != nil {
		return err
	}
	key := "uns.write"
	if c.Action == "record.delete" {
		key = "uns.delete"
	}
	if c.Kind == "source" {
		key = "uns.source.manage"
	}
	if c.Kind == "stream" {
		key = "uns.stream.manage"
	}
	if c.Kind == "app" {
		key = "uns.app.manage"
	}
	if c.Action == "stream.publish" {
		key = "uns.stream.manage"
		if err := require(ctx, key); err != nil {
			return err
		}
		// This guard runs before idempotency receipts and during job inspection.
		// Publication authority cannot be bypassed by replaying an earlier result.
		if c.StreamID.ID != "" {
			var execution int64
			if err := repo.GetUnsConn(ctx).Model(&repo.UnsMeta{}).Where("kind='stream' AND id=? AND data->'draft'->'execution' IS NOT NULL", c.StreamID.ID).Count(&execution).Error; err != nil {
				return err
			}
			if execution > 0 {
				return require(ctx, "uns.stream.manage", "uns.source.manage", "uns.write")
			}

		}
	}
	if c.Action == "meta.archive" || c.Action == "record.delete" {
		if err := require(ctx, "uns.delete"); err != nil {
			return err
		}
	}
	if c.ManagedBy != nil {
		if err := require(ctx, "uns.source.manage"); err != nil {
			return err
		}
	}
	return require(ctx, key)
}
func meta(ctx context.Context, kind, id string, archived bool) (repo.UnsMeta, error) {
	if !unsmeta.ValidID(id) {
		return repo.UnsMeta{}, invalid("id", "valid ID required")
	}
	return repo.NewUnsMetaRepo(ctx).Get(ctx, id, archived, kind)
}
func definition(ctx context.Context, id string) (unsmeta.Document, error) {
	if !unsmeta.ValidID(id) {
		return unsmeta.Document{}, invalid("id", "valid ID required")
	}
	return unsmeta.RecordDefinition(ctx, id)
}
func readDefinition(ctx context.Context, id string) (unsmeta.Document, error) {
	if !unsmeta.ValidID(id) {
		return unsmeta.Document{}, invalid("id", "valid ID required")
	}
	return unsmeta.ReadCollectionDefinition(ctx, id)
}
func writeMeta(tx *gorm.DB, c Change, data any) (repo.UnsMeta, error) {
	now := time.Now().UnixMilli()
	raw, err := json.Marshal(data)
	if err != nil {
		return repo.UnsMeta{}, err
	}
	row := repo.UnsMeta{ID: c.ID.ID, Kind: c.Kind, StorageKind: c.Kind, Data: raw, Revision: c.ExpectedRevision + 1, CreatedAt: now, UpdatedAt: now}
	if c.Action == "meta.create" {
		if c.ExpectedRevision != 0 {
			return row, problem("VERSION_CONFLICT", "creation expectedRevision must be zero")
		}
		err = tx.Create(&row).Error
		return row, err
	}
	var old repo.UnsMeta
	err = tx.Where("kind=? AND id=? AND deleted_at=0", c.Kind, c.ID.ID).Take(&old).Error
	if err != nil {
		return row, err
	}
	if old.Revision != c.ExpectedRevision {
		return row, problem("VERSION_CONFLICT", "object revision changed")
	}
	if old.MountID != nil {
		return row, problem("FORBIDDEN", "mounted UNS resources are read-only")
	}
	if c.Kind == "source" {
		var prior, updated map[string]json.RawMessage
		if json.Unmarshal(old.Data, &prior) == nil && json.Unmarshal(raw, &updated) == nil {
			if credentials, ok := prior["credentials"]; ok {
				updated["credentials"] = credentials
				raw, err = json.Marshal(updated)
				if err != nil {
					return row, err
				}
				row.Data = raw
			}
		}
	}
	before := old
	row.CreatedAt = old.CreatedAt
	err = tx.Model(&old).Where("revision=?", c.ExpectedRevision).Updates(map[string]any{"data": raw, "revision": row.Revision, "updated_at": now}).Error
	if err == nil && c.Kind == "source" {
		err = preserveUnchangedAcquisition(tx, before, row)
	}
	return row, err
}
func validateStream(data StreamData) error {
	if err := data.Header.validate(); err != nil {
		return err
	}
	if data.ExecutionMode != "" && data.ExecutionMode != "transform" {
		return invalid("executionMode", "shared configuration only; complete programs retain their existing API")
	}
	if data.Draft == nil {
		return invalid("draft", "processor definition required")
	}
	d := data.Draft
	if d.ParametersSchema == nil || d.ParametersSchema.Type != "object" || d.ParametersSchema.Validate() != nil || d.Output.RecordSchema == nil || d.Output.RecordSchema.Type != "object" || d.Output.RecordSchema.Validate() != nil {
		return invalid("draft", "valid parameter and output schemas required")
	}
	if d.Output.SampleSchema != nil && (d.Output.SampleSchema.Type != "object" || d.Output.SampleSchema.Validate() != nil) {
		return invalid("output.sampleSchema", "invalid sample schema")
	}
	if d.Processor.Language != "javascript" || len(d.Processor.Code) > sourcestream.MaxScriptBytes || strings.TrimSpace(d.Processor.Code) == "" || d.Processor.TimeoutMs < 1 || d.Processor.TimeoutMs > sourcestream.MaxScriptTimeoutMs {
		return invalid("processor", "invalid processor language, code or timeout")
	}
	if _, err := parser.ParseFile(nil, "processor.js", "function process(input,params){"+d.Processor.Code+"}", 0); err != nil {
		return invalid("processor.code", "invalid JavaScript")
	}
	return nil
}
func validateSource(ctx context.Context, data *SourceData, old *SourceData) error {
	if err := data.Header.validate(); err != nil {
		return err
	}
	if len(data.Config.Streams) > repo.MaxSourceStreams {
		return invalid("config.streams", "too many Source instances")
	}
	conn := data.Config.Connection
	if conn == nil {
		return invalid("config.connection", "connection required")
	}
	raw, _ := json.Marshal(conn)
	var d sourcestream.SourceDefinition
	if err := Decode(raw, &d); err != nil {
		var fault *Fault
		if errors.As(err, &fault) {
			fault.Path = "data/config/connection/" + strings.TrimPrefix(fault.Path, "/")
		}
		return err
	}
	d.Name = data.Name
	d.Description = data.Description
	d.Protocol = sourcestream.Protocol(data.Adapter)
	if d.RuntimeLocation == "" {
		d.RuntimeLocation = "local"
	}
	if d.MQTT == nil && d.Protocol == sourcestream.MQTT {
		d.MQTT = &sourcestream.MQTTOptions{ClientID: "uns-v3"}
	}
	if d.OPCUA == nil && d.Protocol == sourcestream.OPCUA {
		d.OPCUA = &sourcestream.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None"}
	}
	if validation := d.Validate(); validation != nil {
		var field *sourcestream.ValidationError
		if errors.As(validation, &field) {
			return invalid("data/config/connection/"+field.Field, "Connection validation: "+field.Code)
		}
		return invalid("data/config/connection", "invalid protocol connection")
	}
	if parsed, err := url.Parse(d.Endpoint); err != nil || parsed.User != nil {
		return invalid("config.connection", "credentials must not be embedded in endpoint")
	}
	// This endpoint cannot create encrypted secrets or mint credential references.
	if d.CredentialRef != "" {
		if !unsmeta.ValidID(d.CredentialRef) {
			return invalid("credentialRef", "valid Source credential reference required")
		}
		if old == nil || old.Config.Connection["credentialRef"] != d.CredentialRef {
			sourceID, _ := ctx.Value(credentialSourceKey{}).(string)
			var count int64
			if sourceID == "" {
				return invalid("credentialRef", "provision a credential on this Source first")
			}
			if err := repo.GetUnsConn(ctx).Model(&repo.UnsMeta{}).Where("kind='source' AND id=? AND deleted_at=0 AND jsonb_exists(data->'credentials',?)", sourceID, d.CredentialRef).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return invalid("credentialRef", "credential does not belong to this Source")
			}
		}
	}
	normalized, _ := json.Marshal(d)
	if err := json.Unmarshal(normalized, &data.Config.Connection); err != nil {
		return err
	}
	for instance, b := range data.Config.Streams {

		if !unsmeta.ValidID(instance) || b.Name == "" || common.ExceedNameLimit(b.Name) || !unsmeta.ValidID(b.StreamID) || b.StreamRelease < 1 {
			return invalid("config.streams", "invalid instance identity or release")
		}
		if b.Enabled && (old == nil || !old.Config.Streams[instance].Enabled) {
			if allowed, _ := ctx.Value(bindingLifecycleKey{}).(bool); !allowed {
				return invalid("data/config/streams/"+jsonPointerPart(instance)+"/enabled", "enable through sourceBindingsStateUpdate")
			}
		}
		if b.Enabled && (old == nil || !reflect.DeepEqual(b, old.Config.Streams[instance]) || !reflect.DeepEqual(data.Config.Connection, old.Config.Connection)) {
			if err := require(ctx, "uns.write", "uns.stream.manage"); err != nil {
				return err
			}
		}
		a, err := acquisition(b.Acquisition, data.Adapter, b.Enabled)
		if err != nil {
			return err
		}
		if a.Mode == "poll" && d.ConnectionRole() != sourcestream.ClientRole {
			return invalid("acquisition.mode", "shared HTTP poll requires a client Source; server Sources use the complete program lifecycle")
		}
		if a.Mode == "watch" {
			if _, err := (sourcestream.StreamDefinition{Model: sourcestream.Publish, Collection: a.Collection, NativeResourceRefs: a.NativeResourceRefs}).EffectiveSource(d); err != nil {
				return invalid("acquisition.collection", "invalid native resources or protocol collection settings")
			}
		}
		row, err := meta(ctx, "stream", b.StreamID, false)
		if err != nil {
			return err
		}
		var stream StreamData
		if json.Unmarshal(row.Data, &stream) != nil {
			return invalid("streamId", "not a reusable Stream")
		}
		release, ok := stream.Releases[strconv.Itoa(b.StreamRelease)]
		if !ok {
			return invalid("streamRelease", "release not found")
		}
		if release.Definition.ParametersSchema == nil || !release.Definition.ParametersSchema.Accepts(b.Parameters) {
			return invalid("parameters", "parameters do not satisfy shared Stream schema")
		}
		protocolOK := false
		for _, protocol := range release.Definition.SupportedProtocols {
			if protocol == data.Adapter {
				protocolOK = true
			}
		}
		if !protocolOK {
			return invalid("adapter", "Stream does not support this protocol")
		}
		doc, err := definition(ctx, b.Target.CollectionID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(doc.RecordSchema, release.Definition.Output.RecordSchema) || !reflect.DeepEqual(doc.SampleSchema, release.Definition.Output.SampleSchema) {
			return invalid("target.collectionId", "target schemas differ from Stream output")
		}
		b.Mapping.CollectionID = b.Target.CollectionID
		b.Mapping.TopicID = b.TopicID
		if err := unsmeta.ValidateMapping(ctx, b.Mapping); err != nil {
			return err
		}
		// Store identity only at target/topicId; mapping paths remain source-independent.
		b.Mapping.CollectionID = ""
		b.Mapping.TopicID = ""
		data.Config.Streams[instance] = b
	}
	return nil
}
func validateApp(ctx context.Context, data AppData) error {
	if err := validateAppEntry(data.Entry); err != nil {
		return err
	}
	platformPermissions := map[string]bool{}
	for _, permission := range data.PlatformPermissions {
		if !contains(AppPlatformPermissions(), permission) || platformPermissions[permission] {
			return invalid("platformPermissions", "unknown or duplicate App platform permission")
		}
		platformPermissions[permission] = true
		if subject, ok := contextx.SubjectFrom(ctx); ok && subject.MCPRestricted {
			return problem("FORBIDDEN", "MCP has no consent for App platform permissions")
		}
		if err := require(ctx, permission); err != nil {
			return err
		}
	}
	if data.Status != "" && data.Status != "active" && data.Status != "disabled" {
		return invalid("status", "active or disabled required")
	}
	if err := data.Header.validate(); err != nil {
		return err
	}
	roles := map[string]bool{}
	for _, role := range data.Roles {
		if !unsmeta.ValidID(role.ID) || roles[role.ID] || role.Name == "" || common.ExceedNameLimit(role.Name) {
			return invalid("roles", "invalid or duplicate role")
		}
		roles[role.ID] = true
	}
	members := map[string]bool{}
	for _, m := range data.Members {
		if members[m.UserID] || len(m.RoleIDs) == 0 {
			return invalid("members", "duplicate member or no role")
		}
		members[m.UserID] = true
		id, err := strconv.ParseInt(m.UserID, 10, 64)
		if err != nil {
			return invalid("userId", "platform user ID required")
		}
		if user, err := repo.NewIAMRepo(ctx).GetUserByID(ctx, id); err != nil || user.Status != 1 {
			return invalid("userId", "active platform user required")
		}
		for _, role := range m.RoleIDs {
			if !roles[role] {
				return invalid("roleIds", "role does not belong to this App")
			}
		}
	}
	aliases := map[string]bool{}
	for _, g := range data.Collections {
		if !unsmeta.ValidID(g.Alias) || aliases[g.Alias] || len(g.Permissions) == 0 {
			return invalid("collections", "invalid grant alias")
		}
		aliases[g.Alias] = true
		if _, err := definition(ctx, g.CollectionID); err != nil {
			return err
		}
		for _, p := range g.Permissions {
			key := "uns.read"
			switch p {
			case "record:read", "sample:read":
			case "record:write", "sample:write":
				key = "uns.write"
			case "record:delete":
				key = "uns.delete"
			default:
				return invalid("permissions", "unknown Collection permission")
			}
			if err := require(ctx, key); err != nil {
				return err
			}
			if subject, ok := contextx.SubjectFrom(ctx); ok && subject.MCPRestricted &&
				((key == "uns.write" && !mcpaccess.Has(subject, "uns.write")) || (key == "uns.delete" && !mcpaccess.Has(subject, "uns.delete"))) {
				return problem("FORBIDDEN", "App grants cannot exceed MCP consented data permissions")
			}
		}
	}
	for _, id := range data.Navigation.TopicIDs {
		if _, err := meta(ctx, "topic", id, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) metadataChange(ctx context.Context, tx *gorm.DB, c Change) (Result, error) {
	if isAppStreamPolicyChange(c) {
		return s.writeAppStreamPolicy(ctx, tx, c)
	}
	if c.Kind == "app" && c.ID.ID != "" {
		reserved, err := repo.NewProjectRepo(ctx).ProjectAppPrincipalReserved(ctx, c.ID.ID)
		if err != nil {
			return Result{}, err
		}
		if reserved {
			return Result{}, problem("FORBIDDEN", "Project App identities cannot be managed through external App registration")
		}
	}
	if c.Action == metadataDocumentAction {
		return s.metadataDocumentChange(ctx, tx, c)
	}
	// The metadata gate is already held. Resolve the current revision here,
	// inside the same transaction, rather than racing a preflight read.
	if strings.HasPrefix(c.Action, "meta.") && c.Action != "meta.create" && c.ExpectedRevision == 0 {
		current, err := meta(ctx, c.Kind, c.ID.ID, c.Action == "meta.restore")
		if err != nil {
			return Result{}, err
		}
		c.ExpectedRevision = current.Revision
	}
	if c.Action == "meta.resourcePatch" {
		var operation resourcePatch
		_ = json.Unmarshal(c.Data, &operation)
		if operation.Operation == "sourceBindingsStateUpdate" {
			ctx = context.WithValue(ctx, bindingLifecycleKey{}, true)
		}
		merged, err := mergeResourcePatch(ctx, c)
		if err != nil {
			return Result{OpID: c.OpID, Action: c.Action, Kind: c.Kind, ID: c.ID.ID}, err
		}
		result, err := s.metadataChange(ctx, tx, merged)
		result.Action = c.Action
		return result, err
	}
	if c.Action == "meta.patch" {
		merged, err := s.mergePatch(ctx, c)
		if err != nil {
			return Result{OpID: c.OpID, Action: c.Action, Kind: c.Kind, ID: c.ID.ID}, err
		}
		result, err := s.metadataChange(ctx, tx, merged)
		result.Action = "meta.patch"
		return result, err
	}
	result := Result{OpID: c.OpID, Action: c.Action, Kind: c.Kind, ID: c.ID.ID, Status: "created"}
	if c.Action == "stream.publish" {
		row, err := meta(ctx, "stream", c.StreamID.ID, false)
		if err != nil {
			return result, err
		}
		if c.ExpectedRevision != 0 && row.Revision != c.ExpectedRevision {
			return result, problem("VERSION_CONFLICT", "Stream revision changed")
		}
		c.ExpectedRevision = row.Revision
		var data StreamData
		if json.Unmarshal(row.Data, &data) != nil {
			return result, invalid("streamId", "not a reusable Stream")
		}
		if err := validateStream(data); err != nil {
			return result, err
		}
		release := 1
		for key := range data.Releases {
			n, _ := strconv.Atoi(key)
			if n >= release {
				release = n + 1
			}
		}
		if data.Releases == nil {
			data.Releases = map[string]Release{}
		}
		data.Releases[strconv.Itoa(release)] = Release{PublishedAt: time.Now().UnixMilli(), Definition: *data.Draft}
		c.Kind = "stream"
		c.ID.ID = c.StreamID.ID
		c.Action = "meta.replace"
		saved, err := writeMeta(tx, c, data)
		result.Kind = "stream"
		result.ID = saved.ID
		result.Revision = saved.Revision
		result.Release = release
		result.Status = "published"
		return result, err
	}
	if c.Kind == "directory" || c.Kind == "topic" || c.Kind == "collection" {
		kind := c.Kind
		if kind == "directory" {
			kind = "folder"
		}
		var row repo.UnsMeta
		var err error
		if c.Action == "meta.copy" {
			var target struct {
				Name        string  `json:"name,omitempty"`
				Description *string `json:"description,omitempty"`
				ParentID    string  `json:"parentId,omitempty"`
			}
			if err := decodeChangeData(c.Data, &target); err != nil {
				return result, err
			}
			row, err = s.metadata.Copy(ctx, c.ID.ID, c.ExpectedRevision, target.ParentID, target.Name, target.Description, c.Kind)
			result.ID = row.ID
			result.Revision = row.Revision
			result.Status = "created"
			return result, err
		}
		if c.Action == "meta.archive" || c.Action == "meta.restore" {
			row, err = s.metadata.State(ctx, c.ID.ID, c.ExpectedRevision, c.Action == "meta.restore", c.Kind)
			result.Status = "archived"
			if c.Action == "meta.restore" {
				result.Status = "restored"
			}
		} else {
			allowed := []string{"schemaVersion", "name", "description"}
			switch c.Kind {
			case "directory":
				allowed = append(allowed, "parentId")
			case "topic":
				allowed = append(allowed, "parentId", "viewMode", "collectionId", "fields")
			case "collection":
				allowed = append(allowed, "parentId", "recordSchema", "sampleSchema", "indexes", "semantics")
			}
			var fields map[string]json.RawMessage
			if err := decodeChangeData(c.Data, &fields); err != nil {
				return result, err
			}
			for key := range fields {
				if !contains(allowed, key) {
					return result, invalid("data/"+key, "field is not allowed for this metadata kind")
				}
			}
			var doc unsmeta.Document
			if err = decodeChangeData(c.Data, &doc); err != nil {
				return result, err
			}
			if doc.SchemaVersion != 1 {
				return result, invalid("schemaVersion", "schemaVersion must be 1")
			}
			if c.Kind == "topic" && doc.ViewMode == "" {
				return result, invalid("viewMode", "records or fields required")
			}
			if c.Kind == "collection" {
				if err = ValidateCollectionRelationships(ctx, c.ID.ID, doc); err != nil {
					return result, err
				}
			}
			row, err = s.metadata.Save(ctx, c.ID.ID, kind, c.ExpectedRevision, doc)
		}
		result.ID = row.ID
		result.Revision = row.Revision
		if c.Action == "meta.replace" {
			result.Status = "updated"
		}
		return result, err
	}
	if c.Kind == "source" && (c.Action == "meta.archive" || c.Action == "meta.restore") {
		return s.sharedSourceState(ctx, tx, c)
	}
	if c.Action != "meta.create" && c.Action != "meta.replace" {
		return result, invalid("action", "configuration archive requires its domain lifecycle API")
	}
	var old repo.UnsMeta
	if c.Action == "meta.replace" {
		var err error
		old, err = meta(ctx, c.Kind, c.ID.ID, false)
		if err != nil {
			return result, err
		}
		if old.Revision != c.ExpectedRevision {
			return result, problem("VERSION_CONFLICT", "configuration revision changed")
		}
	}
	var payload any
	switch c.Kind {
	case "stream":
		var data StreamData
		if err := decodeChangeData(c.Data, &data); err != nil {
			return result, err
		}
		if len(data.Releases) > 0 {
			return result, invalid("releases", "published releases are read-only")
		}
		if err := validateStream(data); err != nil {
			return result, err
		}
		if old.ID != "" {
			var previous StreamData
			if json.Unmarshal(old.Data, &previous) != nil || previous.SchemaVersion != 1 || previous.Draft == nil {
				return result, invalid("id", "complete programs use their existing API")
			}
			data.Releases = previous.Releases
		}
		payload = data
	case "source":
		var data SourceData
		if err := decodeChangeData(c.Data, &data); err != nil {
			return result, err
		}
		if data.ConfigRevision != 0 {
			return result, invalid("data", "configRevision is read-only")
		}
		var previous *SourceData
		if old.ID != "" {
			previous = &SourceData{}
			if json.Unmarshal(old.Data, previous) != nil || previous.SchemaVersion != 1 {
				return result, invalid("id", "legacy Source configuration uses its existing API")
			}
		}
		if err := validateSource(context.WithValue(ctx, credentialSourceKey{}, c.ID.ID), &data, previous); err != nil {
			return result, err
		}
		data.ConfigRevision = 1
		if previous != nil {
			data.ConfigRevision = previous.ConfigRevision + 1
		}
		payload = data
	case "app":
		var data AppData
		if err := decodeChangeData(c.Data, &data); err != nil {
			return result, err
		}
		if err := preserveAppStreamPolicy(ctx, old, &data); err != nil {
			return result, err
		}
		if subject, ok := contextx.SubjectFrom(ctx); ok && subject.MCPRestricted {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(c.Data, &fields)
			var previous AppData
			_ = json.Unmarshal(old.Data, &previous)
			if _, supplied := fields["platformPermissions"]; supplied || len(previous.PlatformPermissions) > 0 {
				return result, problem("FORBIDDEN", "MCP cannot manage App platform permissions")
			}
		}
		if err := validateApp(ctx, data); err != nil {
			return result, err
		}
		if IsNativeAppEntry(c.ID.ID, data) {
			roles := make([]repo.BusinessRole, 0, len(data.Roles))
			for _, r := range data.Roles {
				roles = append(roles, repo.BusinessRole{Key: r.ID, Name: r.Name})
			}
			if err := repo.RegisterBusinessRoles(tx, roles); err != nil {
				return result, err
			}
		}
		payload = data
	default:
		return result, invalid("kind", "kind is read-only or unsupported")
	}
	row, err := writeMeta(tx, c, payload)
	if err != nil {
		return result, err
	}
	if c.Kind == "source" && c.Action == "meta.create" {
		state, _ := json.Marshal(map[string]any{"schemaVersion": 1, "health": "unverified", "resources": []any{}, "streams": map[string]any{}})
		err = tx.Create(&repo.UnsMeta{Kind: "source_state", ID: row.ID, Data: state, Revision: 1, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}).Error
	}
	if c.Kind == "source" && old.ID != "" {
		var previous map[string]json.RawMessage
		_ = json.Unmarshal(old.Data, &previous)
		if credentials, ok := previous["credentials"]; ok {
			err = tx.Exec("UPDATE public.uns_meta SET data=jsonb_set(data,'{credentials}',?::jsonb) WHERE kind='source' AND id=?", string(credentials), row.ID).Error
		}
	}
	result.Revision = row.Revision
	if c.Action == "meta.replace" {
		result.Status = "updated"
	}
	return result, err
}

func (s *Service) dataChange(ctx context.Context, tx *gorm.DB, c Change) (Result, error) {
	ctx = unsmeta.WithRecordLease(ctx, c.LockToken, c.Release)
	result := Result{OpID: c.OpID, Action: c.Action, ID: c.ID.ID, CollectionID: c.CollectionID.ID, Status: "created"}
	if !unsmeta.ValidID(c.CollectionID.ID) || !unsmeta.ValidID(c.ID.ID) {
		return result, invalid("id", "valid collection and data IDs required")
	}
	if (c.Action == "record.replace" || c.Action == "record.delete") && c.ExpectedRevision == 0 {
		var current repo.UnsRecord
		if err := tx.Where("collection_id=? AND id=?", c.CollectionID.ID, c.ID.ID).Take(&current).Error; err != nil {
			return result, err
		}
		c.ExpectedRevision = current.Revision
	}
	var before repo.UnsRecord
	if c.ReturnDocument == "before" {
		if err := tx.Where("collection_id=? AND id=? AND deleted_at IS NULL", c.CollectionID.ID, c.ID.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&before).Error; err != nil {
			return result, err
		}
	}
	switch c.Action {
	case "record.restore":
		if err := repo.RequireLocalCollection(ctx, c.CollectionID.ID); err != nil {
			return result, err
		}
		var old repo.UnsRecord
		if err := tx.Unscoped().Where("collection_id=? AND id=?", c.CollectionID.ID, c.ID.ID).Take(&old).Error; err != nil {
			return result, err
		}
		row, err := s.metadata.RestoreRecord(ctx, c.CollectionID.ID, c.ID.ID)
		result.Status = "unchanged"
		if old.DeletedAt.Valid {
			result.Status = "restored"
		}
		result.Revision = row.Revision
		return result, err
	case "record.create", "record.replace":
		if c.Action == "record.replace" || c.ManagedBy != nil || c.ReturnDocument != "" {
			if err := repo.RequireLocalCollection(ctx, c.CollectionID.ID); err != nil {
				return result, err
			}
		}
		var data map[string]any
		if err := decodeChangeData(c.Data, &data); err != nil || data == nil {
			return result, invalid("data", "record object required")
		}
		if c.Action == "record.create" && c.ExpectedRevision != 0 || c.Action == "record.replace" && c.ExpectedRevision < 1 {
			return result, problem("VERSION_CONFLICT", "invalid record revision")
		}
		if doc, err := definition(ctx, c.CollectionID.ID); err == nil && doc.RecordSchema != nil {
			if path, reason := doc.RecordSchema.Explain(data); reason != "" {
				return result, &Fault{Code: "SCHEMA_MISMATCH", Path: "data" + path, Reason: reason, Message: reason, Repair: RepairHint("SCHEMA_MISMATCH")}
			}
		}
		row, err := s.metadata.SaveRecord(ctx, c.CollectionID.ID, c.ID.ID, c.ExpectedRevision, data)
		if err != nil {
			return result, err
		}
		if c.ManagedBy != nil {
			if c.Action != "record.create" {
				return result, invalid("managedBy", "existing records cannot be claimed")
			}
			source, err := meta(ctx, "source", c.ManagedBy.SourceID.ID, false)
			if err != nil {
				return result, err
			}
			var d SourceData
			if json.Unmarshal(source.Data, &d) != nil {
				return result, invalid("managedBy", "incompatible Source")
			}
			b, ok := d.Config.Streams[c.ManagedBy.InstanceID]
			if !ok || b.Target.CollectionID != row.CollectionID || b.Mapping.RecordID != row.ID {
				return result, invalid("managedBy", "Source instance does not target this record")
			}
			id := source.ID
			row.SourceID = &id
			if err := tx.Model(&row).UpdateColumns(map[string]any{"source_id": id, "updated_at": row.UpdatedAt}).Error; err != nil {
				return result, err
			}
			result.SourceID = &id
		}
		result.Revision = row.Revision
		if p, e := repo.CollectionStorage(ctx, c.CollectionID.ID); e != nil {
			return result, e
		} else if p.Mode != "local" {
			result.Status = "pendingUpload"
		}
		if c.Action == "record.replace" {
			result.Status = "updated"
		}
		if c.ReturnDocument == "before" {
			result.Document = recordDocument(before)
		} else if c.ReturnDocument == "after" {
			result.Document = recordDocument(row)
		}
		return result, nil
	case "record.delete":
		if err := repo.RequireLocalCollection(ctx, c.CollectionID.ID); err != nil {
			return result, err
		}
		result.Status = "deleted"
		if c.ReturnDocument == "before" {
			result.Document = recordDocument(before)
		}
		result.Revision = c.ExpectedRevision + 1
		return result, s.metadata.DeleteRecord(ctx, c.CollectionID.ID, c.ID.ID, c.ExpectedRevision)
	case "sample.append":
		if !unsmeta.ValidID(c.RecordID.ID) {
			return result, invalid("recordId", "record required")
		}
		var data map[string]any
		if err := decodeChangeData(c.Data, &data); err != nil {
			return result, err
		}
		if doc, err := definition(ctx, c.CollectionID.ID); err == nil && doc.SampleSchema != nil {
			if path, reason := doc.SampleSchema.Explain(data); reason != "" {
				return result, &Fault{Code: "SCHEMA_MISMATCH", Path: "data" + path, Reason: reason, Message: reason, Repair: RepairHint("SCHEMA_MISMATCH")}
			}
		}
		_, err := s.metadata.AppendSamples(ctx, c.CollectionID.ID, []unsmeta.SampleInput{{ID: c.ID.ID, RecordID: c.RecordID.ID, Time: c.Time, Quality: c.Quality, Data: data}})
		result.Status = "appended"
		if p, e := repo.CollectionStorage(ctx, c.CollectionID.ID); e != nil {
			return result, e
		} else if p.Mode != "local" {
			result.Status = "pendingUpload"
		}
		return result, err
	default:
		return result, invalid("action", "unsupported data action")
	}
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "operation budget expired or request canceled; retry using the same key", Retryable: true}
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if budget := expressionRuntimeError(pg.Message); budget != nil {
			return budget
		}
		switch pg.Code {
		case "22P02", "22003", "22007", "22008", "22012", "22023":
			// Expressions are also accepted in transactional CRUD selectors. Do
			// not expose PostgreSQL's value-bearing conversion diagnostics.
			return invalid("value", "expression input cannot be converted or exceeds the supported range")
		case "42501":
			if pg.ConstraintName == "sync_mount_readonly" {
				return problem("FORBIDDEN", "mounted UNS resources and their data are read-only")
			}
		case "23505":
			if strings.HasPrefix(pg.ConstraintName, "ux_") {
				return problem("DUPLICATE_KEY", "Collection unique index conflicts with an active Record")
			}
			return problem("DUPLICATE", "object identity or name exists")
		case "23503":
			return problem("REFERENCED", "object has retained data or a required reference is missing")
		case "23514":
			if strings.Contains(pg.Message, "COLLECTION_STORAGE_UNSUPPORTED") {
				return classify(repo.ErrStorageOperation)
			}
			return problem("SCHEMA_MISMATCH", "database constraint rejected the data")
		case "40001", "40P01", "55P03", "57014":
			return &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "concurrent change; retry using the same key", Retryable: true}
		}
	}
	var f *Fault
	if errors.As(err, &f) {
		return f
	}
	switch {
	case errors.Is(err, repo.ErrStorageOperation):
		return problem("UNSUPPORTED_OPERATION", "Operation cannot preserve Collection storage semantics; use complete record writes and sample appends")
	case errors.Is(err, repo.ErrStorageDefinitionPending):
		return &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "Collection definition must synchronize with center before remote writes", Retryable: true}
	case errors.Is(err, repo.ErrStorageUnavailable):
		return &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "Center query failed; local history is not a fallback", Retryable: true}
	case errors.Is(err, unsmeta.ErrRecordLocked):
		return problem("LOCK_CONFLICT", "record lease is missing, expired or owned by another principal")
	case errors.Is(err, repo.ErrMountedReadOnly):
		return problem("FORBIDDEN", "mounted UNS resources and their data are read-only")
	case errors.Is(err, repo.ErrNotFound):
		return problem("NOT_FOUND", "object not found")
	case errors.Is(err, repo.ErrConflict):
		return problem("VERSION_CONFLICT", "object identity or revision conflicts")
	case errors.Is(err, repo.ErrDuplicate):
		return problem("DUPLICATE", "object identity or name exists")
	case errors.Is(err, unsmeta.ErrTrashDirectory):
		return &Fault{Code: "CONDITION_FAILED", Reason: "DIRECTORY_TRASH_ROOT_REQUIRED", Message: "restore the original directory before its descendants"}
	case errors.Is(err, unsmeta.ErrPurgeChanged):
		return &Fault{Code: "CONDITION_FAILED", Reason: "PURGE_IN_PROGRESS", Message: "object is already being permanently deleted"}
	case errors.Is(err, unsmeta.ErrReferenced):
		return problem("REFERENCED", "object is referenced by configuration")
	case errors.Is(err, unsmeta.ErrSchemaInUse):
		return problem("SCHEMA_INCOMPATIBLE", "schema change would reject retained data; only widening changes are allowed while records exist")
	case errors.As(err, new(*unsmeta.SchemaMismatchError)):
		var mismatch *unsmeta.SchemaMismatchError
		errors.As(err, &mismatch)
		return &Fault{Code: "SCHEMA_MISMATCH", Path: "data" + mismatch.Path, Reason: mismatch.Reason, Message: mismatch.Reason, Repair: RepairHint("SCHEMA_MISMATCH")}
	case errors.Is(err, unsmeta.ErrData), errors.Is(err, unsmeta.ErrSchema), errors.Is(err, unsmeta.ErrInvalid):
		return problem("SCHEMA_MISMATCH", "invalid definition or data")
	case errors.Is(err, unsmeta.ErrCycle):
		return problem("CYCLIC_DEPENDENCY", "tree cycle")
	}
	return fmt.Errorf("UNS operation failed: %w", err)
}
