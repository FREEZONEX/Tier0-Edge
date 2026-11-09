-- Enterprise UNS edge synchronization selections and center mount points.
CREATE TABLE IF NOT EXISTS public.sync_mount (
    id text PRIMARY KEY,
    direction text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    fleet_node_id text NOT NULL,
    mount_point_id text NOT NULL DEFAULT '',
    selected_directory_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(selected_directory_ids) = 'array'),
    selected_collection_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(selected_collection_ids) = 'array'),
    scope_revision bigint NOT NULL DEFAULT 0 CHECK (scope_revision >= 0),
    enabled boolean NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_sync_mount_fleet_node ON public.sync_mount(fleet_node_id, direction);
