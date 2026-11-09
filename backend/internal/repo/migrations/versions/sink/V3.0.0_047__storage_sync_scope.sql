CREATE TABLE IF NOT EXISTS public.uns_storage_sync_scope (
 id varchar(16) PRIMARY KEY,
 mode varchar(16) NOT NULL DEFAULT 'all' CHECK (mode IN ('all','selected')),
 collection_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
 revision bigint NOT NULL DEFAULT 0
);
REVOKE ALL ON public.uns_storage_sync_scope FROM PUBLIC;
ALTER TABLE public.uns_storage_sync_scope ENABLE ROW LEVEL SECURITY;
