-- Old cloud-edge models are retired. Never discard populated tables on upgrade.
DO $retire$
DECLARE table_name text; occupied boolean; populated text[] := ARRAY[]::text[];
BEGIN
 FOREACH table_name IN ARRAY ARRAY[
  'uns_stream_fleet_adoption',
  'uns_stream_fleet_center_release_event',
  'uns_stream_fleet_consumer',
  'uns_stream_fleet_control',
  'uns_stream_fleet_delivery_receipt',
  'uns_stream_fleet_deployment',
  'uns_stream_fleet_inbox',
  'uns_stream_fleet_local_publication',
  'uns_stream_fleet_outbox',
  'uns_stream_fleet_output',
  'uns_stream_fleet_producer',
  'uns_stream_fleet_publication_receipt',
  'uns_stream_fleet_resource',
  'uns_stream_fleet_shared_outbox',
  'uns_stream_fleet_source_config_event',
  'uns_stream_fleet_subscription',
  'uns_stream_fleet_upload_batch',
  'uns_fleet_resource_draft',
  'uns_fleet_resource_receipt',
  'uns_stream_acquisition',
  'uns_collection_storage_policy',
  'uns_fleet_control_gate'
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
 public.uns_stream_fleet_adoption,
 public.uns_stream_fleet_center_release_event,
 public.uns_stream_fleet_consumer,
 public.uns_stream_fleet_control,
 public.uns_stream_fleet_delivery_receipt,
 public.uns_stream_fleet_deployment,
 public.uns_stream_fleet_inbox,
 public.uns_stream_fleet_local_publication,
 public.uns_stream_fleet_outbox,
 public.uns_stream_fleet_output,
 public.uns_stream_fleet_producer,
 public.uns_stream_fleet_publication_receipt,
 public.uns_stream_fleet_resource,
 public.uns_stream_fleet_shared_outbox,
 public.uns_stream_fleet_source_config_event,
 public.uns_stream_fleet_subscription,
 public.uns_stream_fleet_upload_batch,
 public.uns_fleet_resource_draft,
 public.uns_fleet_resource_receipt,
 public.uns_stream_acquisition,
 public.uns_collection_storage_policy,
 public.uns_fleet_control_gate;

-- Preserve any customized limits; the new baseline creates the destination table.
INSERT INTO public.uns_storage_queue_budget(budget_id,max_events,max_deliveries,max_bytes)
 SELECT budget_id,max_events,max_deliveries,max_bytes FROM public.uns_stream_fleet_queue_budget
 ON CONFLICT(budget_id) DO UPDATE SET max_events=EXCLUDED.max_events,
 max_deliveries=EXCLUDED.max_deliveries,max_bytes=EXCLUDED.max_bytes;
DROP TABLE public.uns_stream_fleet_queue_budget;

DROP INDEX IF EXISTS public.fleet_one_edge_source_per_node;
