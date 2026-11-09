ALTER TABLE public.uns_storage_collection_mapping ADD COLUMN IF NOT EXISTS last_attempt_at bigint NOT NULL DEFAULT 0;
