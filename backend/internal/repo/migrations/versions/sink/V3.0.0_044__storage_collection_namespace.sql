SELECT pg_advisory_xact_lock(814206108);
ALTER TABLE public.uns_meta ADD COLUMN IF NOT EXISTS origin_node_id text NOT NULL DEFAULT '';
UPDATE public.uns_meta m SET origin_node_id=i.node_id FROM public.uns_storage_inbound_collection i
 WHERE m.kind='collection' AND m.id=i.target_collection_id AND m.origin_node_id='';
DROP INDEX IF EXISTS public.uk_uns_meta_active_name;
CREATE UNIQUE INDEX uk_uns_meta_active_name ON public.uns_meta
 ((CASE WHEN kind IN ('directory','topic') THEN 'namespace' ELSE kind END),
 (CASE WHEN kind='collection' THEN COALESCE(mount_id,NULLIF(origin_node_id,''),'') ELSE '' END),
 (CASE WHEN mount_id IS NOT NULL THEN id ELSE '' END),
 COALESCE(data->>'parentId',''),lower(data->>'name'))
 WHERE deleted_at=0 AND kind IN ('directory','topic','collection','source');
