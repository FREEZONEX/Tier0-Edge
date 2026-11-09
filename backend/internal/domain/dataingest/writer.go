package dataingest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	metricTopicType        = int16(3)
	metricTable            = "uns_timeserial"
	sinkSchema             = "public"
	physicalDDLAdvisoryKey = "tier0.uns.physical.ensure"
	physicalDDLColumnBatch = 4

	metricIDColumn       = "_id"
	metricTimeColumn     = "_timestamp"
	metricQualityColumn  = "_quality"
	metricFallbackColumn = "double_1"
	eventJSONColumn      = "_json"
)

type metricFieldMapping struct {
	Field        Field
	SourceColumn string
	Expression   string
}

type metricViewColumn struct {
	ColumnName   string
	SourceColumn string
	Expression   string
}

type sinkRelationInfo struct {
	Kind           string
	Columns        []string
	ViewDefinition string
}

type SinkWriter struct {
	pool           *pgxpool.Pool
	batchSize      int
	retentionYears int
}

func NewSinkWriter(ctx context.Context, url string, batchSize int) (*SinkWriter, error) {
	if batchSize <= 0 {
		batchSize = 5000
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 8
	cfg.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	writer := &SinkWriter{
		pool:           pool,
		batchSize:      batchSize,
		retentionYears: config.NormalizeTimeseriesRetentionYears(0),
	}
	if err := writer.EnsureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return writer, nil
}

func (w *SinkWriter) Close() {
	if w != nil && w.pool != nil {
		w.pool.Close()
	}
}

func (w *SinkWriter) EnsureSchema(ctx context.Context) error {
	if w == nil || w.pool == nil {
		return nil
	}
	_, _ = w.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`)
	if _, err := w.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(sinkSchema))); err != nil {
		return err
	}
	if _, err := w.pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (%s int8 NOT NULL, %s timestamptz(3) NOT NULL DEFAULT now(), %s int8 DEFAULT 0, %s float8 NULL, PRIMARY KEY (%s, %s))`,
		sinkQualifiedName(metricTable),
		quoteIdent(metricIDColumn),
		quoteIdent(metricTimeColumn),
		quoteIdent(metricQualityColumn),
		quoteIdent(metricFallbackColumn),
		quoteIdent(metricIDColumn),
		quoteIdent(metricTimeColumn),
	)); err != nil {
		return err
	}
	if err := w.ensureMetricBaseColumns(ctx); err != nil {
		return err
	}
	_, _ = w.pool.Exec(ctx, `SELECT create_hypertable($1::regclass, $2, partitioning_column => $3, number_partitions => 4, chunk_time_interval => INTERVAL '7 days', if_not_exists => TRUE)`, sinkRegclassName(metricTable), metricTimeColumn, metricIDColumn)
	_, _ = w.pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s SET (timescaledb.compress, timescaledb.compress_segmentby = %s, timescaledb.compress_orderby = %s)`,
		sinkQualifiedName(metricTable),
		quoteLiteral(metricIDColumn),
		quoteLiteral(quoteIdent(metricTimeColumn)+" DESC"),
	))
	_, _ = w.pool.Exec(ctx, `SELECT add_retention_policy($1::regclass, make_interval(years => $2), if_not_exists => TRUE)`, sinkRegclassName(metricTable), w.retentionYears)
	_, _ = w.pool.Exec(ctx, `SELECT add_compression_policy($1::regclass, compress_after => INTERVAL '7 days', schedule_interval => INTERVAL '1 day')`, sinkRegclassName(metricTable))
	return nil
}

func (w *SinkWriter) ensureMetricBaseColumns(ctx context.Context) error {
	if err := w.renameColumnIfNeeded(ctx, metricTable, "timeStamp", metricTimeColumn); err != nil {
		return err
	}
	return w.renameColumnIfNeeded(ctx, metricTable, "quality", metricQualityColumn)
}

func (w *SinkWriter) renameColumnIfNeeded(ctx context.Context, table, oldName, newName string) error {
	var shouldRename bool
	err := w.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
) AND NOT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = $1 AND table_name = $2 AND column_name = $4
)`, sinkSchema, table, oldName, newName).Scan(&shouldRename)
	if err != nil {
		return err
	}
	if !shouldRename {
		return nil
	}
	_, err = w.pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s RENAME COLUMN %s TO %s`,
		sinkQualifiedName(table),
		quoteIdent(oldName),
		quoteIdent(newName),
	))
	return err
}

func (w *SinkWriter) WriteRecords(ctx context.Context, records []Record) error {
	if w == nil || w.pool == nil || len(records) == 0 {
		return nil
	}
	mappings, err := w.ensurePhysicalForRecords(ctx, records)
	if err != nil {
		return err
	}
	metricRecords := make([]Record, 0, len(records))
	aliasRecords := map[string][]Record{}
	for _, record := range records {
		if record.TopicType == metricTopicType {
			metricRecords = append(metricRecords, record)
			continue
		}
		alias := recordAlias(record)
		if alias == "" {
			continue
		}
		aliasRecords[alias] = append(aliasRecords[alias], record)
	}
	if len(metricRecords) > 0 {
		if err := w.writeMetricRecords(ctx, metricRecords, mappings); err != nil {
			return err
		}
	}
	aliases := make([]string, 0, len(aliasRecords))
	for alias := range aliasRecords {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if err := w.writeAliasRecords(ctx, alias, aliasRecords[alias]); err != nil {
			return err
		}
	}
	return nil
}

func (w *SinkWriter) writeMetricRecords(ctx context.Context, records []Record, mappings map[int64][]metricFieldMapping) error {
	records = dedupeMetricRecords(records)
	for start := 0; start < len(records); start += w.batchSize {
		end := start + w.batchSize
		if end > len(records) {
			end = len(records)
		}
		if err := w.writeMetricBatch(ctx, records[start:end], mappings); err != nil {
			return err
		}
	}
	return nil
}

func (w *SinkWriter) ensurePhysicalForRecords(ctx context.Context, records []Record) (map[int64][]metricFieldMapping, error) {
	if w == nil || w.pool == nil || len(records) == 0 {
		return map[int64][]metricFieldMapping{}, nil
	}
	metricRecords := make([]Record, 0, len(records))
	aliasSet := map[string]struct{}{}
	eventAliases := map[string]struct{}{}
	for _, record := range records {
		alias := recordAlias(record)
		if alias == "" {
			continue
		}
		aliasSet[alias] = struct{}{}
		if record.TopicType == metricTopicType {
			metricRecords = append(metricRecords, record)
		} else {
			eventAliases[alias] = struct{}{}
		}
	}
	aliases := sortedStringSet(aliasSet)
	catalogNames := append([]string{metricTable}, aliases...)
	var mappings map[int64][]metricFieldMapping
	var existingMetricColumns []string
	if err := w.runPhysicalDDLTransaction(ctx, func(tx pgx.Tx) error {
		catalog, err := loadSinkRelationCatalog(ctx, tx, catalogNames)
		if err != nil {
			return err
		}
		mappings = metricMappings(metricRecords, catalog)
		existingMetricColumns = append([]string(nil), catalog[metricTable].Columns...)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(metricRecords) > 0 {
		for _, statement := range metricColumnDDLStatements(mappings, existingMetricColumns) {
			statement := statement
			if err := w.runPhysicalDDLTransaction(ctx, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, statement)
				return err
			}); err != nil {
				return nil, err
			}
		}
	}
	if err := w.runPhysicalDDLTransaction(ctx, func(tx pgx.Tx) error {
		catalog, err := loadSinkRelationCatalog(ctx, tx, catalogNames)
		if err != nil {
			return err
		}
		batch := &pgx.Batch{}
		if len(metricRecords) > 0 {
			queueMetricViews(batch, metricRecords, mappings, catalog)
		}
		queueEventAliasTables(batch, sortedStringSet(eventAliases), catalog)
		return executePhysicalDDLBatch(ctx, tx, batch)
	}); err != nil {
		return nil, err
	}
	return mappings, nil
}

func (w *SinkWriter) runPhysicalDDLTransaction(ctx context.Context, run func(pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, physicalDDLAdvisoryKey); err != nil {
		return err
	}
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func executePhysicalDDLBatch(ctx context.Context, tx pgx.Tx, batch *pgx.Batch) error {
	if batch.Len() == 0 {
		return nil
	}
	results := tx.SendBatch(ctx, batch)
	var batchErr error
	for i := 0; i < batch.Len(); i++ {
		if _, err := results.Exec(); err != nil && batchErr == nil {
			batchErr = err
		}
	}
	closeErr := results.Close()
	if batchErr != nil {
		return batchErr
	}
	return closeErr
}

func metricColumnDDLStatements(mappings map[int64][]metricFieldMapping, existingColumns []string) []string {
	columns := missingMetricColumns(mappings, existingColumns)
	names := sortedColumnNames(columns)
	statements := make([]string, 0, (len(names)+physicalDDLColumnBatch-1)/physicalDDLColumnBatch)
	for start := 0; start < len(names); start += physicalDDLColumnBatch {
		end := min(start+physicalDDLColumnBatch, len(names))
		clauses := make([]string, 0, end-start)
		for _, column := range names[start:end] {
			field := columns[column]
			clauses = append(clauses, fmt.Sprintf(`ADD COLUMN IF NOT EXISTS %s %s NULL`,
				quoteIdent(column), metricSQLType(column, field.Type)))
		}
		// TimescaleDB propagates every added column to every chunk. Bound the
		// columns per statement to avoid the memory peak of one large ALTER while
		// avoiding a full chunk-catalog traversal for every individual column.
		statements = append(statements, fmt.Sprintf(`ALTER TABLE %s %s`,
			sinkQualifiedName(metricTable), strings.Join(clauses, ", ")))
	}
	return statements
}

func missingMetricColumns(mappings map[int64][]metricFieldMapping, existingColumns []string) map[string]Field {
	columns := metricColumns(mappings)
	columns[metricFallbackColumn] = Field{Name: metricFallbackColumn, Type: "double"}
	existing := make(map[string]struct{}, len(existingColumns))
	for _, column := range existingColumns {
		existing[strings.ToLower(strings.TrimSpace(column))] = struct{}{}
	}
	for column := range columns {
		if _, ok := existing[strings.ToLower(column)]; ok {
			delete(columns, column)
		}
	}
	return columns
}

func queueMetricViews(batch *pgx.Batch, records []Record, mappings map[int64][]metricFieldMapping, catalog map[string]sinkRelationInfo) {
	representatives := representativeMetricRecords(records)
	ordered := make([]Record, 0, len(representatives))
	for _, record := range representatives {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := recordAlias(ordered[i])
		right := recordAlias(ordered[j])
		if left != right {
			return left < right
		}
		return ordered[i].UnsID < ordered[j].UnsID
	})
	for _, record := range ordered {
		alias := recordAlias(record)
		if alias == "" {
			continue
		}
		selects := []string{quoteIdent(metricIDColumn), quoteIdent(metricTimeColumn), quoteIdent(metricQualityColumn)}
		for _, mapping := range mappings[record.UnsID] {
			selects = append(selects, metricSelectExpression(mapping))
		}
		if metricViewNeedsRebuild(catalog[alias], mappings[record.UnsID]) {
			// Recreate only the projection, in the same DDL transaction. History
			// remains in the shared table. Do not cascade into dependent objects.
			batch.Queue(fmt.Sprintf(`DROP VIEW %s`, sinkQualifiedName(alias)))
		}
		batch.Queue(fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS SELECT %s FROM %s WHERE %s = %d`, sinkQualifiedName(alias), strings.Join(selects, ", "), sinkQualifiedName(metricTable), quoteIdent(metricIDColumn), record.UnsID))
	}
}

