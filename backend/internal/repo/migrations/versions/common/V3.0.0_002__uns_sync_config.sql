ALTER TABLE cluster_sync_config ADD COLUMN IF NOT EXISTS uns_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cluster_sync_config ADD COLUMN IF NOT EXISTS selected_directory_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE cluster_sync_config ADD COLUMN IF NOT EXISTS selected_collection_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
