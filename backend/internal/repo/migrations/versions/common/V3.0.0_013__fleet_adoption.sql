CREATE TABLE IF NOT EXISTS fleet_adoption (
 operation_id TEXT PRIMARY KEY, node_id TEXT NOT NULL, local_source_id TEXT NOT NULL,
 managed_source_id TEXT NOT NULL, local_revision BIGINT NOT NULL,
 phase TEXT NOT NULL CHECK(phase IN ('queued','prepared','committed')),
 hash TEXT NOT NULL DEFAULT '', resource_ids JSONB NOT NULL, receipt JSONB NOT NULL,
 last_error TEXT NOT NULL DEFAULT '', created_by BIGINT NOT NULL, created_at BIGINT NOT NULL,
 checked_at BIGINT NOT NULL DEFAULT 0,
 UNIQUE(node_id,local_source_id), UNIQUE(node_id,managed_source_id)
);
