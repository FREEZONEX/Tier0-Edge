package dataingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// These are reliability tables in the UNS sink, not a second namespace model.
const streamReliabilitySchema = `
CREATE TABLE IF NOT EXISTS public.stream_receipt (
 receipt_id text PRIMARY KEY, stream_id bigint NOT NULL, version_id bigint NOT NULL,
 topic_id bigint NOT NULL, completed_at timestamptz NOT NULL DEFAULT now()
);
`

type StreamCommit struct {
	Native    bool
	ReceiptID string
	StreamID  int64
	VersionID int64
	TopicID   int64
	Subject   string
	Event     json.RawMessage
	Records   []Record
}

func (w *SinkWriter) EnsureStreamSchema(ctx context.Context) error {
	if w == nil || w.pool == nil {
		return errors.New("SourceStream.sinkUnavailable")
	}
	_, err := w.pool.Exec(ctx, streamReliabilitySchema)
	return err
}

// CommitStreamTarget commits one target independently. On redelivery, successful
// targets are skipped while failed targets can still be retried.
func (w *SinkWriter) CommitStreamTarget(ctx context.Context, item StreamCommit) (bool, error) {
	if w == nil || w.pool == nil {
		return false, errors.New("SourceStream.sinkUnavailable")
	}
	validVersion := item.StreamID > 0 && item.VersionID > 0 && !item.Native
	validNative := item.Native && item.StreamID == 0 && item.VersionID == 0
	if item.ReceiptID == "" || len(item.ReceiptID) > 200 || (!validVersion && !validNative) || item.TopicID <= 0 {
		return false, errors.New("SourceStream.invalidReceipt")
	}
	if item.Subject != "" && (!json.Valid(item.Event) || len(item.Event) > 1024*1024 || strings.ContainsAny(item.Subject, " *>\t\n\r")) {
		return false, errors.New("SourceStream.invalidEvent")
	}
	for _, record := range item.Records {
		if record.UnsID != item.TopicID || record.Time.IsZero() {
			return false, errors.New("SourceStream.invalidTarget")
		}
		for _, field := range recordFields(record) {
			if raw, exists := record.Values[field.Name]; exists {
				if _, err := typedValueByType(field.Type, raw); err != nil {
					return false, fmt.Errorf("SourceStream.schemaMismatch: %s", field.Name)
				}
			}
		}
	}
	// Existing physical-table maintenance remains outside the data transaction.
	// No message is acknowledged until the following transaction has committed.
	mappings, err := w.ensurePhysicalForRecords(ctx, item.Records)
	if err != nil {
		return false, err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	tag, err := tx.Exec(ctx, `INSERT INTO public.stream_receipt(receipt_id,stream_id,version_id,topic_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, item.ReceiptID, item.StreamID, item.VersionID, item.TopicID)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	for _, record := range item.Records {
		if err := insertStreamRecord(ctx, tx, record, mappings[record.UnsID]); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

func insertStreamRecord(ctx context.Context, tx pgx.Tx, record Record, mappings []metricFieldMapping) error {
	if record.TopicType != metricTopicType {
		payload, err := eventRecordJSON(record)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s,%s) VALUES($1,$2::jsonb)`, sinkQualifiedName(recordAlias(record)), quoteIdent(metricTimeColumn), quoteIdent(eventJSONColumn)), record.Time.UTC(), string(payload))
		return err
	}
	columns := []string{quoteIdent(metricIDColumn), quoteIdent(metricTimeColumn), quoteIdent(metricQualityColumn)}
	args := []any{record.UnsID, record.Time.UTC(), record.Quality}
	updates := []string{fmt.Sprintf(`%s=EXCLUDED.%s`, quoteIdent(metricQualityColumn), quoteIdent(metricQualityColumn))}
	for _, mapping := range mappings {
		raw, exists := record.Values[mapping.Field.Name]
		if !exists {
			continue
		}
		value, err := typedValueByType(mapping.Field.Type, raw)
		if err != nil {
			return err
		}
		column := quoteIdent(mapping.SourceColumn)
		columns = append(columns, column)
		args = append(args, value)
		updates = append(updates, column+"=EXCLUDED."+column)
	}
	placeholders := make([]string, len(args))
	for i := range args {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s) VALUES(%s) ON CONFLICT (%s,%s) DO UPDATE SET %s`, sinkQualifiedName(metricTable), strings.Join(columns, ","), strings.Join(placeholders, ","), quoteIdent(metricIDColumn), quoteIdent(metricTimeColumn), strings.Join(updates, ",")), args...)
	return err
}
