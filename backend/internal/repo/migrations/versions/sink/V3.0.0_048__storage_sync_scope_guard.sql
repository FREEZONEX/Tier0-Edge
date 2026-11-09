CREATE OR REPLACE FUNCTION public.uns_collection_storage_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cid text;
BEGIN
 IF TG_OP='DELETE' THEN cid:=OLD.collection_id; ELSE cid:=NEW.collection_id; END IF;
 IF EXISTS(SELECT 1 FROM public.uns_global_storage_policy WHERE id='edge' AND mode<>'local')
 AND NOT EXISTS(SELECT 1 FROM public.uns_storage_sync_scope WHERE id='edge' AND mode='selected' AND NOT collection_ids @> jsonb_build_array(cid))
 AND NOT EXISTS(SELECT 1 FROM public.uns_storage_inbound_collection WHERE target_collection_id=cid)
 AND NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='collection' AND id=cid AND mount_id IS NOT NULL)
 AND COALESCE(current_setting('public.collection_storage_write',true),'')<>cid THEN
 RAISE EXCEPTION 'COLLECTION_STORAGE_UNSUPPORTED: writer must use global storage policy' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