func queueEventAliasTables(batch *pgx.Batch, aliases []string, catalog map[string]sinkRelationInfo) {
	for _, alias := range aliases {
		info := catalog[alias]
		if shouldResetAliasEventTable(info.Columns) {
			switch info.Kind {
			case "v":
				batch.Queue(fmt.Sprintf(`DROP VIEW IF EXISTS %s CASCADE`, sinkQualifiedName(alias)))
			case "m":
				batch.Queue(fmt.Sprintf(`DROP MATERIALIZED VIEW IF EXISTS %s CASCADE`, sinkQualifiedName(alias)))
			default:
				batch.Queue(fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, sinkQualifiedName(alias)))
			}
		}
		batch.Queue(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (%s BIGSERIAL PRIMARY KEY, %s timestamptz(3) NOT NULL, %s JSONB NOT NULL)`,
			sinkQualifiedName(alias), quoteIdent(metricIDColumn), quoteIdent(metricTimeColumn), quoteIdent(eventJSONColumn)))
		idxName := "idx_" + shortHash(alias+"_event_ts")
		batch.Queue(fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s DESC)`, quoteIdent(idxName), sinkQualifiedName(alias), quoteIdent(metricTimeColumn)))
	}
}

func loadSinkRelationCatalog(ctx context.Context, tx pgx.Tx, aliases []string) (map[string]sinkRelationInfo, error) {
	out := make(map[string]sinkRelationInfo, len(aliases))
	if len(aliases) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
SELECT c.relname,
       c.relkind::text,
       CASE WHEN c.relkind = 'v' THEN pg_get_viewdef(c.oid, true) ELSE '' END,
       COALESCE(a.attname, '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE n.nspname = $1 AND c.relname = ANY($2::text[])
ORDER BY c.relname, a.attnum`, sinkSchema, aliases)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias, kind, viewDefinition, column string
		if err := rows.Scan(&alias, &kind, &viewDefinition, &column); err != nil {
			return nil, err
		}
		info := out[alias]
		info.Kind = kind
		info.ViewDefinition = viewDefinition
		if column != "" {
			info.Columns = append(info.Columns, column)
		}
		out[alias] = info
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func sortedStringSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (w *SinkWriter) writeMetricBatch(ctx context.Context, records []Record, mappings map[int64][]metricFieldMapping) error {
	if len(records) == 0 {
		return nil
	}
	columns := metricColumns(mappings)
	valueColumns := sortedColumnNames(columns)
	copyColumns := append([]string{metricIDColumn, metricTimeColumn, metricQualityColumn}, valueColumns...)
	rows := make([][]any, 0, len(records))
	for _, record := range records {
		values := metricRecordValues(record, mappings[record.UnsID])
		row := make([]any, 0, len(copyColumns))
		row = append(row, record.UnsID, record.Time.UTC(), record.Quality)
		for _, column := range valueColumns {
			field := columns[column]
			raw, ok := values[column]
			if !ok {
				row = append(row, nil)
				continue
			}
			value, err := typedValueByType(field.Type, raw)
			if err != nil {
				value = nil
			}
			row = append(row, value)
		}
		rows = append(rows, row)
	}
	return w.copyAndMerge(ctx, metricTable, []string{metricIDColumn, metricTimeColumn}, copyColumns, valueColumns, rows, metricQualityColumn)
}

func (w *SinkWriter) writeAliasRecords(ctx context.Context, alias string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	copyColumns := []string{metricTimeColumn, eventJSONColumn}
	for start := 0; start < len(records); start += w.batchSize {
		end := start + w.batchSize
		if end > len(records) {
			end = len(records)
		}
		rows := make([][]any, 0, end-start)
		for _, record := range records[start:end] {
			payload, err := eventRecordJSON(record)
			if err != nil {
				return err
			}
			rows = append(rows, []any{record.Time.UTC(), payload})
		}
		if _, err := w.pool.CopyFrom(ctx, pgx.Identifier{sinkSchema, alias}, copyColumns, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
	}
	return nil
}

func shouldResetAliasEventTable(columns []string) bool {
	if len(columns) == 0 {
		return false
	}
	required := map[string]struct{}{
		strings.ToLower(metricIDColumn):   {},
		strings.ToLower(metricTimeColumn): {},
		strings.ToLower(eventJSONColumn):  {},
	}
	if len(columns) != len(required) {
		return true
	}
	for _, column := range columns {
		key := strings.ToLower(strings.TrimSpace(column))
		if _, ok := required[key]; !ok {
			return true
		}
		delete(required, key)
	}
	return len(required) > 0
}

func eventRecordJSON(record Record) (json.RawMessage, error) {
	values := eventPayloadValues(record)
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func eventPayloadValues(record Record) map[string]any {
	if record.EventPayload != nil {
		return clonePayloadMap(record.EventPayload)
	}
	fieldTypes := map[string]string{}
	for _, field := range recordFields(record) {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		fieldTypes[name] = field.Type
	}

	values := make(map[string]any)
	for key, raw := range record.RawValues {
		values[key] = eventPayloadValue(fieldTypes[key], raw)
	}
	if len(values) > 0 {
		return values
	}
	for key, raw := range record.Values {
		values[key] = eventPayloadValue(fieldTypes[key], raw)
	}
	return values
}

func eventPayloadValue(kind, raw string) any {
	if kind == "" {
		return raw
	}
	value, err := typedValueByType(kind, raw)
	if err != nil {
		return raw
	}
	return value
}

func (w *SinkWriter) copyAndMerge(ctx context.Context, table string, keyColumns, copyColumns, updateColumns []string, rows [][]any, qualityColumn string) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	temp := "tmp_sink_" + shortHash(table+strconv.FormatInt(time.Now().UnixNano(), 10))
	if _, err := tx.Exec(ctx, fmt.Sprintf(`CREATE TEMP TABLE %s (LIKE %s INCLUDING DEFAULTS) ON COMMIT DROP`, quoteIdent(temp), sinkQualifiedName(table))); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{temp}, copyColumns, pgx.CopyFromRows(rows)); err != nil {
		return err
	}

	insertCols := quotedList(copyColumns)
	selectCols := quotedList(copyColumns)
	conflictCols := quotedList(keyColumns)
	assignments := make([]string, 0, len(updateColumns)+1)
	if qualityColumn != "" {
		assignments = append(assignments, fmt.Sprintf(`%s=EXCLUDED.%s`, quoteIdent(qualityColumn), quoteIdent(qualityColumn)))
	}
	for _, column := range updateColumns {
		assignments = append(assignments, fmt.Sprintf(`%s=COALESCE(EXCLUDED.%s, %s.%s)`, quoteIdent(column), quoteIdent(column), quoteIdent(table), quoteIdent(column)))
	}
	mergeSQL := fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s ON CONFLICT (%s) DO UPDATE SET %s`,
		sinkQualifiedName(table),
		insertCols,
		selectCols,
		quoteIdent(temp),
		conflictCols,
		strings.Join(assignments, ", "),
	)
	if _, err := tx.Exec(ctx, mergeSQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func metricMappings(records []Record, catalog map[string]sinkRelationInfo) map[int64][]metricFieldMapping {
	representatives := representativeMetricRecords(records)
	ids := make([]int64, 0, len(representatives))
	for id := range representatives {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := make(map[int64][]metricFieldMapping, len(representatives))
	for _, id := range ids {
		record := representatives[id]
		alias := recordAlias(record)
		if alias == "" {
			continue
		}
		var columns []metricViewColumn
		if info := catalog[alias]; info.Kind == "v" {
			columns = parseMetricViewColumns(info.ViewDefinition)
		}
		out[id] = buildMetricMappings(recordFields(record), columns)
	}
	return out
}

func representativeMetricRecords(records []Record) map[int64]Record {
	out := make(map[int64]Record, len(records))
	for _, record := range records {
		if existing, ok := out[record.UnsID]; ok {
			out[record.UnsID] = mergeRecord(existing, record)
			continue
		}
		out[record.UnsID] = record
	}
	return out
}

func buildMetricMappings(fields []Field, existing []metricViewColumn) []metricFieldMapping {
	now := time.Now().Unix()
	existingByField := make(map[string]metricViewColumn, len(existing))
	seenSources := make(map[string]bool, len(existing))
	for _, column := range existing {
		if column.ColumnName != "" {
			existingByField[column.ColumnName] = column
		}
		if column.SourceColumn != "" {
			seenSources[column.SourceColumn] = true
		}
	}

	compatible := make(map[string]metricViewColumn, len(fields))
	usedByPrefix := map[string]map[int]bool{}
	for _, field := range fields {
		prefix := legacyMetricColumnPrefix(field.Type)
		column, ok := existingByField[field.Name]
		if !ok || !strings.HasPrefix(column.SourceColumn, prefix+"_") {
			continue
		}
		compatible[field.Name] = column
		markMetricColumnUsed(usedByPrefix, prefix, column.SourceColumn)
	}

	out := make([]metricFieldMapping, 0, len(fields))
	for _, field := range fields {
		if column, ok := compatible[field.Name]; ok {
			out = append(out, metricFieldMapping{
				Field:        field,
				SourceColumn: column.SourceColumn,
				Expression:   column.Expression,
			})
			continue
		}
		prefix := legacyMetricColumnPrefix(field.Type)
		sourceColumn := nextMetricColumn(usedByPrefix, prefix)
		mapping := metricFieldMapping{Field: field, SourceColumn: sourceColumn}
		if seenSources[sourceColumn] {
			mapping.Expression = fmt.Sprintf(`CASE WHEN %s < to_timestamp(%d) THEN NULL ELSE %s END AS %s`,
				quoteIdent(metricTimeColumn),
				now,
				quoteIdent(sourceColumn),
				quoteIdent(field.Name),
			)
		}
		out = append(out, mapping)
	}
	return out
}

func markMetricColumnUsed(usedByPrefix map[string]map[int]bool, prefix, sourceColumn string) {
	if usedByPrefix[prefix] == nil {
		usedByPrefix[prefix] = map[int]bool{}
	}
	if number := metricColumnNumber(prefix, sourceColumn); number > 0 {
		usedByPrefix[prefix][number] = true
	}
}

func nextMetricColumn(usedByPrefix map[string]map[int]bool, prefix string) string {
	if usedByPrefix[prefix] == nil {
		usedByPrefix[prefix] = map[int]bool{}
	}
	for number := 1; ; number++ {
		if !usedByPrefix[prefix][number] {
			usedByPrefix[prefix][number] = true
			return fmt.Sprintf("%s_%d", prefix, number)
		}
	}
}

func metricColumnNumber(prefix, sourceColumn string) int {
	if !strings.HasPrefix(sourceColumn, prefix+"_") {
		return 0
	}
	number, err := strconv.Atoi(strings.TrimPrefix(sourceColumn, prefix+"_"))
	if err != nil {
		return 0
	}
	return number
}

func metricColumns(mappings map[int64][]metricFieldMapping) map[string]Field {
	columns := map[string]Field{}
	for _, recordMappings := range mappings {
		for _, mapping := range recordMappings {
			columns[mapping.SourceColumn] = mapping.Field
		}
	}
	return columns
}

func metricRecordValues(record Record, mappings []metricFieldMapping) map[string]string {
	values := map[string]string{}
	for _, mapping := range mappings {
		if value, ok := record.Values[mapping.Field.Name]; ok {
			values[mapping.SourceColumn] = value
		}
	}
	return values
}

func aliasColumns(records []Record) map[string]Field {
	columns := map[string]Field{}
	for _, record := range records {
		for _, field := range recordFields(record) {
			columns[field.Name] = field
		}
	}
	return columns
}

func recordFields(record Record) []Field {
	fields := dataFields(record.Fields, record.TopicType)
	if len(fields) == 0 {
		fields = []Field{{Name: "value", Type: "string"}}
	}
	return fields
}

func sortedFields(fields []Field) []Field {
	out := append([]Field(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func sortedColumnNames(columns map[string]Field) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func dedupeMetricRecords(records []Record) []Record {
	seen := map[string]int{}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		key := fmt.Sprintf("%d:%d", record.UnsID, record.Time.UnixMilli())
		if idx, exists := seen[key]; exists {
			out[idx] = mergeRecord(out[idx], record)
			continue
		}
		seen[key] = len(out)
		out = append(out, record)
	}
	return out
}

func dedupeAliasRecords(records []Record) []Record {
	seen := map[string]int{}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		key := strconv.FormatInt(record.Time.UnixMilli(), 10)
		if idx, exists := seen[key]; exists {
			out[idx] = mergeRecord(out[idx], record)
			continue
		}
		seen[key] = len(out)
		out = append(out, record)
	}
	return out
}

func mergeRecord(base, next Record) Record {
	base.Quality = next.Quality
	if next.Time.After(base.Time) {
		base.Time = next.Time
	}
	if base.Values == nil {
		base.Values = map[string]string{}
	}
	for key, value := range next.Values {
		base.Values[key] = value
	}
	seen := map[string]struct{}{}
	fields := make([]Field, 0, len(base.Fields)+len(next.Fields))
	for _, field := range append(base.Fields, next.Fields...) {
		key := strings.ToLower(strings.TrimSpace(field.Name))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		fields = append(fields, field)
	}
	base.Fields = fields
	return base
}

func recordAlias(record Record) string {
	for _, value := range []string{record.Alias, record.Namespace, strconv.FormatInt(record.UnsID, 10)} {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value != "" {
			return value
		}
	}
	return ""
}

func metricSelectExpression(mapping metricFieldMapping) string {
	expression := strings.TrimSpace(mapping.Expression)
	if expression == "" {
		expression = quoteIdent(mapping.SourceColumn)
		switch normalizeFieldType(mapping.Field.Type) {
		case "integer":
			expression = fmt.Sprintf(`%s::int4`, quoteIdent(mapping.SourceColumn))
		case "float":
			expression = fmt.Sprintf(`%s::real`, quoteIdent(mapping.SourceColumn))
		}
	}
	if !strings.Contains(strings.ToUpper(expression), " AS ") {
		expression = fmt.Sprintf(`%s AS %s`, expression, quoteIdent(mapping.Field.Name))
	}
	return expression
}

func parseMetricViewColumns(definition string) []metricViewColumn {
	selectList := selectListFromView(definition)
	if selectList == "" {
		return nil
	}
	expressions := splitSQLList(selectList)
	out := make([]metricViewColumn, 0, len(expressions))
	for _, expression := range expressions {
		columnName, expr := splitSelectAlias(expression)
		if columnName == "" {
			columnName = lastIdentifier(expr)
		}
		sourceColumn := metricSourceColumn(expr)
		if columnName == "" || sourceColumn == "" {
			continue
		}
		out = append(out, metricViewColumn{
			ColumnName:   columnName,
			SourceColumn: sourceColumn,
			Expression:   expr,
		})
	}
	return out
}

func selectListFromView(definition string) string {
	upper := strings.ToUpper(definition)
	selectIndex := strings.Index(upper, "SELECT")
	fromIndex := strings.Index(upper, "FROM")
	if selectIndex < 0 || fromIndex <= selectIndex {
		return ""
	}
	return strings.TrimSpace(definition[selectIndex+len("SELECT") : fromIndex])
}

func splitSQLList(value string) []string {
	var out []string
	var current strings.Builder
	parenDepth := 0
	singleQuoted := false
	doubleQuoted := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch ch {
		case '\'':
			if !doubleQuoted {
				singleQuoted = !singleQuoted
			}
		case '"':
			if !singleQuoted {
				doubleQuoted = !doubleQuoted
			}
		case '(':
			if !singleQuoted && !doubleQuoted {
				parenDepth++
			}
		case ')':
			if !singleQuoted && !doubleQuoted && parenDepth > 0 {
				parenDepth--
			}
		case ',':
			if !singleQuoted && !doubleQuoted && parenDepth == 0 {
				if item := strings.TrimSpace(current.String()); item != "" {
					out = append(out, item)
				}
				current.Reset()
				continue
			}
		}
		current.WriteByte(ch)
	}
	if item := strings.TrimSpace(current.String()); item != "" {
		out = append(out, item)
	}
	return out
}

func splitSelectAlias(expression string) (string, string) {
	upper := strings.ToUpper(expression)
	asIndex := strings.LastIndex(upper, " AS ")
	if asIndex < 0 {
		return "", strings.TrimSpace(expression)
	}
	alias := strings.TrimSpace(expression[asIndex+4:])
	expr := strings.TrimSpace(expression[:asIndex])
	return cleanIdentifier(alias), expr
}

func metricSourceColumn(expression string) string {
	upper := strings.ToUpper(expression)
	if elseIndex := strings.LastIndex(upper, " ELSE "); elseIndex >= 0 {
		endIndex := strings.LastIndex(upper, " END")
		if endIndex > elseIndex {
			expression = expression[elseIndex+6 : endIndex]
		} else {
			expression = expression[elseIndex+6:]
		}
	}
	return lastIdentifier(expression)
}

func lastIdentifier(expression string) string {
	expression = strings.TrimSpace(expression)
	lastQuoted := ""
	for i := 0; i < len(expression); i++ {
		if expression[i] != '"' {
			continue
		}
		var builder strings.Builder
		i++
		for ; i < len(expression); i++ {
			if expression[i] == '"' {
				if i+1 < len(expression) && expression[i+1] == '"' {
					builder.WriteByte('"')
					i++
					continue
				}
				break
			}
			builder.WriteByte(expression[i])
		}
		if builder.Len() > 0 {
			lastQuoted = builder.String()
		}
	}
	if lastQuoted != "" {
		return lastQuoted
	}
	fields := strings.FieldsFunc(expression, func(r rune) bool {
		return !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	for i := len(fields) - 1; i >= 0; i-- {
		if fields[i] != "" {
			return fields[i]
		}
	}
	return ""
}

func cleanIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) && len(value) >= 2 {
		return strings.ReplaceAll(value[1:len(value)-1], `""`, `"`)
	}
	return strings.Trim(value, `"`)
}

func legacyMetricColumnPrefix(kind string) string {
	switch normalizeFieldType(kind) {
	case "integer", "long":
		return "long"
	case "float", "double":
		return "double"
	case "boolean":
		return "bool"
	case "datetime":
		return "date"
	default:
		return "str"
	}
}

func metricSQLType(column, kind string) string {
	switch {
	case strings.HasPrefix(column, "long_"):
		return "int8"
	case strings.HasPrefix(column, "double_"):
		return "float8"
	case strings.HasPrefix(column, "bool_"):
		return "boolean"
	case strings.HasPrefix(column, "date_"):
		return "timestamptz"
	case strings.HasPrefix(column, "str_"):
		return "text"
	default:
		return sqlType(kind)
	}
}

func sqlType(kind string) string {
	switch normalizeFieldType(kind) {
	case "integer":
		return "int4"
	case "long":
		return "int8"
	case "float":
		return "float4"
	case "double":
		return "float8"
	case "boolean":
		return "boolean"
	case "datetime":
		return "timestamptz(3)"
	case "blob":
		return "varchar(512)"
	default:
		return "text"
	}
}

func typedValueByType(kind, raw string) (any, error) {
	switch normalizeFieldType(kind) {
	case "integer":
		n, err := strconv.ParseInt(raw, 10, 32)
		return int32(n), err
	case "long":
		return strconv.ParseInt(raw, 10, 64)
	case "float":
		f, err := strconv.ParseFloat(raw, 32)
		return float32(f), err
	case "double":
		return strconv.ParseFloat(raw, 64)
	case "boolean":
		return strconv.ParseBool(raw)
	case "datetime":
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		t, ok := parseTime(raw)
		if !ok {
			return nil, fmt.Errorf("invalid datetime value %q", raw)
		}
		return t, nil
	default:
		return raw, nil
	}
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func sinkQualifiedName(table string) string {
	return quoteIdent(sinkSchema) + "." + quoteIdent(table)
}

func sinkRegclassName(table string) string {
	return sinkQualifiedName(table)
}

func quotedList(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, quoteIdent(value))
	}
	return strings.Join(out, ", ")
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

// PostgreSQL cannot replace a view when columns are removed, reordered or retyped.
func metricViewNeedsRebuild(info sinkRelationInfo, mappings []metricFieldMapping) bool {
	if info.Kind != "v" {
		return false
	}
	names := []string{metricIDColumn, metricTimeColumn, metricQualityColumn}
	for _, mapping := range mappings {
		names = append(names, mapping.Field.Name)
	}
	if len(info.Columns) > len(names) {
		return true
	}
	for i, name := range info.Columns {
		if names[i] != name {
			return true
		}
	}
	old := map[string]string{}
	for _, col := range parseMetricViewColumns(info.ViewDefinition) {
		old[col.ColumnName] = col.SourceColumn
	}
	for _, mapping := range mappings {
		if source, ok := old[mapping.Field.Name]; ok && source != mapping.SourceColumn {
			return true
		}
	}
	return false
}
