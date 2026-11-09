CREATE TABLE IF NOT EXISTS fleet_managed_draft (
 node_id text NOT NULL,
 resource_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('source','stream')),
 revision bigint NOT NULL,
 data jsonb NOT NULL,
 updated_by bigint NOT NULL,
 updated_at bigint NOT NULL,
 PRIMARY KEY(node_id,resource_id,kind)
);
CREATE TABLE IF NOT EXISTS fleet_managed_release (
 node_id text NOT NULL,
 stream_id text NOT NULL,
 release bigint NOT NULL,
 draft_revision bigint NOT NULL,
 definition jsonb NOT NULL,
 published_at bigint NOT NULL,
 PRIMARY KEY(node_id,stream_id,release),
 UNIQUE(node_id,stream_id,draft_revision)
);
CREATE TABLE IF NOT EXISTS fleet_snapshot (
 snapshot_id text PRIMARY KEY,
 node_id text NOT NULL,
 source_id text NOT NULL,
 source_revision bigint NOT NULL,
 hash text NOT NULL,
 payload jsonb NOT NULL,
 created_at bigint NOT NULL,
 UNIQUE(node_id,source_id,hash)
);
CREATE TABLE IF NOT EXISTS fleet_deployment (
 operation_id text PRIMARY KEY,
 node_id text NOT NULL,
 source_id text NOT NULL,
 snapshot_id text NOT NULL,
 generation bigint NOT NULL,
 idempotency_key text NOT NULL,
 phase text NOT NULL DEFAULT 'queued',
 last_error text NOT NULL DEFAULT '',
 observed jsonb NOT NULL DEFAULT '{}',
 created_by bigint NOT NULL,
 created_at bigint NOT NULL,
 checked_at bigint NOT NULL DEFAULT 0,
 UNIQUE(node_id,source_id,generation),
 UNIQUE(node_id,idempotency_key)
);
