CREATE TABLE IF NOT EXISTS fleet_offline_draft (
 node_id TEXT NOT NULL, actor_id BIGINT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('source','stream')),
 resource_id TEXT NOT NULL, base_revision BIGINT NOT NULL, revision BIGINT NOT NULL,
 data JSONB NOT NULL, is_draft BOOLEAN NOT NULL, saved_at BIGINT NOT NULL,
 PRIMARY KEY(node_id,actor_id,kind,resource_id)
);
