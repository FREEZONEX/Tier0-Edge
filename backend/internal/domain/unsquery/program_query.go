package unsquery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"gorm.io/gorm"
)

const MaxProgramQueryBytes = 256 * 1024

type TimeRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// ProgramOptions is data-only. Authority is checked by the admitted Stream host.
type ProgramOptions struct {
	// Host-only binding; callers cannot select or widen this authority.
	ResourceKey               string         `json:"-"`
	StorageCursorCollectionID string         `json:"-"`
	Kind                      string         `json:"kind"`
	Limit                     int            `json:"limit,omitempty"`
	Cursor                    string         `json:"cursor,omitempty"`
	RecordIDs                 []string       `json:"recordIds,omitempty"`
	Filters                   []RecordFilter `json:"filters,omitempty"`
	OrderBy                   []RecordOrder  `json:"orderBy,omitempty"`
	TimeRange                 *TimeRange     `json:"timeRange,omitempty"`
}

type ProgramPage struct {
	Items      []any  `json:"items"`
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
}

type programCursor struct {
	Revision int               `json:"revision"`
	Binding  string            `json:"binding"`
	Values   []json.RawMessage `json:"values,omitempty"`
	Time     int64             `json:"time,omitempty"`
	ID       string            `json:"id,omitempty"`
	RecordID string            `json:"recordId,omitempty"`
}

func normalizeProgramOptions(collectionID string, options *ProgramOptions) (string, programCursor, error) {
	if !unsmeta.ValidID(collectionID) || options.Kind != "record" && options.Kind != "sample" || options.Limit < 0 || options.Limit > 100 || len(options.RecordIDs) > 100 || len(options.Cursor) > 8192 || len(options.Filters) > maxRecordFilters || len(options.OrderBy) > maxRecordOrder {
		return "", programCursor{}, invalid("query", "invalid resource, kind, limit or cursor")
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	seen := map[string]bool{}
	for _, id := range options.RecordIDs {
		if !unsmeta.ValidID(id) || seen[id] {
			return "", programCursor{}, invalid("recordIds", "unique valid record IDs required")
		}
		seen[id] = true
	}
	if options.Kind == "sample" {
		if options.TimeRange == nil || options.TimeRange.From < 0 || options.TimeRange.To <= options.TimeRange.From || options.TimeRange.To > 8640000000000000 || len(options.Filters) != 0 || len(options.OrderBy) != 0 {
			return "", programCursor{}, invalid("query", "sample requires millisecond [from,to) and fixed time/ID order")
		}
	} else if options.TimeRange != nil {
		return "", programCursor{}, invalid("timeRange", "record query does not accept a time range")
	}
	copy := *options
	copy.Cursor = ""
	cursorCollectionID := collectionID
	if options.StorageCursorCollectionID != "" {
		cursorCollectionID = options.StorageCursorCollectionID
	}
	raw, _ := json.Marshal(struct {
		CollectionID string
		ResourceKey  string
		Options      ProgramOptions
	}{cursorCollectionID, options.ResourceKey, copy})
	sum := sha256.Sum256(raw)
	binding := hex.EncodeToString(sum[:])
	cursor := programCursor{}
	if options.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil || unsmeta.Parse(json.RawMessage(raw), &cursor) != nil || cursor.Revision != 1 || cursor.Binding != binding {
			return "", cursor, invalid("cursor", "cursor belongs to a different resource or query")
		}
	}
	return binding, cursor, nil
}

func encodeProgramCursor(cursor programCursor) string {
	cursor.Revision = 1
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// ValidateProgramOptions is also used before selecting a declared dry-run page.
func ValidateProgramOptions(collectionID string, options ProgramOptions) error {
	_, _, err := normalizeProgramOptions(collectionID, &options)
	return err
}

func programQueryDefinition(ctx context.Context, collectionID string, options ProgramOptions, bindings []repo.ProgramUNSBinding) (unsmeta.Document, error) {
	if len(bindings) > 1 {
		return unsmeta.Document{}, unsmeta.ErrInvalid
	}
	if len(bindings) == 1 {
		binding := bindings[0]
		if binding.CollectionID != collectionID || !binding.Reference.Readable() {
			return unsmeta.Document{}, repo.ErrInvalidArgument
		}
		if err := repo.ValidateProgramV2Bindings(ctx, streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{binding.Reference}}, bindings); err != nil {
			return unsmeta.Document{}, err
		}
	}
	doc, err := unsmeta.ReadCollectionDefinition(ctx, collectionID)
	if err != nil {
		return unsmeta.Document{}, err
	}
	if options.Kind == "sample" {
		if doc.SampleSchema == nil {
			return doc, invalid("kind", "Collection has no sample schema")
		}
	} else {
		if doc.RecordSchema == nil {
			return doc, invalid("kind", "Collection has no record schema")
		}
		q, err := RecordFilters(repo.GetUnsConn(ctx).Model(&repo.UnsRecord{}).Where("deleted_at IS NULL"), doc.RecordSchema, options.Filters)
		if err != nil {
			return doc, err
		}
		if _, err := OrderRecords(q, doc.RecordSchema, options.OrderBy); err != nil {
			return doc, err
		}
	}
	return doc, nil
}

