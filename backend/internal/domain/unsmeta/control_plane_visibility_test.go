package unsmeta_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

// Public Namespace endpoints must not expose private control-plane JSON,
// even when a caller knows an object's stable ID.
func TestControlPlaneMetadataIsNotPublicNamespace(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	service := unsmeta.New()
	for index, kind := range []string{"source", "source_state", "source_plan", "stream", "task"} {
		id := fmt.Sprintf("810%d", index)
		if kind == "source_state" {
			id = "8100"
		}
		row := repo.UnsMeta{ID: id, StorageKind: kind, Kind: kind,
			Data: json.RawMessage(`{"name":"private","credentials":{"ciphertext":"must-not-be-visible"}}`), Revision: 1}
		require.NoError(t, store.UnsDB().Create(&row).Error)
		_, err := service.View(ctx, id)
		require.Error(t, err, "control-plane kind %s must not be retrievable by Namespace ID", kind)
		_, err = service.List(ctx, kind, false, 1, 100)
		require.ErrorIs(t, err, unsmeta.ErrInvalid)
	}
	listed, err := service.List(ctx, "", false, 1, 100)
	require.NoError(t, err)
	raw, err := json.Marshal(listed)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "must-not-be-visible")
}
