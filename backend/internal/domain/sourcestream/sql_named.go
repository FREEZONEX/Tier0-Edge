package sourcestream

import (
	"backend/internal/contextx"
	"backend/internal/infra/edgesql"
	"backend/internal/repo"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"net/url"
	"reflect"
	"strings"
	"time"
)

type sqlProgramExecutionKey struct{}

func potentiallyWritingSQL(def SourceDefinition) bool {
	u, err := url.Parse(def.Endpoint)
	return err == nil && def.Protocol == EdgeSQL && def.SQL != nil && len(def.SQL.Queries) > 0 && u.Scheme == "sqlserver"
}

func openNamedSQL(def SourceDefinition, credential Credential) (*sql.DB, string, error) {
	endpoint, err := url.Parse(def.Endpoint)
	if err != nil || def.SQL == nil || credential.Kind != "basic" {
		return nil, "", ErrCredential
	}
	trust := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()}
	if def.tlsConfig != nil {
		trust = def.tlsConfig.Clone()
	}
	if def.SQL.CACertificate != "" {
		if trust.RootCAs == nil {
			trust.RootCAs = x509.NewCertPool()
		} else {
			trust.RootCAs = trust.RootCAs.Clone()
		}
		if !trust.RootCAs.AppendCertsFromPEM([]byte(def.SQL.CACertificate)) {
			return nil, "", ErrCredential
		}
	}
	db, err := edgesql.OpenNamed(endpoint, credential.Username, credential.Secret, def.SQL.TLSMode, trust)
	if err != nil {
		return nil, "", safeSQLReadError(context.Background(), err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	return db, endpoint.Scheme, nil
}

func probeNamedSQL(ctx context.Context, def SourceDefinition, credential Credential) error {
	db, _, err := openNamedSQL(def, credential)
	if err != nil {
		return err
	}
	defer db.Close()
	return safeSQLReadError(ctx, db.PingContext(ctx))
}

func (s *Service) namedSQLRead(ctx context.Context, row repo.Source, def SourceDefinition, ref string, limit int) (any, error) {
	name, ok := strings.CutPrefix(ref, "sql:")
	if !ok || def.SQL == nil || def.SQL.Queries[name] == "" {
		return nil, invalid("resourceRef", "undeclared")
	}
	current, credential, _, err := s.connection(ctx, row.ID, nil)
	if err != nil {
		return nil, err
	}
	if current.Endpoint != def.Endpoint || current.CredentialRef != def.CredentialRef || !reflect.DeepEqual(current.SQL, def.SQL) {
		return nil, repo.ErrConflict
	}
	if credential.Kind != "basic" {
		return nil, ErrCredential
	}
	if potentiallyWritingSQL(def) {
		subject, ok := contextx.SubjectFrom(ctx)
		if ctx.Value(sqlProgramExecutionKey{}) != true {
			return nil, invalid("resourceRef", "unsupportedCapability")
		}
		if !ok || !contextx.HasResource(subject, "uns.source.manage") || subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only" {
			return nil, ErrForbidden
		}
	}
	db, dialect, err := openNamedSQL(current, credential)
	if err != nil {
		return nil, safeSQLReadError(ctx, err)
	}
	defer db.Close()
	items, columns, truncated, err := edgesql.ReadNamed(ctx, db, def.SQL.Queries[name], limit, dialect)
	if err != nil {
		return nil, safeSQLReadError(ctx, err)
	}
	return map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "items": items, "columns": columns, "returned": len(items), "truncated": truncated, "collectedAt": time.Now().UnixMilli()}, nil
}