// ValidateProgramCollection checks the same contract in previews without
// reading business rows. Frozen references are rechecked under the metadata lock.
func ValidateProgramCollection(ctx context.Context, collectionID string, options ProgramOptions, bindings ...repo.ProgramUNSBinding) error {
	if err := ValidateProgramOptions(collectionID, options); err != nil {
		return err
	}
	return unsmeta.WithProgramReadTransaction(ctx, func(ctx context.Context) error {
		_, err := programQueryDefinition(ctx, collectionID, options, bindings)
		return err
	})
}

// QueryProgramCollection shares API filter validation, but has a smaller page
// and JSON budget. Each page is consistent; separate calls are not a snapshot.
func QueryProgramCollection(ctx context.Context, collectionID string, options ProgramOptions, bindings ...repo.ProgramUNSBinding) (ProgramPage, error) {
	page := ProgramPage{Items: []any{}}
	binding, cursor, err := normalizeProgramOptions(collectionID, &options)
	if err != nil {
		return page, err
	}
	err = unsmeta.WithProgramReadTransaction(ctx, func(ctx context.Context) error {
		tx := repo.GetUnsConn(ctx).WithContext(ctx)
		doc, err := programQueryDefinition(ctx, collectionID, options, bindings)
		if err != nil {
			return err
		}
		if raw, routed, e := repo.ReadCollectionStorage(ctx, collectionID, "programQuery", map[string]any{"options": options, "resourceKey": options.ResourceKey, "logicalCollectionId": collectionID}); routed || e != nil {
			if e != nil {
				return e
			}
			return unsmeta.Parse(raw, &page)
		}
		q := tx.Where("collection_id=?", collectionID)
		if len(options.RecordIDs) > 0 {
			column := "id"
			if options.Kind == "sample" {
				column = "record_id"
			}
			q = q.Where(column+" IN ?", options.RecordIDs)
		}
		if options.Kind == "record" {
			if doc.RecordSchema == nil {
				return invalid("kind", "Collection has no record schema")
			}
			q = q.Model(&repo.UnsRecord{}).Where("deleted_at IS NULL")
			q, err = RecordFilters(q, doc.RecordSchema, options.Filters)
			if err != nil {
				return err
			}
			q, err = OrderRecords(q, doc.RecordSchema, options.OrderBy)
			if err != nil {
				return err
			}
			orders := append([]RecordOrder{}, options.OrderBy...)
			foundID := false
			for _, order := range orders {
				foundID = foundID || order.Field == "id"
			}
			if !foundID {
				orders = append(orders, RecordOrder{Field: "id", Direction: "asc"})
			}
			if options.Cursor != "" {
				q, err = afterProgramRecord(q, doc.RecordSchema, orders, cursor)
				if err != nil {
					return err
				}
			}
			if err := preflightProgramQuery(tx, q, options.Limit); err != nil {
				return err
			}
			rows := []repo.UnsRecord{}
			if err := q.Limit(options.Limit + 1).Find(&rows).Error; err != nil {
				return err
			}
			page.HasMore = len(rows) > options.Limit
			if page.HasMore {
				rows = rows[:options.Limit]
			}
			for _, row := range rows {
				page.Items = append(page.Items, row)
			}
			if page.HasMore {
				last := rows[len(rows)-1]
				values := []json.RawMessage{}
				for _, order := range orders {
					values = append(values, programRecordValue(last, order.Field))
				}
				page.NextCursor = encodeProgramCursor(programCursor{Binding: binding, Values: values})
			}
		} else {
			if doc.SampleSchema == nil {
				return invalid("kind", "Collection has no sample schema")
			}
			q = q.Model(&repo.UnsSample{}).Where("time>=? AND time<?", time.UnixMilli(options.TimeRange.From), time.UnixMilli(options.TimeRange.To)).Order("time ASC,id ASC,record_id ASC")
			if options.Cursor != "" {
				if !unsmeta.ValidID(cursor.ID) || !unsmeta.ValidID(cursor.RecordID) || len(cursor.Values) != 0 || cursor.Time < options.TimeRange.From || cursor.Time >= options.TimeRange.To {
					return invalid("cursor", "invalid sample anchor")
				}
				q = q.Where("(time,id,record_id)>(?,?,?)", time.UnixMilli(cursor.Time), cursor.ID, cursor.RecordID)
			}
			if err := preflightProgramQuery(tx, q, options.Limit); err != nil {
				return err
			}
			rows := []repo.UnsSample{}
			if err := q.Limit(options.Limit + 1).Find(&rows).Error; err != nil {
				return err
			}
			page.HasMore = len(rows) > options.Limit
			if page.HasMore {
				rows = rows[:options.Limit]
			}
			for _, row := range rows {
				page.Items = append(page.Items, map[string]any{"id": row.ID, "collectionId": row.CollectionID, "recordId": row.RecordID, "time": row.Time.UnixMilli(), "receivedAt": row.ReceivedAt.UnixMilli(), "quality": row.Quality, "data": row.Data, "sourceId": row.SourceID, "streamId": row.StreamID, "epoch": row.Epoch, "seq": row.Seq, "producerId": row.ProducerID})
			}
			if page.HasMore {
				last := rows[len(rows)-1]
				page.NextCursor = encodeProgramCursor(programCursor{Binding: binding, Time: last.Time.UnixMilli(), ID: last.ID, RecordID: last.RecordID})
			}
		}
		raw, err := json.Marshal(page)
		if err != nil {
			return err
		}
		if len(raw) > MaxProgramQueryBytes || len(page.NextCursor) > 8192 {
			return unsmeta.ErrData
		}
		return nil
	})
	return page, err
}

