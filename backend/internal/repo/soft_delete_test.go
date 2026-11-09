package repo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/contextx"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type softDeleteTestModel struct {
	ID   int64  `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name"`
	SoftTime
}

func (softDeleteTestModel) TableName() string {
	return "soft_delete_test"
}

func newSoftDeleteDryRunDB(t *testing.T) (*gorm.DB, time.Time) {
	t.Helper()
	now := time.Date(2026, time.July, 28, 12, 30, 0, 0, time.UTC)
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=test password=test dbname=test port=5432 sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db, now
}

func TestDeletedTimeAddsActiveQueryScope(t *testing.T) {
	db, _ := newSoftDeleteDryRunDB(t)

	var rows []softDeleteTestModel
	stmt := db.Where("name = ? OR name = ?", "first", "second").Find(&rows).Statement
	sql := stmt.SQL.String()
	if !strings.Contains(sql, `(name = $1 OR name = $2)`) {
		t.Fatalf("query did not preserve OR grouping: %s", sql)
	}
	if !strings.Contains(sql, `"soft_delete_test"."deleted_time" = $3`) {
		t.Fatalf("query did not add active soft-delete scope: %s", sql)
	}
	if got := stmt.Vars[len(stmt.Vars)-1]; got != 0 {
		t.Fatalf("active soft-delete value = %#v, want 0", got)
	}
}

func TestDeletedTimeUnscopedQueryIncludesDeletedRows(t *testing.T) {
	db, _ := newSoftDeleteDryRunDB(t)

	var rows []softDeleteTestModel
	stmt := db.Unscoped().Find(&rows).Statement
	if strings.Contains(stmt.SQL.String(), "deleted_time") {
		t.Fatalf("Unscoped query still contains deleted_time filter: %s", stmt.SQL.String())
	}
}

func TestDeletedTimeScopesUpdates(t *testing.T) {
	db, _ := newSoftDeleteDryRunDB(t)

	result := db.Model(&softDeleteTestModel{}).
		Where("id = ?", int64(7)).
		Update("name", "updated")
	if result.Error != nil {
		t.Fatalf("Update() error = %v", result.Error)
	}
	stmt := result.Statement
	sql := stmt.SQL.String()
	if !strings.Contains(sql, `"soft_delete_test"."deleted_time" =`) {
		t.Fatalf("update did not add active soft-delete scope: %s", sql)
	}

	unscopedResult := db.Unscoped().
		Model(&softDeleteTestModel{}).
		Where("id = ?", int64(7)).
		Update("name", "updated")
	if unscopedResult.Error != nil {
		t.Fatalf("Unscoped Update() error = %v", unscopedResult.Error)
	}
	unscoped := unscopedResult.Statement
	if strings.Contains(unscoped.SQL.String(), `"soft_delete_test"."deleted_time"`) {
		t.Fatalf("Unscoped update still contains deleted_time filter: %s", unscoped.SQL.String())
	}
}

func TestDeletedTimeConvertsDeleteToUpdate(t *testing.T) {
	db, now := newSoftDeleteDryRunDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 42})

	result := db.WithContext(ctx).Delete(&softDeleteTestModel{ID: 7})
	if result.Error != nil {
		t.Fatalf("Delete() error = %v", result.Error)
	}
	stmt := result.Statement
	sql := stmt.SQL.String()
	if !strings.HasPrefix(sql, `UPDATE "soft_delete_test" SET`) {
		t.Fatalf("soft delete SQL = %s, want UPDATE", sql)
	}
	if !strings.Contains(sql, `"deleted_time"=`) || !strings.Contains(sql, `"deleted_by"=`) {
		t.Fatalf("soft delete SQL is missing audit fields: %s", sql)
	}
	if !containsSoftDeleteVar(stmt.Vars, now.Unix()) {
		t.Fatalf("soft delete vars = %#v, want deleted_time %d", stmt.Vars, now.Unix())
	}
	if !containsSoftDeleteVar(stmt.Vars, int64(42)) {
		t.Fatalf("soft delete vars = %#v, want deleted_by 42", stmt.Vars)
	}

	hardDeleteResult := db.Unscoped().Delete(&softDeleteTestModel{ID: 7})
	if hardDeleteResult.Error != nil {
		t.Fatalf("Unscoped Delete() error = %v", hardDeleteResult.Error)
	}
	hardDelete := hardDeleteResult.Statement.SQL.String()
	if !strings.HasPrefix(hardDelete, `DELETE FROM "soft_delete_test"`) {
		t.Fatalf("Unscoped delete SQL = %s, want DELETE", hardDelete)
	}
}

