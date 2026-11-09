package edgesql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// WMSMapping is approved on the Edge Source. Callers only select the named
// resource and cannot override the relation, projection or watermark.
type WMSMapping struct {
	Table     string   `json:"table"`
	Fields    []string `json:"fields"`
	KeyColumn string   `json:"keyColumn"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
}

var sqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func (m *WMSMapping) Validate() error {
	if m == nil || !sqlIdentifier.MatchString(m.Table) || !sqlIdentifier.MatchString(m.KeyColumn) || len(m.Fields) == 0 || len(m.Fields) > 64 {
		return errors.New("invalid WMS mapping")
	}
	seen := map[string]bool{}
	for _, field := range m.Fields {
		if !sqlIdentifier.MatchString(field) || seen[field] {
			return errors.New("invalid WMS projection")
		}
		seen[field] = true
	}
	if !seen[m.KeyColumn] || m.UpdatedAt != "" && (!seen[m.UpdatedAt] || m.UpdatedAt == m.KeyColumn) {
		return errors.New("WMS projection must include its key and watermark")
	}
	return nil
}

// schema is fixed, and all identifiers below come from Validate and live
// PostgreSQL catalog checks. Query values always use driver parameters.
func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

const wmsCatalogSQL = `SELECT c.relname,a.attname,t.typname,a.attnotnull,
 COALESCE((SELECT i.indnkeyatts FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid AND i.indisprimary),0),
 EXISTS(SELECT 1 FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid AND i.indisprimary AND a.attnum=ANY(i.indkey))
 FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
 WHERE n.nspname='wms' AND c.relname=$1 AND c.relkind IN ('r','p')
 AND a.attnum>0 AND NOT a.attisdropped AND has_table_privilege(c.oid,'SELECT')`

const wmsSchemaSQL = `SELECT (c.relname||':'||lpad(a.attnum::text,5,'0')) AS "resourceKey",
 c.relname AS "tableName", a.attname AS "columnName", t.typname AS "dataType",
 NOT a.attnotnull AS "nullable",
 EXISTS(SELECT 1 FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid AND i.indisprimary AND a.attnum=ANY(i.indkey)) AS "primaryKey",
 EXISTS(SELECT 1 FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid AND a.attnum=ANY(i.indkey) AND i.indisvalid) AS "indexed"
 FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
 WHERE n.nspname='wms' AND c.relkind IN ('r','p') AND a.attnum>0 AND NOT a.attisdropped
 AND has_table_privilege(c.oid,'SELECT')
 AND (c.relname||':'||lpad(a.attnum::text,5,'0')) COLLATE "C" > $1::text COLLATE "C"
 ORDER BY (c.relname||':'||lpad(a.attnum::text,5,'0')) COLLATE "C" LIMIT $2`

type wmsColumn struct {
	typeName       string
	notNull        bool
	primaryColumns int
	primary        bool
}

func wmsColumns(ctx context.Context, db *sql.Tx, table string) (map[string]wmsColumn, error) {
	rows, err := db.QueryContext(ctx, wmsCatalogSQL, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]wmsColumn{}
	for rows.Next() {
		var relation, name string
		var column wmsColumn
		if err := rows.Scan(&relation, &name, &column.typeName, &column.notNull, &column.primaryColumns, &column.primary); err != nil {
			return nil, err
		}
		columns[name] = column
	}
	return columns, rows.Err()
}

func readWMS(ctx context.Context, db Connection, m *WMSMapping, after Position, take int) (rows *sql.Rows, finish func(), err error) {
	if err := m.Validate(); err != nil {
		return nil, nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	finish = func() { _ = tx.Rollback() }
	defer func() {
		if err != nil {
			finish()
		}
	}()
	// Keep the table definition stable between catalog validation and SELECT.
	if _, err = tx.ExecContext(ctx, "LOCK TABLE "+quoteIdentifier("wms")+"."+quoteIdentifier(m.Table)+" IN ACCESS SHARE MODE"); err != nil {
		return nil, finish, err
	}
	columns, err := wmsColumns(ctx, tx, m.Table)
	if err != nil {
		return nil, finish, err
	}
	key := columns[m.KeyColumn]
	keyTypes := map[string]string{"int2": "smallint", "int4": "integer", "int8": "bigint", "text": "text", "varchar": "text", "bpchar": "text", "uuid": "uuid"}
	keyType := keyTypes[key.typeName]
	if !key.primary || key.primaryColumns != 1 || !key.notNull || keyType == "" {
		return nil, finish, errors.New("WMS key must be a supported single-column primary key")
	}
	projection := make([]string, 0, len(m.Fields))
	for _, field := range m.Fields {
		column, ok := columns[field]
		if !ok {
			return nil, finish, errors.New("unknown WMS field")
		}
		name := quoteIdentifier(field)
		switch {
		case field == m.KeyColumn:
			projection = append(projection, name+"::text AS "+name)
		case column.typeName == "timestamp":
			projection = append(projection, `to_char(`+name+`, 'YYYY-MM-DD"T"HH24:MI:SS.US') AS `+name)
		case column.typeName == "timestamptz":
			projection = append(projection, `to_char(`+name+` AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS `+name)
		default:
			projection = append(projection, name)
		}
	}
	keyName := "src." + quoteIdentifier(m.KeyColumn)
	predicate := `($1::` + keyType + ` IS NULL OR ` + keyName + ` > $1::` + keyType + `)`
	order := keyName
	var lastKey any
	if after.Key != "" {
		lastKey = after.Key
	}
	args := []any{lastKey, take}
	if m.UpdatedAt != "" {
		column := columns[m.UpdatedAt]
		if !column.notNull || column.typeName != "timestamp" && column.typeName != "timestamptz" {
			return nil, finish, errors.New("WMS watermark must be a non-null timestamp")
		}
		var stamp any
		if after.Key != "" {
			if after.Stamp == "" {
				return nil, finish, errors.New("missing WMS watermark")
			}
			stamp = after.Stamp
		}
		stampName := "src." + quoteIdentifier(m.UpdatedAt)
		predicate = `($1::` + keyType + ` IS NULL OR ` + stampName + ` > $3::` + column.typeName + ` OR (` + stampName + ` = $3::` + column.typeName + ` AND ` + keyName + ` > $1::` + keyType + `))`
		order = stampName + "," + keyName
		args = append(args, stamp)
	}
	query := "SELECT " + strings.Join(projection, ",") + " FROM " + quoteIdentifier("wms") + "." + quoteIdentifier(m.Table) + " AS src WHERE " + predicate + " ORDER BY " + order + " LIMIT $2"
	rows, err = tx.QueryContext(ctx, query, args...)
	return rows, finish, err
}
