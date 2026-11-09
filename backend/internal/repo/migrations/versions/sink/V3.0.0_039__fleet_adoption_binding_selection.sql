ALTER TABLE uns_stream_fleet_adoption ADD COLUMN IF NOT EXISTS binding_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
SELECT public.protect_stream_runtime_storage();
