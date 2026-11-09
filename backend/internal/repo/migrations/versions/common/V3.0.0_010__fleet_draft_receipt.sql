-- Save result and authoritative revision commit in the same common-DB transaction.
CREATE TABLE IF NOT EXISTS fleet_draft_receipt (
 node_id TEXT NOT NULL, idempotency_key VARCHAR(128) NOT NULL,
 request_hash TEXT NOT NULL, actor_id BIGINT NOT NULL,
 result JSONB NOT NULL, created_at BIGINT NOT NULL,
 PRIMARY KEY (node_id, idempotency_key)
);