func TestDeletedTimeJSONRemainsNumeric(t *testing.T) {
	raw, err := json.Marshal(DeletedTime(42))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(raw) != "42" {
		t.Fatalf("json.Marshal(DeletedTime(42)) = %s, want 42", raw)
	}
}

func TestDeletedTimeBusinessBypassQueriesRemainUnscoped(t *testing.T) {
	db, _ := newSoftDeleteDryRunDB(t)
	recorder := &softDeleteSQLRecorder{Interface: logger.Discard}
	db = db.Session(&gorm.Session{Logger: recorder})

	t.Run("UNS include deleted", func(t *testing.T) {
		recorder.Reset()
		_, _ = loadUnsNode(db, 7, true)
		sql := recorder.Last()
		if sql == "" || strings.Contains(sql, `"uns_namespace_node_info"."deleted_time" = 0`) {
			t.Fatalf("include-deleted UNS query is still scoped: %s", sql)
		}

		recorder.Reset()
		_, _ = loadUnsNode(db, 7, false)
		if sql := recorder.Last(); !strings.Contains(sql, `"uns_namespace_node_info"."deleted_time" = 0`) {
			t.Fatalf("active UNS query is missing soft-delete scope: %s", sql)
		}

		recorder.Reset()
		_, _ = NewUnsRepo(db).ListUnsNodes(context.Background(), UnsNodeFilter{IncludeRecycle: true})
		sql = recorder.Last()
		if !strings.Contains(sql, "deleted_time > 0") ||
			strings.Contains(sql, `AND "uns_namespace_node_info"."deleted_time" = 0`) {
			t.Fatalf("UNS recycle query cannot include deleted rows: %s", sql)
		}
	})

	t.Run("Fleet include deleted", func(t *testing.T) {
		repo := NewFleetRepo(db)
		recorder.Reset()
		_, _ = repo.GetNode(context.Background(), "fleet-1", true)
		if sql := recorder.Last(); sql == "" || strings.Contains(sql, `"fleet_node"."deleted_time" = 0`) {
			t.Fatalf("include-deleted Fleet query is still scoped: %s", sql)
		}

		recorder.Reset()
		_, _ = repo.GetNode(context.Background(), "fleet-1", false)
		if sql := recorder.Last(); !strings.Contains(sql, `"fleet_node"."deleted_time" = 0`) {
			t.Fatalf("active Fleet query is missing soft-delete scope: %s", sql)
		}
	})

	t.Run("cluster user sync sees deleted users", func(t *testing.T) {
		recorder.Reset()
		_, _, _, _ = clusterSyncUsersByID(db)
		if sql := recorder.Last(); sql == "" || strings.Contains(sql, `"sys_user_info"."deleted_time" = 0`) {
			t.Fatalf("cluster user sync query is still scoped: %s", sql)
		}
	})

	t.Run("API key registration can reuse a deleted row", func(t *testing.T) {
		recorder.Reset()
		err := NewAPIKeyRepo(db).RegisterAPIKey(
			context.Background(),
			"test-raw-key",
			"test-key",
			"",
			7,
			"personal",
			"read_only",
			"external",
			nil,
		)
		if err != nil {
			t.Fatalf("RegisterAPIKey() error = %v", err)
		}
		sql := recorder.Last()
		if sql == "" || !strings.Contains(sql, "key_hash =") ||
			strings.Contains(sql, `"sys_api_key"."deleted_time" = 0`) {
			t.Fatalf("API key reuse lookup is still scoped: %s", sql)
		}
	})

	t.Run("Fleet status count includes deleted nodes", func(t *testing.T) {
		recorder.Reset()
		_, _ = NewFleetRepo(db).CountNodesByStatus(context.Background())
		if sql := recorder.Last(); sql == "" || strings.Contains(sql, `"fleet_node"."deleted_time" = 0`) {
			t.Fatalf("Fleet status count query is still scoped: %s", sql)
		}
	})
}

func containsSoftDeleteVar(values []any, want int64) bool {
	for _, value := range values {
		switch typed := value.(type) {
		case int64:
			if typed == want {
				return true
			}
		case DeletedTime:
			if int64(typed) == want {
				return true
			}
		}
	}
	return false
}

type softDeleteSQLRecorder struct {
	logger.Interface
	statements []string
}

func (r *softDeleteSQLRecorder) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	r.statements = append(r.statements, statement)
}

func (r *softDeleteSQLRecorder) Last() string {
	if len(r.statements) == 0 {
		return ""
	}
	return r.statements[len(r.statements)-1]
}

func (r *softDeleteSQLRecorder) Reset() {
	r.statements = nil
}
