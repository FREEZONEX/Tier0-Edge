package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/jackc/pgx/v5/pgconn"
	testrequire "github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A malformed legacy/raw value must not change which condition fails first.
// Use real PostgreSQL because SQL CASE evaluation and numeric casts matter.
func TestTransactionConditionsPreserveSQLShortCircuit(t *testing.T) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}}})
	metadata := unsmeta.New()
	created, err := metadata.CreateCollection(ctx, "", unsmeta.Document{Name: "condition-short-circuit", RecordSchema: &unsmeta.Schema{
		Type: "object", Properties: map[string]unsmeta.Schema{"label": {Type: "string"}, "amount": {Type: "number"}},
	}})
	testrequire.NoError(t, err)
	collectionID := created["collection"].(repo.UnsMeta).ID
	_, err = metadata.SaveRecord(ctx, collectionID, "row", 0, map[string]any{"label": "actual", "amount": 1})
	testrequire.NoError(t, err)
	// Deliberately model damaged pre-existing storage without weakening the API.
	testrequire.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("collection_id=? AND id=?", collectionID, "row").Update("data", json.RawMessage(`{"label":"actual","amount":"not-a-number"}`)).Error)
	service := New(metadata, nil)
	check := func(label string) error {
		return store.UnsDB().Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
				return err
			}
			if err := repo.LockUnsRecords(tx, repo.UnsRecordLock{CollectionID: collectionID, RecordID: "row"}); err != nil {
				return err
			}
			var change Change
			raw, err := json.Marshal(map[string]any{"opId": "check", "action": "record.check", "collectionId": collectionID, "id": "row", "conditions": []map[string]any{
				{"field": "data.label", "op": "eq", "value": label},
				{"field": "data.amount", "op": "gt", "value": 0},
			}})
			if err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &change); err != nil {
				return err
			}
			_, err = service.recordTransaction(repo.WithUnsTransaction(ctx, tx), tx, change)
			return err
		})
	}
	var fault *Fault
	testrequire.ErrorAs(t, check("different"), &fault)
	testrequire.Equal(t, "CONDITION_FAILED", fault.Code)
	testrequire.NotNil(t, fault.ConditionIndex)
	testrequire.Equal(t, 0, *fault.ConditionIndex)
	var pgError *pgconn.PgError
	testrequire.True(t, errors.As(check("actual"), &pgError), "the later predicate must still execute when prior conditions pass")
	testrequire.Equal(t, "22P02", pgError.Code)
}