func preflightProgramQuery(tx, query *gorm.DB, limit int) error {
	var size int64
	page := query.Session(&gorm.Session{}).Select("data").Limit(limit + 1)
	if err := tx.Table("(?) AS stream_query_page", page).Select("COALESCE(SUM(octet_length(data::text)),0)").Scan(&size).Error; err != nil {
		return err
	}
	if size > MaxProgramQueryBytes {
		return unsmeta.ErrData
	}
	return nil
}

func programRecordValue(row repo.UnsRecord, field string) json.RawMessage {
	var value any
	switch field {
	case "id":
		value = row.ID
	case "createdAt":
		value = row.CreatedAt
	case "updatedAt":
		value = row.UpdatedAt
	case "revision":
		value = row.Revision
	default:
		// Preserve JSON numeric literals; do not round IDs or money in Go.
		value = json.RawMessage(row.Data)
		for _, part := range strings.Split(field, ".")[1:] {
			var object map[string]json.RawMessage
			if json.Unmarshal(value.(json.RawMessage), &object) != nil {
				value = nil
				break
			}
			child, ok := object[part]
			if !ok {
				value = nil
				break
			}
			value = child
		}
	}
	raw, _ := json.Marshal(value)
	return raw
}

func afterProgramRecord(q *gorm.DB, schema *unsmeta.Schema, orders []RecordOrder, cursor programCursor) (*gorm.DB, error) {
	if len(cursor.Values) != len(orders) || cursor.ID != "" || cursor.RecordID != "" || cursor.Time != 0 {
		return nil, invalid("cursor", "invalid record anchor")
	}
	parts, args := []string{}, []any{}
	prefix, prefixArgs := []string{}, []any{}
	for i, order := range orders {
		expr, vars, kind, err := recordField(schema, order.Field)
		if err != nil {
			return nil, err
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(cursor.Values[i]))
		decoder.UseNumber()
		if !json.Valid(cursor.Values[i]) || decoder.Decode(&value) != nil || value != nil && !filterValue(kind, value) {
			return nil, invalid("cursor", "invalid anchor value")
		}
		if value != nil {
			op := ">"
			if order.Direction == "desc" {
				op = "<"
			}
			term := "(" + expr + " " + op + " ? OR " + expr + " IS NULL)"
			all := append(append([]string{}, prefix...), term)
			parts = append(parts, "("+strings.Join(all, " AND ")+")")
			args = append(args, prefixArgs...)
			args = append(args, vars...)
			args = append(args, value)
			args = append(args, vars...)
		}
		prefix = append(prefix, expr+" IS NOT DISTINCT FROM ?")
		prefixArgs = append(prefixArgs, vars...)
		prefixArgs = append(prefixArgs, value)
	}
	if len(parts) == 0 {
		return nil, invalid("cursor", "record anchor requires stable ID")
	}
	return q.Where("("+strings.Join(parts, " OR ")+")", args...), nil
}
