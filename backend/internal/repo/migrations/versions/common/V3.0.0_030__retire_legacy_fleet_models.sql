-- Old cloud-edge models are retired. Never discard populated tables on upgrade.
DO $retire$
DECLARE table_name text; occupied boolean; populated text[] := ARRAY[]::text[];
BEGIN
 FOREACH table_name IN ARRAY ARRAY[
  'fleet_adoption',
  'fleet_batch_receipt',
  'fleet_collection_sync',
  'fleet_consumer_input_state',
  'fleet_delivery_version',
  'fleet_deployment',
  'fleet_draft_receipt',
  'fleet_ingress_policy',
  'fleet_ingress_policy_version',
  'fleet_managed_draft',
  'fleet_managed_release',
  'fleet_node_queue_budget',
  'fleet_offline_draft',
  'fleet_output_catalog',
  'fleet_publication_operation',
  'fleet_resource_catalog',
  'fleet_shared_delivery',
  'fleet_shared_inbox',
  'fleet_snapshot',
  'fleet_stream_group',
  'fleet_stream_preparation',
  'cluster_auth_token',
  'cluster_edge_node',
  'cluster_sync_cursor',
  'cluster_sync_config',
  'cluster_sync_scope',
  'uns_connect_node_info',
  'fleet_uns_scope_state',
  'uns_cloud_sync_nodes_mapping',
  'uns_cloud_sync_checkpoint'
 ] LOOP
  IF to_regclass(format('public.%I',table_name)) IS NOT NULL THEN
   EXECUTE format('LOCK TABLE public.%I IN ACCESS EXCLUSIVE MODE',table_name);
   EXECUTE format('SELECT EXISTS(SELECT 1 FROM public.%I LIMIT 1)',table_name) INTO occupied;
   IF occupied THEN populated := array_append(populated,table_name); END IF;
  END IF;
 END LOOP;
 IF cardinality(populated)>0 THEN
  RAISE EXCEPTION 'FLEET_LEGACY_DATA_PRESENT: retire or export old data explicitly before upgrade: %', array_to_string(populated, ', ');
 END IF;
END $retire$;
DROP TABLE IF EXISTS
 public.fleet_adoption,
 public.fleet_batch_receipt,
 public.fleet_collection_sync,
 public.fleet_consumer_input_state,
 public.fleet_delivery_version,
 public.fleet_deployment,
 public.fleet_draft_receipt,
 public.fleet_ingress_policy,
 public.fleet_ingress_policy_version,
 public.fleet_managed_draft,
 public.fleet_managed_release,
 public.fleet_node_queue_budget,
 public.fleet_offline_draft,
 public.fleet_output_catalog,
 public.fleet_publication_operation,
 public.fleet_resource_catalog,
 public.fleet_shared_delivery,
 public.fleet_shared_inbox,
 public.fleet_snapshot,
 public.fleet_stream_group,
 public.fleet_stream_preparation,
 public.cluster_auth_token,
 public.cluster_edge_node,
 public.cluster_sync_cursor,
 public.cluster_sync_config,
 public.cluster_sync_scope,
 public.uns_connect_node_info,
 public.fleet_uns_scope_state,
 public.uns_cloud_sync_nodes_mapping,
 public.uns_cloud_sync_checkpoint;
