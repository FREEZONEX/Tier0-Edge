-- Fleet payloads share the acquisition checkpoint / Collection transaction DB.
SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_output (
 output_id text PRIMARY KEY,
 source_id text NOT NULL,
 binding_id text NOT NULL,
 consumer_id text NOT NULL DEFAULT '',
 active boolean NOT NULL DEFAULT false,
 revision bigint NOT NULL DEFAULT 1,
 max_events bigint NOT NULL DEFAULT 10000 CHECK(max_events BETWEEN 1 AND 1000000),
 max_bytes bigint NOT NULL DEFAULT 67108864 CHECK(max_bytes BETWEEN 1024 AND 1073741824),
 UNIQUE(source_id,binding_id)
);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_outbox (
 output_id text NOT NULL,
 event_id text NOT NULL,
 consumer_id text NOT NULL,
 sequence bigint NOT NULL,
 received_at bigint NOT NULL,
 payload jsonb NOT NULL,
 payload_bytes bigint NOT NULL,
 accepted_at bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(output_id,event_id)
);
CREATE INDEX IF NOT EXISTS fleet_outbox_pending ON public.uns_stream_fleet_outbox(output_id,sequence) WHERE accepted_at=0;
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_control (
 source_id text PRIMARY KEY,
 revision bigint NOT NULL DEFAULT 1,
 local_paused boolean NOT NULL DEFAULT false,
 remote_paused boolean NOT NULL DEFAULT false,
 reason text NOT NULL DEFAULT '',
 updated_at bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_inbox (
 node_id text NOT NULL,
 output_id text NOT NULL,
 event_id text NOT NULL,
 consumer_id text NOT NULL,
 sequence bigint NOT NULL,
 received_at bigint NOT NULL,
 payload jsonb NOT NULL,
 payload_hash text NOT NULL,
 accepted_at bigint NOT NULL,
 committed_at bigint NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT '',
 PRIMARY KEY(node_id,output_id,event_id)
);
SELECT public.protect_stream_runtime_storage();
