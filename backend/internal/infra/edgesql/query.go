// Package edgesql executes a closed set of read-only, parameterized queries.
package edgesql

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

const Tools = "sql:tool-inventory"
const ToolLoans = "sql:tool-loans"
const ToolSharpenings = "sql:tool-sharpenings"
const ToolSuppliers = "sql:tool-suppliers"
const WMSSchema = "sql:wms-schema"
const WMSInventory = "sql:wms-inventory"
const WMSMovements = "sql:wms-movements"
const Quality = "sql:quality-inspections"

// Dialect is the closed named-query allowlist used by Source validation.
func Dialect(query string) string {
	switch query {
	case Tools, ToolLoans, ToolSharpenings, ToolSuppliers, WMSSchema, WMSInventory, WMSMovements:
		return "postgresql"
	case Quality:
		return "sqlserver"
	}
	return ""
}

type Position struct {
	Stamp string
	Key   string
}

type Page struct {
	Items []map[string]any
	Last  Position
	More  bool
}

// Connection accepts a caller-owned, credential-leased SQL connection as well
// as a pool. Query text remains the same closed, approved resource set.
type Connection interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Open requires authenticated transport in either dialect. No fallback socket
// is available to pgx when the peer declines the SSLRequest.
func Open(endpoint *url.URL, username, password string, trust *tls.Config) (*sql.DB, error) {
	if trust == nil || trust.InsecureSkipVerify || trust.ServerName != endpoint.Hostname() || trust.MinVersion < tls.VersionTLS12 {
		return nil, errors.New("verified TLS required")
	}
	switch endpoint.Scheme {
	case "postgresql":
		configURL := *endpoint
		configURL.RawQuery = "sslmode=require"
		cfg, err := pgx.ParseConfig(configURL.String())
		if err != nil {
			return nil, err
		}
		cfg.User, cfg.Password = username, password
		cfg.TLSConfig, cfg.Fallbacks = trust, nil
		cfg.RuntimeParams = map[string]string{"default_transaction_read_only": "on"}
		return stdlib.OpenDB(*cfg), nil
	case "sqlserver":
		configURL := *endpoint
		configURL.Path = ""
		configURL.RawQuery = url.Values{"database": {strings.TrimPrefix(endpoint.Path, "/")}, "encrypt": {"true"}}.Encode()
		cfg, err := msdsn.Parse(configURL.String())
		if err != nil {
			return nil, err
		}
		cfg.User, cfg.Password = username, password
		cfg.TLSConfig = trust
		cfg.HostInCertificateProvided = true
		return sql.OpenDB(mssql.NewConnectorConfig(cfg)), nil
	}
	return nil, errors.New("unsupported driver")
}

// Read uses one extra row to detect a following page, never OFFSET. SQL
// Server returns its exact six-digit datetimeoffset as text for rebinding.
func Read(ctx context.Context, db Connection, query string, after Position, limit int) (Page, error) {
	return ReadConfigured(ctx, db, query, after, limit, nil)
}

func ReadConfigured(ctx context.Context, db Connection, query string, after Position, limit int, mapping *WMSMapping) (Page, error) {
	result := Page{Items: make([]map[string]any, 0), Last: after}
	if limit < 1 || limit > 100 {
		return result, errors.New("invalid page size")
	}
	var rows *sql.Rows
	var err error
	var finish func()
	switch query {
	case Tools, ToolLoans, ToolSharpenings, ToolSuppliers:
		var last any
		if after.Key != "" {
			last, err = strconv.ParseInt(after.Key, 10, 32)
			if err != nil {
				return result, err
			}
		}
		statements := map[string]string{
			Tools:           "SELECT id,djbh,mc,gg,cz,sl,dw,cfwz,jg,gys,lrrq,bz FROM public.djb",
			ToolLoans:       "SELECT id,djbh,llr,jt,gdh,llsj,thsj,sl,bz FROM public.llb",
			ToolSharpenings: "SELECT id,djbh,mdrq,mdcs,czr,bz FROM public.mdb",
			ToolSuppliers:   "SELECT id,gysmc,lxr,dh,bz FROM public.gysb",
		}
		rows, err = db.QueryContext(ctx, statements[query]+`
			WHERE ($1::integer IS NULL OR id > $1) ORDER BY id LIMIT $2`, last, limit+1)
	case WMSSchema:
		rows, err = db.QueryContext(ctx, wmsSchemaSQL, after.Key, limit+1)
	case WMSInventory, WMSMovements:
		rows, finish, err = readWMS(ctx, db, mapping, after, limit+1)
	case Quality:
		var lastStamp any
		if after.Key != "" {
			lastStamp = after.Stamp
		}
		rows, err = db.QueryContext(ctx, `SELECT TOP (@take) InspectionId, WorkOrder, Material, OperationNo, InspectedAt,
			InspectionType, Result, SampleCount, FailCount, Inspector, IsDeleted, CONVERT(varchar(40),SyncedAt,127) AS SyncedAt
			FROM reporting.Inspection
			WHERE @stamp IS NULL OR SyncedAt > CONVERT(datetimeoffset(6),@stamp)
				OR (SyncedAt = CONVERT(datetimeoffset(6),@stamp) AND InspectionId > @lastKey)
			ORDER BY SyncedAt, InspectionId`, sql.Named("take", limit+1), sql.Named("stamp", lastStamp), sql.Named("lastKey", after.Key))
	default:
		return result, errors.New("unknown named query")
	}
	if err != nil {
		return result, err
	}
	if finish != nil {
		defer finish()
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return result, err
	}
	resultBytes := 0
	for rows.Next() {
		if len(result.Items) == limit {
			result.More = true
			break
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return result, err
		}
		item := make(map[string]any, len(columns))
		for i, name := range columns {
			if raw, ok := values[i].([]byte); ok {
				values[i] = string(raw)
			}
			item[name] = values[i]
		}
		switch query {
		case Tools, ToolLoans, ToolSharpenings, ToolSuppliers:
			id, ok := item["id"].(int64)
			if !ok {
				return result, errors.New("invalid tool ID")
			}
			result.Last.Key = strconv.FormatInt(id, 10)
		case WMSSchema:
			result.Last.Key, _ = item["resourceKey"].(string)
			if result.Last.Key == "" {
				return result, errors.New("invalid schema identity")
			}
		case WMSInventory, WMSMovements:
			result.Last.Key, _ = item[mapping.KeyColumn].(string)
			if result.Last.Key == "" {
				return result, errors.New("invalid WMS identity")
			}
			if mapping.UpdatedAt != "" {
				result.Last.Stamp, _ = item[mapping.UpdatedAt].(string)
				if result.Last.Stamp == "" {
					return result, errors.New("invalid WMS watermark")
				}
			}
		case Quality:
			key, k := item["InspectionId"].(string)
			stamp, t := item["SyncedAt"].(string)
			if !k || !t || key == "" || stamp == "" {
				return result, errors.New("invalid quality watermark")
			}
			result.Last = Position{Stamp: stamp, Key: key}
		}
		encoded, e := json.Marshal(item)
		if e != nil {
			return result, e
		}
		resultBytes += len(encoded)
		if resultBytes > 1<<20 {
			return result, ErrResultLimit
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}
