package repo_test

import (
	"context"
	"testing"

	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFreshMigrationUsesMetadataOnly(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	for _, name := range []string{"public.uns_source", "public.uns_source_credential", "public.uns_stream", "public.uns_stream_version", "public.source_stream_import"} {
		var exists bool
		require.NoError(t, store.CommonDB().Raw("SELECT to_regclass(?) IS NOT NULL", name).Scan(&exists).Error)
		require.False(t, exists, name)
	}
	var oldSchema bool
	require.NoError(t, store.UnsDB().Raw(`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='uns')`).Scan(&oldSchema).Error)
	require.False(t, oldSchema, "fresh UNS storage is unified in public, not a separate legacy schema")
	for _, name := range []string{"public.source_projection", "public.source_credential_projection", "public.program_stream_projection", "public.program_version_projection"} {
		var kind string
		require.NoError(t, store.UnsDB().Raw("SELECT relkind::text FROM pg_class WHERE oid=to_regclass(?)", name).Scan(&kind).Error)
		require.Equal(t, "v", kind, name)
	}
	require.NoError(t, store.Migrate(context.Background()))
}

// Retirement must survive restart: baseline DDL must not recreate retired models.
func TestFleetSchemaRetirementFreshAndRestart(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	for i := 0; i < 2; i++ {
		for _, name := range []string{
			"fleet_adoption",
			"fleet_batch_receipt",
			"fleet_collection_sync",
			"fleet_consumer_input_state",
			"fleet_delivery_version",
			"fleet_deployment",
			"fleet_draft_receipt",
			"fleet_ingress_policy",
			"fleet_ingress_policy_version",
			"fleet_managed_draft",
			"fleet_managed_release",
			"fleet_node_queue_budget",
			"fleet_offline_draft",
			"fleet_output_catalog",
			"fleet_publication_operation",
			"fleet_resource_catalog",
			"fleet_shared_delivery",
			"fleet_shared_inbox",
			"fleet_snapshot",
			"fleet_stream_group",
			"fleet_stream_preparation",
			"cluster_auth_token",
			"cluster_edge_node",
			"cluster_sync_cursor",
			"cluster_sync_config",
			"cluster_sync_scope",
			"uns_connect_node_info",
			"fleet_uns_scope_state",
			"uns_cloud_sync_nodes_mapping",
			"uns_cloud_sync_checkpoint",
			"uns_stream_fleet_adoption",
			"uns_stream_fleet_center_release_event",
			"uns_stream_fleet_consumer",
			"uns_stream_fleet_control",
			"uns_stream_fleet_delivery_receipt",
			"uns_stream_fleet_deployment",
			"uns_stream_fleet_inbox",
			"uns_stream_fleet_local_publication",
			"uns_stream_fleet_outbox",
			"uns_stream_fleet_output",
			"uns_stream_fleet_producer",
			"uns_stream_fleet_publication_receipt",
			"uns_stream_fleet_resource",
			"uns_stream_fleet_shared_outbox",
			"uns_stream_fleet_source_config_event",
			"uns_stream_fleet_subscription",
			"uns_stream_fleet_upload_batch",
			"uns_fleet_resource_draft",
			"uns_fleet_resource_receipt",
			"uns_stream_acquisition",
			"uns_collection_storage_policy",
			"uns_fleet_control_gate",
			"uns_stream_fleet_queue_budget",
		} {
			var exists bool
			require.NoError(t, store.CommonDB().Raw("SELECT to_regclass(?) IS NOT NULL", "public."+name).Scan(&exists).Error)
			require.False(t, exists, name)
		}
		for _, name := range []string{"uns_collection_storage_outbox", "uns_collection_storage_receipt", "uns_storage_queue_budget", "uns_fleet_control_intent", "fleet_node", "fleet_user_binding", "uns_cloud_sync_log", "uns_stream_run_history"} {
			var exists bool
			require.NoError(t, store.CommonDB().Raw("SELECT to_regclass(?) IS NOT NULL", "public."+name).Scan(&exists).Error)
			require.True(t, exists, name)
		}
		require.NoError(t, store.Migrate(context.Background()))
	}
}

func TestFleetSchemaRetirementProtectsExistingData(t *testing.T) {
	for _, item := range []struct{ scope, table string }{{"common", "fleet_managed_draft"}, {"sink", "uns_stream_fleet_outbox"}} {
		t.Run(item.scope, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			db := store.CommonDB()
			require.NoError(t, db.Exec("CREATE TABLE public."+item.table+"(id text PRIMARY KEY); INSERT INTO public."+item.table+" VALUES ('preserved')").Error)
			require.NoError(t, db.Exec("DELETE FROM sys_schema_migration WHERE scope=? AND name='retire_legacy_fleet_models'", item.scope).Error)
			err := store.Migrate(context.Background())
			require.ErrorContains(t, err, "FLEET_LEGACY_DATA_PRESENT")
			require.ErrorContains(t, err, item.table)
			var id string
			require.NoError(t, db.Raw("SELECT id FROM public."+item.table).Scan(&id).Error)
			require.Equal(t, "preserved", id)
		})
	}
}

func TestFleetSchemaRetirementPreservesQueueBudget(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.CommonDB()
	require.NoError(t, db.Exec(`CREATE TABLE uns_stream_fleet_queue_budget (LIKE uns_storage_queue_budget INCLUDING ALL);
 INSERT INTO uns_stream_fleet_queue_budget VALUES ('collection-storage',123,456,789);
 DELETE FROM sys_schema_migration WHERE scope='sink' AND name='retire_legacy_fleet_models';`).Error)
	require.NoError(t, store.Migrate(context.Background()))
	var maxEvents int64
	require.NoError(t, db.Raw("SELECT max_events FROM uns_storage_queue_budget WHERE budget_id='collection-storage'").Scan(&maxEvents).Error)
	require.EqualValues(t, 123, maxEvents)
	// Current storage guard still applies to direct writes.
	err := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("INSERT INTO uns_global_storage_policy VALUES ('edge','center',1,'test',1)").Error; e != nil {
			return e
		}
		return tx.Exec("INSERT INTO uns_record(collection_id,id,data) VALUES ('test','test','{}')").Error
	})
	require.ErrorContains(t, err, "COLLECTION_STORAGE_UNSUPPORTED")
}
