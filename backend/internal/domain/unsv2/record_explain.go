package unsv2

import (
	repo "backend/internal/repo"
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"time"
)

// 评估只读取有界匹配结果并返回数据库实际计划，不执行任何写入。
func (s *Service) recordExplain(ctx context.Context, r novaRecordRequest) (any, error) {
	if r.Op == "" {
		r.Op = "find"
	}
	if !contains([]string{"find", "updateMany", "deleteMany"}, r.Op) {
		return nil, invalid("op", "expected find, updateMany or deleteMany")
	}
	if err := authorizeCollection(ctx, "read", r.CollectionID); err != nil {
		return nil, err
	}
	if r.Op != "find" {
		op := "write"
		if r.Op == "deleteMany" {
			op = "delete"
		}
		if err := authorizeCollection(ctx, op, r.CollectionID); err != nil {
			return nil, err
		}
	}
	limit := r.Limit
	if limit == 0 {
		limit = 1000
	}
	if limit < 1 || limit > 10000 {
		return nil, invalid("limit", "limit must be 1 to 10000")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := map[string]any{}
	err := repo.GetUnsConn(ctx).WithContext(bounded).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		qctx := repo.WithUnsTransaction(bounded, tx)
		predicate, args, err := CompileDataFilter(qctx, "records", r.CollectionID, "base:r", r.Filter)
		if err != nil {
			return err
		}
		order, err := CompileDataSort(qctx, "records", r.CollectionID, "base:r", r.Sort)
		if err != nil {
			return err
		}
		query := tx.Table("public.uns_record AS r").Where("r.collection_id=? AND r.deleted_at IS NULL", r.CollectionID).Where(predicate, args...).Select("r.id")
		if order != "" {
			query = query.Order(order)
		}
		query = query.Order("r.id").Limit(limit + 1)
		var ids []string
		if err := query.Pluck("r.id", &ids).Error; err != nil {
			return err
		}
		statement := query.Session(&gorm.Session{DryRun: true}).Find(&[]repo.UnsRecord{}).Statement
		var planRaw []byte
		if err := tx.Raw("EXPLAIN (FORMAT JSON) "+statement.SQL.String(), statement.Vars...).Row().Scan(&planRaw); err != nil {
			return err
		}
		var plans []map[string]any
		if err := json.Unmarshal(planRaw, &plans); err != nil {
			return err
		}
		plan := map[string]any{}
		if len(plans) > 0 {
			plan = plans[0]
		}
		capped := len(ids) > limit
		if capped {
			ids = ids[:limit]
		}
		if ids == nil {
			ids = []string{}
		}
		matched := len(ids)
		if len(ids) > 100 {
			ids = ids[:100]
		}
		result = map[string]any{"op": r.Op, "matchedCount": matched, "capped": capped, "sampleIds": ids, "plan": plan, "writesCommitted": false, "executionGuaranteed": false}
		return nil
	})
	return result, classify(err)
}
