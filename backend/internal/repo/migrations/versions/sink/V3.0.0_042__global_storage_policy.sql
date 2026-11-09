SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_global_storage_policy (
 id text PRIMARY KEY CHECK(id='edge'), mode text NOT NULL CHECK(mode IN ('local','center','both')),
 revision bigint NOT NULL, center_id text NOT NULL DEFAULT '', updated_at bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS public.uns_storage_collection_mapping (
 collection_id text PRIMARY KEY, center_id text NOT NULL, target_collection_id text NOT NULL,
 source_revision bigint NOT NULL DEFAULT 0, target_revision bigint NOT NULL DEFAULT 0,
 last_sync_at bigint NOT NULL DEFAULT 0, last_error text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS public.uns_storage_inbound_collection (
 node_id text NOT NULL, collection_id text NOT NULL, target_collection_id text NOT NULL UNIQUE,
 source_revision bigint NOT NULL, target_revision bigint NOT NULL, source_hash text NOT NULL,
 PRIMARY KEY(node_id,collection_id)
);
REVOKE ALL ON public.uns_global_storage_policy,public.uns_storage_collection_mapping,public.uns_storage_inbound_collection FROM PUBLIC;
ALTER TABLE public.uns_global_storage_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.uns_storage_collection_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.uns_storage_inbound_collection ENABLE ROW LEVEL SECURITY;
CREATE OR REPLACE FUNCTION public.uns_collection_storage_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cid text;
BEGIN
 IF TG_OP='DELETE' THEN cid:=OLD.collection_id; ELSE cid:=NEW.collection_id; END IF;
 IF EXISTS(SELECT 1 FROM public.uns_global_storage_policy WHERE id='edge' AND mode<>'local')
 AND NOT EXISTS(SELECT 1 FROM public.uns_storage_inbound_collection WHERE target_collection_id=cid)
 AND NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='collection' AND id=cid AND mount_id IS NOT NULL)
 AND COALESCE(current_setting('public.collection_storage_write',true),'')<>cid THEN
 RAISE EXCEPTION 'COLLECTION_STORAGE_UNSUPPORTED: writer must use global storage policy' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
