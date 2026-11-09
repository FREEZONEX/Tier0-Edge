CREATE TABLE IF NOT EXISTS uns_stream_fleet_queue_budget (
 budget_id TEXT PRIMARY KEY, max_events BIGINT NOT NULL CHECK(max_events>0),
 max_deliveries BIGINT NOT NULL CHECK(max_deliveries>0), max_bytes BIGINT NOT NULL CHECK(max_bytes>0)
);
SELECT public.protect_stream_runtime_storage();
