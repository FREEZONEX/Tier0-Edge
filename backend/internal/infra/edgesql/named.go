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
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// Named queries are Source-owned statements. PostgreSQL enforces read-only
// transactions, including writable CTEs and functions, independently of SQL text.
func OpenNamed(endpoint *url.URL, username, password, mode string, trust *tls.Config) (*sql.DB, error) {
	if mode != "" && mode != "disable" && mode != "verify-full" {
		return nil, errors.New("invalid TLS mode")
	}
	if endpoint.Scheme == "sqlserver" {
		if mode != "disable" {
			return Open(endpoint, username, password, trust)
		}
		address := *endpoint
		address.Path = ""
		address.RawQuery = url.Values{"database": {strings.TrimPrefix(endpoint.Path, "/")}, "encrypt": {"disable"}, "connection timeout": {"5"}}.Encode()
		cfg, err := msdsn.Parse(address.String())
		if err != nil {
			return nil, err
		}
		cfg.User, cfg.Password = username, password
		return sql.OpenDB(mssql.NewConnectorConfig(cfg)), nil
	}
	if endpoint.Scheme == "mysql" {
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.DBName = username, password, strings.TrimPrefix(endpoint.Path, "/")
		cfg.Net, cfg.Addr, cfg.ParseTime = "tcp", endpoint.Host, true
		cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 10*time.Second, 10*time.Second
		cfg.Params = map[string]string{"wait_timeout": "5", "max_execution_time": "10000"}
		if mode != "disable" {
			if trust == nil || trust.InsecureSkipVerify || trust.ServerName != endpoint.Hostname() {
				return nil, errors.New("verified TLS required")
			}
			cfg.TLS = trust.Clone()
		}
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, err
		}
		db := sql.OpenDB(connector)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		return db, nil
	}
	if endpoint.Scheme != "postgresql" {
		return nil, errors.New("unsupported driver")
	}
	if mode != "disable" {
		return Open(endpoint, username, password, trust)
	}
	configURL := *endpoint
	configURL.RawQuery = "sslmode=disable"
	cfg, err := pgx.ParseConfig(configURL.String())
	if err != nil {
		return nil, err
	}
	cfg.User, cfg.Password = username, password
	cfg.TLSConfig, cfg.Fallbacks = nil, nil
	cfg.RuntimeParams = map[string]string{"default_transaction_read_only": "on"}
	return stdlib.OpenDB(*cfg), nil
}

func ReadNamed(ctx context.Context, db *sql.DB, query string, limit int, dialect ...string) ([]map[string]any, []string, bool, error) {
	if limit < 1 || limit > 100 {
		return nil, nil, false, errors.New("invalid row limit")
	}
	readOnly := len(dialect) == 0 || dialect[0] != "sqlserver"
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	if deadline, ok := ctx.Deadline(); ok && (len(dialect) == 0 || dialect[0] == "postgresql") {
		remaining := time.Until(deadline).Milliseconds()
		if remaining < 1 {
			return nil, nil, false, context.DeadlineExceeded
		}
		if _, err = tx.ExecContext(ctx, "SELECT set_config('statement_timeout', $1, true)", strconv.FormatInt(remaining, 10)+"ms"); err != nil {
			return nil, nil, false, err
		}
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, false, err
	}
	seen := map[string]bool{}
	for _, column := range columns {
		if seen[column] {
			return nil, nil, false, errors.New("duplicate result column")
		}
		seen[column] = true
	}
	items := []map[string]any{}
	bytes := 0
	for rows.Next() {
		if len(items) == limit {
			return items, columns, true, nil
		}
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return nil, nil, false, err
		}
		item := map[string]any{}
		for i, column := range columns {
			switch value := values[i].(type) {
			case []byte:
				values[i] = string(value)
			case int64:
				if value > 9007199254740991 || value < -9007199254740991 {
					values[i] = strconv.FormatInt(value, 10)
				}
			case time.Time:
				values[i] = value.UTC().Format(time.RFC3339Nano)
			}
			item[column] = values[i]
		}
		encoded, encodeErr := json.Marshal(item)
		if encodeErr != nil {
			return nil, nil, false, encodeErr
		}
		bytes += len(encoded)
		if bytes > 1024*1024 {
			return nil, nil, false, ErrResultLimit
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, false, err
	}
	return items, columns, false, nil
}
