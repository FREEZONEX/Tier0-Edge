package unsmeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"backend/internal/fieldpath"
	"backend/internal/repo"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QueryIndex 是持久任务的内部编译结构；公开声明使用 IndexSpec。
type QueryIndex struct {
	Unique bool              `json:"unique,omitempty"`
	Spec   IndexSpec         `json:"spec"`
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Type   string            `json:"type"`
	Fields []QueryIndexField `json:"fields"`
}
type QueryIndexField struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}
type QueryIndexState struct {
	Spec      IndexSpec `json:"spec"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	ErrorCode string    `json:"errorCode,omitempty"`
	Message   string    `json:"message,omitempty"`
}
type queryIndexTask struct {
	Type         string     `json:"type"`
	CollectionID string     `json:"collectionId"`
	Index        QueryIndex `json:"index"`
	Schema       *Schema    `json:"schema"`
	Status       string     `json:"status"`
	ErrorCode    string     `json:"errorCode,omitempty"`
	Message      string     `json:"message,omitempty"`
}

const (
	queryIndexHypertableLockTimeout      = 5 * time.Second
	queryIndexHypertableStatementTimeout = 2 * time.Minute
)

// 根据实际存储判断 DDL 能力，安装扩展但尚未转换的普通表仍可并发建索引。
func queryIndexHypertable(db *gorm.DB, table string) (bool, error) {
	var catalogAvailable bool
	if err := db.Raw("SELECT to_regclass('timescaledb_information.hypertables') IS NOT NULL").Scan(&catalogAvailable).Error; err != nil || !catalogAvailable {
		return false, err
	}
	var hypertable bool
	err := db.Raw("SELECT EXISTS(SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_schema='public' AND hypertable_name=?)", table).Scan(&hypertable).Error
	return hypertable, err
}

// Hypertable 不支持并发创建索引，使用有界事务 DDL，超时设置随事务结束恢复。
func queryIndexDDL(db *gorm.DB, hypertable bool, statement string) error {
	if !hypertable {
		return db.Exec(statement).Error
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('lock_timeout', ?, true), set_config('statement_timeout', ?, true)", strconv.FormatInt(queryIndexHypertableLockTimeout.Milliseconds(), 10), strconv.FormatInt(queryIndexHypertableStatementTimeout.Milliseconds(), 10)).Error; err != nil {
			return err
		}
		return tx.Exec(statement).Error
	})
}

func queryIndexSchema(d Document, kind string) *Schema {
	if kind == "records" {
		return d.RecordSchema
	}
	return d.SampleSchema
}
func indexField(schema *Schema, kind, path string) (*Schema, string, error) {
	parts, err := ParseFieldPath(path)
	if err != nil || len(parts) == 0 {
		return nil, "", ErrSchema
	}
	columns := map[string]string{"_id": "id", "_meta.collectionId": "collection_id", "_meta.recordId": "record_id", "_meta.sourceId": "source_id", "_meta.sourceTime": "source_time", "_meta.streamId": "stream_id", "_meta.bindingId": "binding_id", "_meta.quality": "quality", "_meta.revision": "revision", "_meta.createdAt": "created_at", "_meta.updatedAt": "updated_at", "_meta.time": "time", "_meta.receivedAt": "received_at"}
	if column, ok := columns[path]; ok {
		typ := "string"
		if strings.Contains(path, "At") || strings.Contains(path, "Time") || strings.HasSuffix(path, ".time") || strings.HasSuffix(path, ".revision") {
			typ = "integer"
		}
		if kind == "records" && (column == "record_id" || column == "time" || column == "quality" || column == "stream_id" || column == "binding_id" || column == "received_at") {
			return nil, "", ErrSchema
		}
		if kind == "samples" && (column == "revision" || column == "created_at" || column == "updated_at" || column == "source_time") {
			return nil, "", ErrSchema
		}
		return &Schema{Type: typ}, column, nil
	}
	if parts[0] == "_id" || parts[0] == "_meta" {
		return nil, "", ErrSchema
	}
	field, ok := SchemaField(schema, parts)
	if !ok {
		return nil, "", ErrSchema
	}
	return field, "", nil
}
func ValidateQueryIndexes(d Document) error { return ValidateIndexes(d) }
func indexLiteral(s string) string          { return fieldpath.PGTextLiteral(s) }
func queryIndexID(collectionID string, index QueryIndex, schema *Schema) string {
	if index.Unique {
		schema = nil
	}
	raw, _ := json.Marshal([]any{collectionID, index, schema})
	sum := sha256.Sum256(raw)
	return "query-index-" + hex.EncodeToString(sum[:16])
}
func queryIndexNames(id string, index QueryIndex) []string {
	base := "qi_" + strings.TrimPrefix(id, "query-index-")
	if index.Unique {
		return []string{"ux_" + strings.TrimPrefix(id, "query-index-")}
	}
	if index.Type == "gin" {
		return []string{base + "_g"}
	}
	hasData := false
	for _, field := range index.Fields {
		parts, _ := ParseFieldPath(field.Field)
		if len(parts) > 0 && parts[0] != "_id" && parts[0] != "_meta" {
			hasData = true
			break
		}
	}
	if !hasData {
		return []string{base + "_v"}
	}
	return []string{base + "_j", base + "_v"}
}
func queryIndexExpressions(task queryIndexTask, scalar bool) ([]string, error) {
	terms := []string{}
	for _, field := range task.Index.Fields {
		s, column, err := indexField(task.Schema, task.Index.Kind, field.Field)
		if err != nil {
			return nil, err
		}
		expr := column
		if expr == "" {
			parts, _ := ParseFieldPath(field.Field)
			quoted := []string{}
			for _, part := range parts {
				quoted = append(quoted, indexLiteral(part))
			}
			expr = "jsonb_extract_path(data," + strings.Join(quoted, ",") + ")"
			if !scalar && task.Index.Type == "btree" && !task.Index.Unique {
				expr = "COALESCE(" + expr + ",'null'::jsonb)"
			}
			if scalar {
				expr = "(" + expr + " #>> '{}')"
				if s.Type == "number" || s.Type == "integer" {
					expr = "(" + expr + ")::numeric"
				} else if s.Type == "boolean" {
					expr = "(" + expr + ")::boolean"
				}
			}
		}
		term := "(" + expr + ")"
		if task.Index.Type == "btree" && !task.Index.Unique {
			term += " " + strings.ToUpper(field.Direction) + " NULLS LAST"
		}
		terms = append(terms, term)
	}
	return terms, nil
}

// Reconciliation intent is in the same UNS transaction as the collection.
// DDL never runs under the metadata writer lock or the caller's transaction.
func scheduleQueryIndexes(tx *gorm.DB, collectionID string, doc Document, retryFailed bool) error {
	var count int64
	if err := tx.Raw("SELECT COALESCE(sum(jsonb_array_length(COALESCE(data->'indexes','[]'::jsonb))),0) FROM public.uns_meta WHERE kind='collection' AND id<>?", collectionID).Scan(&count).Error; err != nil {
		return err
	}
	if count+int64(len(doc.Indexes)) > 200 {
		return ErrSchema
	}

	var existing []repo.UnsMeta
	if err := tx.Where("kind='index_state' AND data->>'type'='queryIndexTask' AND data->>'collectionId'=?", collectionID).Find(&existing).Error; err != nil {
		return err
	}
	var owner repo.UnsMeta
	mounted := false
	if err := tx.Select("mount_id").Where("kind='collection' AND id=?", collectionID).Take(&owner).Error; err == nil {
		mounted = owner.MountID != nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	wanted := map[string]queryIndexTask{}
	for _, index := range queryIndexes(doc) {
		if mounted && index.Unique {
			continue
		}
		schema := queryIndexSchema(doc, index.Kind)
		id := queryIndexID(collectionID, index, schema)
		wanted[id] = queryIndexTask{Type: "queryIndexTask", CollectionID: collectionID, Index: index, Schema: schema, Status: "pending"}
	}
	for _, row := range existing {
		var task queryIndexTask
		if err := json.Unmarshal(row.Data, &task); err != nil {
			return err
		}
		if _, ok := wanted[row.ID]; ok && (task.Status == "ready" || task.Status == "building" || task.Status == "pending" || task.Status == "failed" && !retryFailed) {
			valid := true
			if task.Status == "ready" {
				for _, name := range queryIndexNames(row.ID, task.Index) {
					if err := tx.Raw("SELECT COALESCE((SELECT indisvalid AND indisready FROM pg_index WHERE indexrelid=to_regclass(?)),false)", "public."+name).Scan(&valid).Error; err != nil {
						return err
					}
					if !valid {
						break
					}
				}
			}
			if valid {
				delete(wanted, row.ID)
				continue
			}
		}
		if _, ok := wanted[row.ID]; ok {
			continue
		}
		if task.Status == "removed" || task.Status == "removing" {
			continue
		}
		task.Status = "removing"
		task.ErrorCode = ""
		task.Message = ""
		raw, _ := json.Marshal(task)
		if err := tx.Model(&repo.UnsMeta{}).Where("kind='index_state' AND id=?", row.ID).Updates(map[string]any{"data": raw, "updated_at": time.Now().UnixMilli(), "revision": gorm.Expr("revision+1")}).Error; err != nil {
			return err
		}
	}
	for id, task := range wanted {
		raw, _ := json.Marshal(task)
		now := time.Now().UnixMilli()
		row := repo.UnsMeta{Kind: "index_state", ID: id, Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "kind"}, {Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{"data": raw, "updated_at": now, "revision": gorm.Expr("uns_meta.revision+1")})}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func RunQueryIndexWorker(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	cursor := ""
	nextScan := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			// Collection declarations may arrive through Fleet/import paths that do
			// not call Save. Page through persisted definitions so local physical
			// indexes converge without replicating their internal runtime tasks.
			var rows []repo.UnsMeta
			if time.Now().Before(nextScan) {
				// Continue queued DDL work between complete declaration scans.
			} else if err := repo.GetUnsConn(ctx).Where("kind='collection' AND id>?", cursor).Order("id").Limit(50).Find(&rows).Error; err != nil {
				if ctx.Err() == nil {
					logx.Errorf("UNS query index declarations: %v", err)
				}
			} else {
				for _, row := range rows {
					cursor = row.ID
					var doc Document
					if decodeNumbers(row.Data, &doc) != nil || ValidateQueryIndexes(doc) != nil {
						continue
					}
					if err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
						if err := lockMetadata(tx, true); err != nil {
							return err
						}
						var current repo.UnsMeta
						if err := tx.Where("kind='collection' AND id=?", row.ID).Take(&current).Error; err != nil {
							return err
						}
						if current.Revision != row.Revision {
							return nil
						}
						return scheduleQueryIndexes(tx, row.ID, doc, false)
					}); err != nil && ctx.Err() == nil {
						logx.Errorf("UNS query index schedule: %v", err)
					}
				}
				if len(rows) < 50 {
					cursor = ""
					nextScan = time.Now().Add(time.Minute)
				}
			}
			if err := ReconcileQueryIndex(ctx); err != nil && ctx.Err() == nil {
				logx.Errorf("UNS query index worker: %v", err)
			}
		}
	}
}

// ReconcileQueryIndex runs one durable task. A session advisory lock serializes
// concurrent builders without blocking metadata writers. Interrupted building
// rows are reclaimable immediately after the old database session releases it.
func ReconcileQueryIndex(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return repo.GetUnsConn(bounded).Connection(func(db *gorm.DB) error {
		// Connection supplies a pinned clone=0 handle. Fork each statement from
		// a clean session so a SELECT does not leak FROM/WHERE into later DDL or
		// state updates, while retaining the same physical advisory-lock session.
		db = db.Session(&gorm.Session{NewDB: true})
		var locked bool
		if err := db.Raw("SELECT pg_try_advisory_lock(814206112)").Scan(&locked).Error; err != nil || !locked {
			return err
		}
		defer func() {
			clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			db.WithContext(clean).Exec("SELECT pg_advisory_unlock(814206112)")
		}()
		// Hard-deleted collections no longer appear in declaration scans. Retire
		// their local indexes as well; archived collections retain their indexes
		// because their records are retained and can be restored.
		if err := db.Exec(`UPDATE public.uns_meta SET data=jsonb_set(data,'{status}','"removing"'::jsonb),revision=revision+1,updated_at=?
WHERE kind='index_state' AND id IN (SELECT t.id FROM public.uns_meta t WHERE t.kind='index_state' AND t.data->>'type'='queryIndexTask' AND t.data->>'status'<>'removing'
AND NOT EXISTS(SELECT 1 FROM public.uns_meta c WHERE c.kind='collection' AND c.id=t.data->>'collectionId') LIMIT 50)`, time.Now().UnixMilli()).Error; err != nil {
			return err
		}
		var row repo.UnsMeta
		err := db.Where("kind='index_state' AND data->>'type'='queryIndexTask' AND data->>'status' IN ('pending','building','removing')").Order("CASE WHEN data->>'status'='removing' THEN 1 ELSE 0 END,updated_at,id").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var task queryIndexTask
		if err = json.Unmarshal(row.Data, &task); err != nil {
			return err
		}
		removing := task.Status == "removing"

		if removing && task.Index.Unique {
			var current repo.UnsMeta
			if e := db.Where("kind='collection' AND id=?", task.CollectionID).Take(&current).Error; e == nil {
				var doc Document
				if e = decodeNumbers(current.Data, &doc); e != nil {
					return e
				}
				for _, replacement := range queryIndexes(doc) {
					if replacement.Unique {
						var ready bool
						id := queryIndexID(task.CollectionID, replacement, queryIndexSchema(doc, replacement.Kind))
						if e = db.Raw("SELECT EXISTS(SELECT 1 FROM public.uns_meta t JOIN pg_index p ON p.indexrelid=to_regclass(?) WHERE t.kind='index_state' AND t.id=? AND t.data->>'status'='ready' AND p.indisvalid AND p.indisready)", "public."+queryIndexNames(id, replacement)[0], id).Scan(&ready).Error; e != nil {
							return e
						}
						if !ready {
							return db.Model(&repo.UnsMeta{}).Where("kind='index_state' AND id=?", row.ID).Update("updated_at", time.Now().UnixMilli()).Error
						}
					}
				}
			} else if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
		}
		if !removing {
			task.Status = "building"
			raw, _ := json.Marshal(task)
			res := db.Model(&repo.UnsMeta{}).Where("kind='index_state' AND id=? AND revision=?", row.ID, row.Revision).Updates(map[string]any{"data": raw, "updated_at": time.Now().UnixMilli(), "revision": row.Revision + 1})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil
			}
			row.Revision++
		}
		tableName := "uns_record"
		if task.Index.Kind == "samples" {
			tableName = "uns_sample"
		}
		hypertable, err := queryIndexHypertable(db, tableName)
		if err != nil {
			return err
		}
		concurrently := " CONCURRENTLY"
		if hypertable {
			concurrently = ""
		}
		names := queryIndexNames(row.ID, task.Index)
		for i, name := range names {
			var valid bool
			err = db.Raw("SELECT COALESCE((SELECT indisvalid AND indisready FROM pg_index WHERE indexrelid=to_regclass(?)),false)", "public."+name).Scan(&valid).Error
			if err != nil {
				break
			}
			if removing || !valid {
				err = queryIndexDDL(db, hypertable, "DROP INDEX"+concurrently+" IF EXISTS public."+name)
				if err != nil {
					break
				}
			}
			if removing || valid {
				continue
			}
			var terms []string
			terms, err = queryIndexExpressions(task, i == 1)
			if err != nil {
				break
			}
			predicate := "collection_id=" + indexLiteral(task.CollectionID)
			if task.Index.Kind == "records" {
				predicate += " AND deleted_at IS NULL"
			}
			unique := ""
			if task.Index.Unique {
				unique = "UNIQUE "
			}
			err = queryIndexDDL(db, hypertable, "CREATE "+unique+"INDEX"+concurrently+" "+name+" ON public."+tableName+" USING "+task.Index.Type+" ("+strings.Join(terms, ",")+") WHERE "+predicate)
			if err != nil {
				break
			}
		}

		if err != nil && !removing && bounded.Err() == nil {
			for _, name := range names {
				var valid bool
				if e := db.Raw("SELECT COALESCE((SELECT indisvalid FROM pg_index WHERE indexrelid=to_regclass(?)),false)", "public."+name).Scan(&valid).Error; e != nil {
					return e
				}
				if !valid {
					if e := queryIndexDDL(db, hypertable, "DROP INDEX"+concurrently+" IF EXISTS public."+name); e != nil {
						return e
					}
				}
			}
		}
		if err == nil && !removing {
			for _, name := range names {
				var valid bool
				if checkErr := db.Raw("SELECT COALESCE((SELECT indisvalid AND indisready FROM pg_index WHERE indexrelid=to_regclass(?)),false)", "public."+name).Scan(&valid).Error; checkErr != nil {
					err = checkErr
					break
				}
				if !valid {
					err = errors.New("index build did not produce a valid index")
					break
				}
			}
		}
		if bounded.Err() != nil {
			return bounded.Err()
		} // retain building for restart/retry
		if err == nil && removing {
			return db.Where("kind='index_state' AND id=? AND revision=?", row.ID, row.Revision).Delete(&repo.UnsMeta{}).Error
		}
		task.Status = "ready"
		if removing {
			task.Status = "removed"
		}
		if err != nil {
			task.Status = "failed"
			task.ErrorCode = "INDEX_BUILD_FAILED"
			task.Message = "Index construction failed; check declared field types and database resources, then save indexes again to retry"
			var pg *pgconn.PgError
			if errors.As(err, &pg) {
				task.ErrorCode = "INDEX_BUILD_" + pg.Code
			}
		}
		raw, _ := json.Marshal(task)
		return db.Model(&repo.UnsMeta{}).Where("kind='index_state' AND id=? AND revision=?", row.ID, row.Revision).Updates(map[string]any{"data": raw, "updated_at": time.Now().UnixMilli(), "revision": gorm.Expr("revision+1")}).Error
	})
}

func QueryIndexStates(ctx context.Context, collectionID string, doc Document) ([]QueryIndexState, error) {
	var owner repo.UnsMeta
	if err := repo.GetUnsConn(ctx).Select("mount_id").Where("kind='collection' AND id=?", collectionID).Take(&owner).Error; err != nil {
		return nil, err
	}
	states := make([]QueryIndexState, 0, len(queryIndexes(doc)))
	for _, index := range queryIndexes(doc) {
		id := queryIndexID(collectionID, index, queryIndexSchema(doc, index.Kind))
		state := QueryIndexState{Name: index.Name, Kind: index.Kind, Status: "pending", Spec: index.Spec}
		if owner.MountID != nil && index.Unique {
			state.Status = "managedAtSource"
			states = append(states, state)
			continue
		}
		var row repo.UnsMeta
		err := repo.GetUnsConn(ctx).Where("kind='index_state' AND id=?", id).Take(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			var task queryIndexTask
			if err = json.Unmarshal(row.Data, &task); err != nil {
				return nil, err
			}
			state.Status = task.Status
			state.ErrorCode = task.ErrorCode
			state.Message = task.Message
		}
		if state.Status == "ready" {
			for _, name := range queryIndexNames(id, index) {
				var valid bool
				if err := repo.GetUnsConn(ctx).Raw("SELECT COALESCE((SELECT indisvalid AND indisready FROM pg_index WHERE indexrelid=to_regclass(?)),false)", "public."+name).Scan(&valid).Error; err != nil {
					return nil, err
				}
				if !valid {
					state.Status = "failed"
					state.ErrorCode = "INDEX_NOT_READY"
					state.Message = "Physical index is unavailable; save indexes again to rebuild"
					break
				}
			}
		}
		states = append(states, state)
	}
	// Removed declarations remain observable while their physical cleanup is
	// pending or failed. Superseded versions of an existing name stay internal.
	var retiring []repo.UnsMeta
	if err := repo.GetUnsConn(ctx).Where("kind='index_state' AND data->>'type'='queryIndexTask' AND data->>'collectionId'=? AND data->>'status' IN ('removing','failed')", collectionID).Find(&retiring).Error; err != nil {
		return nil, err
	}
	for _, row := range retiring {
		var task queryIndexTask
		if err := json.Unmarshal(row.Data, &task); err != nil {
			return nil, err
		}
		declared := false
		for _, index := range queryIndexes(doc) {
			if index.Name == task.Index.Name {
				declared = true
				break
			}
		}
		if declared {
			continue
		}
		states = append(states, QueryIndexState{Name: task.Index.Name, Kind: task.Index.Kind, Status: task.Status, ErrorCode: task.ErrorCode, Message: task.Message, Spec: task.Index.Spec})
	}
	return states, nil
}

// ReadyQueryIndex requires the exact declared configuration and valid physical
// index. A failed/missing build is never advertised as usable for a lookup.
func ReadyQueryIndex(ctx context.Context, collectionID, kind, field string) (bool, error) {
	_, doc, err := readCollection(ctx, ctx, collectionID)
	if err != nil {
		return false, err
	}
	parts, err := ParseFieldPath(field)
	if err != nil {
		return false, err
	}
	canonical := FormatFieldPath(parts)
	for _, index := range queryIndexes(doc) {
		if index.Unique || index.Kind != kind || index.Type != "btree" || len(index.Fields) == 0 {
			continue
		}
		first, _ := ParseFieldPath(index.Fields[0].Field)
		if FormatFieldPath(first) != canonical {
			continue
		}
		id := queryIndexID(collectionID, index, queryIndexSchema(doc, kind))
		names := queryIndexNames(id, index)
		var ready bool
		err := repo.GetUnsConn(ctx).Raw("SELECT EXISTS(SELECT 1 FROM public.uns_meta t JOIN pg_index p ON p.indexrelid=to_regclass(?) WHERE t.kind='index_state' AND t.id=? AND t.data->>'status'='ready' AND p.indisvalid AND p.indisready)", "public."+names[0], id).Scan(&ready).Error
		if err != nil || ready {
			return ready, err
		}
	}
	return false, nil
}

// ReadyUniqueIndex 同时验证配置、持久状态和物理索引，不能仅凭已保存的声明准入。
func ReadyUniqueIndex(ctx context.Context, collectionID string, equalities map[string]bool) (bool, error) {
	_, doc, err := readCollection(ctx, ctx, collectionID)
	if err != nil {
		return false, err
	}
	for _, index := range queryIndexes(doc) {
		if !index.Unique {
			continue
		}
		covered := true
		for _, field := range index.Fields {
			parts, _ := ParseFieldPath(field.Field)
			covered = covered && equalities[FormatFieldPath(parts)]
		}
		if !covered {
			continue
		}
		id := queryIndexID(collectionID, index, queryIndexSchema(doc, "records"))
		name := queryIndexNames(id, index)[0]
		var ready bool
		err := repo.GetUnsConn(ctx).Raw("SELECT EXISTS(SELECT 1 FROM public.uns_meta t JOIN pg_index p ON p.indexrelid=to_regclass(?) WHERE t.kind='index_state' AND t.id=? AND t.data->>'status'='ready' AND p.indisunique AND p.indisvalid AND p.indisready)", "public."+name, id).Scan(&ready).Error
		if err != nil || ready {
			return ready, err
		}
	}
	return false, nil
}

// Physical indexes are retired only after permanent data deletion, preserving trash reads.
func retireQueryIndexes(tx *gorm.DB, collectionID string) error {
	return tx.Exec(`UPDATE public.uns_meta SET data=jsonb_set(data,'{status}','"removing"'::jsonb)-'errorCode'-'message',revision=revision+1,updated_at=?
WHERE kind='index_state' AND data->>'type'='queryIndexTask' AND data->>'collectionId'=? AND data->>'status' NOT IN ('removing','removed')`, time.Now().UnixMilli(), collectionID).Error
}
