CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_center_release_event (
 stream_id TEXT NOT NULL, release INTEGER NOT NULL, stream_revision BIGINT NOT NULL,
 definition JSONB NOT NULL, inputs JSONB NOT NULL, phase TEXT NOT NULL,
 last_error TEXT NOT NULL DEFAULT '', created_by BIGINT NOT NULL, created_at BIGINT NOT NULL,
 PRIMARY KEY(stream_id,release)
);
SELECT public.protect_stream_runtime_storage();
