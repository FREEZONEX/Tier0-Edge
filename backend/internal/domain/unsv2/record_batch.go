package unsv2

import (
	"backend/internal/repo"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"regexp"
	"time"
)

// readTx keeps all batch results on one local snapshot and one execution budget.
func (s *Service) readTx(ctx context.Context, fn func(context.Context, *gorm.DB) error) error {
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = repo.WithUnsTransaction(bounded, repo.GetUnsConn(ctx).WithContext(bounded))
	release, err := s.admitDataWork(ctx)
	if err != nil {
		return err
	}
	defer release()
	return repo.GetUnsConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		return fn(repo.WithUnsTransaction(ctx, tx), tx)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

// batchQueryOps 是 queryBatch 可用的单项读操作
var batchQueryOps = []string{"find", "count", "distinct", "aggregate", "get"}

var batchQueryID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type recordBatchQuery struct {
	novaRecordRequest
	ID string `json:"id"`
}

type recordBatchRequest struct {
	Queries []json.RawMessage `json:"queries"`
}

// recordQueryBatch 在一个可重复读只读事务里执行最多 20 个查询（A3）。校验、编译与授权错误在开事务前逐项收集；
// 每项一个 SAVEPOINT，运行期 SQL 错误只影响该项；语句超时或整批读预算（2000 行、8 MiB）超限时整批失败。
func (s *Service) recordQueryBatch(ctx context.Context, raw []byte) (any, error) {
	var request recordBatchRequest
	if err := Decode(raw, &request); err != nil {
		return nil, err
	}
	if len(request.Queries) < 1 || len(request.Queries) > MaxQueries {
		return nil, invalid("queries", "1 to 20 queries required")
	}
	results := map[string]any{}
	type planned struct {
		id    string
		run   func(qctx context.Context, tx *gorm.DB, bytes *int) (any, error)
		index int
	}
	plans := []planned{}
	seen := map[string]bool{}
	rows := 0
	for i, rawQuery := range request.Queries {
		var q recordBatchQuery
		var head struct {
			ID string `json:"id"`
			Op string `json:"op"`
		}
		if json.Unmarshal(rawQuery, &head) != nil {
			return nil, invalid(fmt.Sprintf("queries[%d]", i), "JSON object required")
		}
		if !batchQueryID.MatchString(head.ID) || seen[head.ID] {
			return nil, invalid(fmt.Sprintf("queries[%d].id", i), "unique id matching ^[A-Za-z0-9_.-]{1,64}$ required")
		}
		seen[head.ID] = true
		fail := func(err error) {
			var fault *Fault
			if !errors.As(err, &fault) {
				fault = problem("INTERNAL_ERROR", "query failed").(*Fault)
			}
			copy := *fault
			// 开事务前的校验、编译与授权错误：该项没有执行（读操作的 execution 只会是 notStarted / rolledBack）
			copy.QueryID, copy.RolledBack = head.ID, true
			results[head.ID] = map[string]any{"ok": false, "error": &copy}
		}
		if err := validateBatchQuery(head.Op, rawQuery); err != nil {
			fail(err)
			continue
		}
		if err := Decode(rawQuery, &q); err != nil {
			fail(err)
			continue
		}
		run, size, err := s.batchQueryRunner(ctx, q, i)
		if err != nil {
			fail(err)
			continue
		}
		rows += size
		plans = append(plans, planned{id: head.ID, run: run, index: i})
	}
	if rows > 2000 {
		return nil, problem("RESPONSE_TOO_LARGE", "query batch row budget exceeds 2000")
	}
	if len(plans) > 0 {
		bytes := 0
		err := s.readTx(ctx, func(qctx context.Context, tx *gorm.DB) error {
			for _, plan := range plans {
				savepoint := fmt.Sprintf("batch_query_%d", plan.index)
				if err := tx.SavePoint(savepoint).Error; err != nil {
					return err
				}
				spent := bytes
				data, err := plan.run(qctx, tx, &bytes)
				if err != nil {
					// 回滚到 savepoint 的项不占整批的字节预算
					bytes = spent
					if batchFatal(err) {
						return err
					}
					if rollback := tx.RollbackTo(savepoint).Error; rollback != nil {
						return rollback
					}
					var fault *Fault
					if !errors.As(pipelineRuntimeError(err), &fault) {
						return err
					}
					copy := *fault
					copy.QueryID, copy.RolledBack = plan.id, true
					results[plan.id] = map[string]any{"ok": false, "error": &copy}
					continue
				}
				results[plan.id] = map[string]any{"ok": true, "data": data}
			}
			return nil
		})
		if err != nil {
			return nil, pipelineRuntimeError(err)
		}
	}
	out := map[string]any{"results": results}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxQueryResponseBytes {
		return nil, problem("RESPONSE_TOO_LARGE", "query batch exceeds 8 MiB; reduce page sizes or projected fields")
	}
	return out, nil
}

// batchFatal 语句超时与整批读预算超限让整批失败，不返回部分结果
func batchFatal(err error) bool {
	var fault *Fault
	if errors.As(pipelineRuntimeError(err), &fault) {
		return fault.Code == "QUERY_TIMEOUT" || fault.Code == "RESPONSE_TOO_LARGE"
	}
	return false
}

// batchQueryRunner 在开事务前完成单项的校验、编译与授权，返回在事务内执行的函数与该项的行预算。
func (s *Service) batchQueryRunner(ctx context.Context, q recordBatchQuery, index int) (func(context.Context, *gorm.DB, *int) (any, error), int, error) {
	r := q.novaRecordRequest
	if err := repo.RequireLocalCollectionRead(ctx, r.CollectionID); err != nil {
		return nil, 0, classify(err)
	}
	if err := authorizeCollection(ctx, "read", r.CollectionID); err != nil {
		return nil, 0, err
	}
	if len(r.Filter) == 0 {
		r.Filter = json.RawMessage(`{}`)
	}
	switch r.Op {
	case "get":
		if err := validateRecordIDs(r.IDs); err != nil {
			return nil, 0, err
		}
		projection, err := recordWriteProjection(ctx, r)
		if err != nil {
			return nil, 0, err
		}
		if len(r.IDs) < 1 || len(r.IDs) > 100 {
			return nil, 0, invalid("ids", "1 to 100 ids required")
		}
		return func(qctx context.Context, tx *gorm.DB, bytes *int) (any, error) {
			result, err := recordGetIn(qctx, tx, r.CollectionID, r.IDs, projection)
			if err != nil {
				return nil, err
			}
			// get 项与查询项共用整批字节预算
			raw, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			if *bytes += len(raw); *bytes > MaxQueryResponseBytes {
				return nil, problem("RESPONSE_TOO_LARGE", "query batch exceeds 8 MiB; reduce page sizes or projected fields")
			}
			return result, nil
		}, len(r.IDs), nil
	case "find", "count", "distinct", "aggregate":
		query, shape, err := recordReadQuery(ctx, r.Op, r)
		if err != nil {
			return nil, 0, err
		}
		query.QueryID = q.ID
		size, _, err := pipelinePagination("records", query)
		if err != nil {
			return nil, 0, err
		}
		if r.Op == "count" {
			size = 0
		}
		return func(qctx context.Context, tx *gorm.DB, bytes *int) (any, error) {
			result, err := s.runDataQuery(qctx, tx, "records", index, query, bytes)
			if err != nil {
				return nil, err
			}
			return shape(result), nil
		}, size, nil
	}
	return nil, 0, invalid("op", "op must be find, count, distinct, aggregate or get")
}

// validateBatchQuery 使用 queryBatch 的逐项模型校验，不依赖是否存在同名独立路由。
// distinct 仍属于批量查询能力，其单独公共入口退役不改变批内契约。
func validateBatchQuery(op string, raw []byte) error {
	if !contains(batchQueryOps, op) {
		return invalid("op", "op must be find, count, distinct, aggregate or get")
	}
	operation, found := FindResourceOperation("recordQueryBatch")
	if !found {
		return problem("INTERNAL_ERROR", "query batch contract is unavailable")
	}
	request, err := json.Marshal(recordBatchRequest{Queries: []json.RawMessage{raw}})
	if err != nil {
		return err
	}
	_, err = validateResourceRequest(operation, request)
	return err
}
