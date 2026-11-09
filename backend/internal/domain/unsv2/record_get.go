package unsv2

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	docquery "backend/internal/domain/document/query"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
)

// Match Cloud's primary-key read and budget the projected response, including
// its envelope. Retain the Enterprise collection and App authorization boundary.
func (s *Service) recordGet(ctx context.Context, r novaRecordRequest) (any, error) {
	if len(r.IDs) < 1 || len(r.IDs) > 100 {
		return nil, invalid("ids", "1 to 100 unique record IDs required")
	}
	seen := map[string]bool{}
	for _, id := range r.IDs {
		if !unsmeta.ValidID(id) || seen[id] {
			return nil, invalid("ids", "valid unique IDs required")
		}
		seen[id] = true
	}
	projection, err := recordWriteProjection(ctx, r)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = repo.WithUnsTransaction(bounded, repo.GetUnsConn(ctx).WithContext(bounded))
	release, err := s.admitDataWork(ctx)
	if err != nil {
		return nil, pipelineRuntimeError(err)
	}
	defer release()
	var result map[string]any
	err = repo.GetUnsConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		var inner error
		result, inner = recordGetIn(repo.WithUnsTransaction(ctx, tx), tx, r.CollectionID, r.IDs, projection)
		return inner
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, pipelineRuntimeError(err)
	}
	return result, nil
}

func recordGetResponseTooLarge() error {
	return &Fault{Code: "RESPONSE_TOO_LARGE", Message: "record get exceeds 8 MiB; reduce ids or projected fields",
		RolledBack: true, Repair: "Request fewer ids or project fewer fields; do not retry the unchanged request."}
}

func recordGetIn(ctx context.Context, tx *gorm.DB, collectionID string, ids []string, projection *docquery.Projection) (map[string]any, error) {
	compiler := &pipelineCompiler{ctx: ctx}
	plan, err := compiler.source("records", collectionID, "")
	if err != nil {
		return nil, err
	}
	args := append(append([]any{}, plan.args...), ids)
	rows, err := tx.Raw("SELECT "+DataDocumentSQL("records", "b")+" AS doc FROM public.uns_record b WHERE "+plan.baseWhere+" AND b.id IN ?", args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]*RecordDocument{}
	responseBytes := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := decodeNumberJSON(raw, &item); err != nil {
			return nil, err
		}
		if projection != nil {
			projected := projection.Apply(item)
			projected["_id"], projected["_meta"] = item["_id"], item["_meta"]
			item = projected
		}
		document := recordQueryDocument(item)
		encoded, err := json.Marshal(document)
		if err != nil {
			return nil, err
		}
		responseBytes += len(encoded)
		if responseBytes > MaxQueryResponseBytes {
			return nil, recordGetResponseTooLarge()
		}
		found[document.ID] = document
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items, missing := make([]*RecordDocument, 0, len(ids)), make([]string, 0)
	for _, id := range ids {
		if doc, ok := found[id]; ok {
			items = append(items, doc)
		} else {
			missing = append(missing, id)
		}
	}
	result := map[string]any{"items": items, "missing": missing}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxQueryResponseBytes {
		return nil, recordGetResponseTooLarge()
	}
	return result, nil
}
