package unsv2

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
)

// CursorOptions never grants access and does not retain a database snapshot.
type CursorOptions struct {
	PaginationMode string `json:"paginationMode,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
	AsOf           int64  `json:"asOf,omitempty"`
	IncludeTotal   *bool  `json:"includeTotal,omitempty"`
}

func (o CursorOptions) present() bool {
	return o.PaginationMode != "" || o.Cursor != "" || o.AsOf != 0 || o.IncludeTotal != nil
}

type queryCursor struct {
	Version  int    `json:"v"`
	Scope    string `json:"scope"`
	AsOf     int64  `json:"asOf"`
	ID       string `json:"id"`
	RecordID string `json:"recordId,omitempty"`
	Time     string `json:"time,omitempty"`
}

func readQueryCursor(operation string, q Query) (queryCursor, error) {
	if q.PaginationMode != "cursor" || q.Page > 1 || q.AsOf < 0 || len(q.OrderBy) != 0 {
		return queryCursor{}, invalid("paginationMode", "cursor requires page omitted or 1, a nonnegative asOf, and no custom orderBy")
	}
	if operation == "samples" && q.TimeRange == nil {
		return queryCursor{}, invalid("timeRange", "Sample cursor queries require an explicit fixed timeRange")
	}
	// Page size and counting preference may change without moving the boundary.
	shape := q
	shape.CursorOptions = CursorOptions{}
	shape.QueryID, shape.Page, shape.Size = "", 0, 0
	shape.Mode = ""
	shape.Kinds = nil // resource and batch adapters express the same query
	raw, _ := json.Marshal(struct {
		Operation string
		Query     Query
	}{operation, shape})
	sum := sha256.Sum256(raw)
	scope := hex.EncodeToString(sum[:])
	c := queryCursor{Version: 1, Scope: scope, AsOf: q.AsOf}
	if q.Cursor != "" {
		if len(q.Cursor) > 2048 {
			return c, invalid("cursor", "cursor exceeds its size limit")
		}
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		var decoded queryCursor
		if err != nil || Decode(raw, &decoded) != nil || decoded.Version != 1 || decoded.Scope != scope || decoded.AsOf <= 0 || !unsmeta.ValidID(decoded.ID) || (q.AsOf != 0 && q.AsOf != decoded.AsOf) {
			return c, invalid("cursor", "invalid cursor or query/boundary changed; restart the traversal")
		}
		c = decoded
		if operation == "samples" {
			if _, err := time.Parse(time.RFC3339Nano, c.Time); err != nil || !unsmeta.ValidID(c.RecordID) {
				return c, invalid("cursor", "invalid Sample cursor identity")
			}
		}
	}
	now := time.Now().UnixMilli()
	if c.AsOf == 0 {
		c.AsOf = now
	}
	if c.AsOf > now {
		return c, invalid("asOf", "asOf cannot be in the future")
	}
	return c, nil
}

func cursorPage(list any, total int64, size int, more bool, q Query, c queryCursor) map[string]any {
	known := q.IncludeTotal == nil || *q.IncludeTotal
	var count any
	if known {
		count = total
	}
	out := map[string]any{"list": list, "total": count, "totalKnown": known, "page": 1, "size": size, "hasMore": more, "complete": q.Cursor == "" && !more, "truncated": false, "snapshotScope": "boundedLive", "asOf": c.AsOf}
	if more {
		raw, _ := json.Marshal(c)
		out["nextCursor"] = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out
}

func recordCursorQuery(ctx context.Context, query *gorm.DB, q Query, size int) (map[string]any, error) {
	c, err := readQueryCursor("records", q)
	if err != nil {
		return nil, err
	}
	query = query.Where("created_at<=?", c.AsOf)
	var total int64
	if q.IncludeTotal == nil || *q.IncludeTotal {
		if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, err
		}
	}
	if q.Cursor != "" {
		query = query.Where("id>?", c.ID)
	}
	rows := []repo.UnsRecord{}
	if err := query.Order("id ASC").Limit(size + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	more := len(rows) > size
	if more {
		rows = rows[:size]
	}
	if len(rows) > 0 {
		c.ID = rows[len(rows)-1].ID
	}
	out := cursorPage(rows, total, size, more, q, c)
	if q.IncludeLatestSample {
		ids := make([]string, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		latest, err := latestQuery(ctx, q.CollectionID, ids)
		if err != nil {
			return nil, err
		}
		out["latestSamples"] = latest
	}
	return out, nil
}

func sampleCursorQuery(query *gorm.DB, q Query, size int) (map[string]any, error) {
	c, err := readQueryCursor("samples", q)
	if err != nil {
		return nil, err
	}
	query = query.Where("received_at<=?", time.UnixMilli(c.AsOf))
	var total int64
	if q.IncludeTotal == nil || *q.IncludeTotal {
		if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, err
		}
	}
	if q.Cursor != "" {
		at, _ := time.Parse(time.RFC3339Nano, c.Time)
		query = query.Where("(time < ? OR (time = ? AND id < ?) OR (time = ? AND id = ? AND record_id > ?))", at, at, c.ID, at, c.ID, c.RecordID)
	}
	rows := []repo.UnsSample{}
	if err := query.Order("time DESC,id DESC,record_id ASC").Limit(size + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	more := len(rows) > size
	if more {
		rows = rows[:size]
	}
	list := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		list = append(list, sampleView(row))
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		c.ID, c.RecordID, c.Time = last.ID, last.RecordID, last.Time.UTC().Format(time.RFC3339Nano)
	}
	return cursorPage(list, total, size, more, q, c), nil
}
