CREATE TABLE IF NOT EXISTS fleet_node_queue_budget (
 budget_id TEXT PRIMARY KEY, max_events BIGINT NOT NULL CHECK(max_events>0),
 max_deliveries BIGINT NOT NULL CHECK(max_deliveries>0), max_bytes BIGINT NOT NULL CHECK(max_bytes>0)
);
ALTER TABLE fleet_consumer_input_state ADD COLUMN IF NOT EXISTS last_attempt_at BIGINT NOT NULL DEFAULT 0;
