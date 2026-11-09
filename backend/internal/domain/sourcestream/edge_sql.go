package sourcestream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"backend/internal/infra/edgesql"
	"backend/internal/repo"
)

const EdgeSQL Protocol = "edge-sql"

var sqlDatabasePath = regexp.MustCompile(`^/[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type SQLConnection struct {
	Queries       map[string]string   `json:"queries,omitempty"`
	TLSMode       string              `json:"tlsMode,omitempty"`
	Query         string              `json:"query,omitempty"`
	CACertificate string              `json:"caCertificate,omitempty"`
	WMS           *edgesql.WMSMapping `json:"wms,omitempty"`
}

func validateSQLConnection(def SourceDefinition, u *url.URL) error {
	if def.SQL == nil || def.HTTP != nil || def.MQTT != nil || def.OPCUA != nil || def.Modbus != nil || def.ConnectionRole() != ClientRole {
		return invalid("sql", "protocolConfigMismatch")
	}
	if len(def.SQL.Queries) > 0 {
		if u.Scheme != "postgresql" && u.Scheme != "mysql" && u.Scheme != "sqlserver" {
			return invalid("sql.queries", "unsupportedCapability")
		}
		if def.SQL.Query != "" || def.SQL.WMS != nil || len(def.SQL.Queries) > 64 || u.Port() == "" || !sqlDatabasePath.MatchString(u.EscapedPath()) {
			return invalid("sql.queries", "invalidOrConflicting")
		}
		for name, query := range def.SQL.Queries {
			invalidName := name == "" || len(name) > 128 || strings.Contains(name, "://") || strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
			if invalidName || strings.TrimSpace(query) == "" || len(query) > 32768 || strings.ContainsRune(query, '\x00') {
				return invalid("sql.queries", "invalid")
			}
		}
		if def.SQL.TLSMode != "" && def.SQL.TLSMode != "verify-full" && def.SQL.TLSMode != "disable" {
			return invalid("sql.tlsMode", "invalid")
		}
		if def.SQL.TLSMode == "disable" && def.SQL.CACertificate != "" {
			return invalid("sql.caCertificate", "verifiedTransportRequired")
		}
		if _, err := publicCertificates(def.SQL.CACertificate, true); err != nil {
			return invalid("sql.caCertificate", "invalid")
		}
		return nil
	}
	if def.SQL.TLSMode != "" && def.SQL.TLSMode != "verify-full" {
		return invalid("sql.tlsMode", "unsupportedCapability")
	}
	if u.Port() == "" || !sqlDatabasePath.MatchString(u.EscapedPath()) || edgesql.Dialect(def.SQL.Query) == "" || u.Scheme != edgesql.Dialect(def.SQL.Query) {
		return invalid("sql.query", "endpointOrQueryInvalid")
	}
	if def.SQL.Query == edgesql.WMSInventory || def.SQL.Query == edgesql.WMSMovements {
		if err := def.SQL.WMS.Validate(); err != nil {
			return invalid("sql.wms", "invalidMapping")
		}
	} else if def.SQL.WMS != nil {
		return invalid("sql.wms", "queryMismatch")
	}
	if _, err := publicCertificates(def.SQL.CACertificate, true); err != nil {
		return invalid("sql.caCertificate", "invalid")
	}
	return nil
}

func edgeSQLResources(def SourceDefinition, revision int64) []edgeResource {
	if def.Protocol != EdgeSQL || def.SQL == nil {
		return nil
	}
	if len(def.SQL.Queries) > 0 {
		names := make([]string, 0, len(def.SQL.Queries))
		for name := range def.SQL.Queries {
			names = append(names, name)
		}
		sort.Strings(names)
		resources := make([]edgeResource, 0, len(names))
		for _, name := range names {
			resources = append(resources, edgeResource{ResourceRef: "sql:" + name, Name: name, Modes: []string{"read"}, Permission: "uns.source", ConfigRevision: revision, InputSchema: resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}), OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]string{"type": "object"}}, "columns": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "returned": map[string]string{"type": "integer"}, "truncated": map[string]string{"type": "boolean"}}), Metadata: map[string]any{"queryKind": "named", "transactionReadOnly": true, "effect": "readOnly", "previewSupported": true, "pagination": "none"}})
		}
		if potentiallyWritingSQL(def) {
			for i := range resources {
				resources[i].Modes = []string{}
				resources[i].Permission = "uns.source.manage"
				resources[i].Metadata["transactionReadOnly"] = false
				resources[i].Metadata["effect"] = "potentialWrite"
				resources[i].Metadata["previewSupported"] = false
				resources[i].Metadata["streamReadSupported"] = true
			}
		}
		return resources
	}
	key, order := "id", "id"
	incremental := def.SQL.Query == edgesql.Quality
	if def.SQL.Query == edgesql.Quality {
		key, order = "InspectionId", "SyncedAt,InspectionId"
	}
	if def.SQL.Query == edgesql.WMSSchema {
		key, order = "resourceKey", "resourceKey"
	}
	if def.SQL.WMS != nil {
		key, order = def.SQL.WMS.KeyColumn, def.SQL.WMS.KeyColumn
		incremental = def.SQL.WMS.UpdatedAt != ""
		if incremental {
			order = def.SQL.WMS.UpdatedAt + "," + key
		}
	}
	return []edgeResource{{ResourceRef: def.SQL.Query, Name: def.SQL.Query, Modes: []string{"read", "scan"}, Permission: "uns.source", ConfigRevision: revision,
		InputSchema: resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}}),
		OutputSchema: resourceObject(map[string]any{
			"items":      map[string]any{"type": "array", "items": map[string]string{"type": "object"}},
			"nextCursor": map[string]string{"type": "string"},
			"checkpoint": map[string]string{"type": "string"},
			"hasMore":    map[string]string{"type": "boolean"},
		}),
		Metadata: map[string]any{"identityField": key, "orderBy": order, "pagination": "keyset", "supportsIncremental": incremental}}}
}

func (s *Service) edgeSQLPage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems int, preview bool) (any, error) {
	if def.SQL != nil && len(def.SQL.Queries) > 0 {
		if !preview || cursor != "" || maxItems != 0 {
			return nil, invalid("mode", "unsupportedCapability")
		}
		return s.namedSQLRead(ctx, row, def, ref, limit)
	}
	if def.Protocol != EdgeSQL || def.SQL == nil || ref != def.SQL.Query {
		return nil, invalid("resourceRef", "undeclared")
	}
	if limit < 1 || limit > 100 || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("limit", "outOfRange")
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	pos := edgesql.Position{}
	remaining := maxItems
	if cursor != "" {
		page, err := s.openScanCursor(cursor, scope)
		if err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(page.Link), &pos) != nil || pos.Key == "" {
			return nil, invalid("cursor", "invalidOrStale")
		}
		remaining = page.Remaining
	}
	if maxItems > 0 {
		limit = min(limit, remaining)
	}
	source, cred, _, err := s.connection(ctx, row.ID, nil)
	if err != nil {
		return nil, err
	}
	// Do not send credentials from a reconfigured Source to the old endpoint.
	if source.SQL == nil || source.Protocol != EdgeSQL || source.Endpoint != def.Endpoint || source.CredentialRef != def.CredentialRef || !reflect.DeepEqual(source.SQL, def.SQL) {
		return nil, repo.ErrConflict
	}
	if cred.Kind != "basic" {
		return nil, ErrCredential
	}
	u, err := url.Parse(def.Endpoint)
	if err != nil {
		return nil, ErrVendorResponse
	}
	trust := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if source.tlsConfig != nil {
		trust = source.tlsConfig.Clone()
	}
	if def.SQL.CACertificate != "" {
		if trust.RootCAs == nil {
			trust.RootCAs = x509.NewCertPool()
		}
		if !trust.RootCAs.AppendCertsFromPEM([]byte(def.SQL.CACertificate)) {
			return nil, ErrCredential
		}
	}
	db, err := edgesql.Open(u, cred.Username, cred.Secret, trust)
	if err != nil {
		return nil, safeSQLReadError(ctx, err)
	}
	defer db.Close()
	result, err := edgesql.ReadConfigured(ctx, db, ref, pos, limit, def.SQL.WMS)
	if err != nil {
		return nil, safeSQLReadError(ctx, err)
	}
	reply := map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "items": result.Items, "collectedAt": time.Now().UnixMilli()}
	if preview {
		reply["returned"], reply["truncated"] = len(result.Items), result.More
		return reply, nil
	}
	if maxItems > 0 {
		remaining -= len(result.Items)
	}
	more := result.More && (maxItems == 0 || remaining > 0)
	token := cursor
	if len(result.Items) > 0 {
		position, _ := json.Marshal(result.Last)
		token, err = s.nextScanCursor(string(position), remaining, scope)
		if err != nil {
			return nil, err
		}
	}
	reply["hasMore"], reply["nextCursor"] = more, ""
	if more {
		reply["nextCursor"] = token
	}
	if maxItems == 0 && (ref == edgesql.Quality || def.SQL.WMS != nil && def.SQL.WMS.UpdatedAt != "") {
		reply["checkpoint"] = token
	}
	return reply, nil
}
