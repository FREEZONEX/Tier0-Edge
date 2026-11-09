ALTER TABLE public.uns_control_grant ADD COLUMN allow_automatic BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE public.uns_fleet_control_intent ADD COLUMN automatic BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE public.uns_stream_fleet_center_release_event ADD COLUMN control_actor JSONB NOT NULL DEFAULT '{}';
ALTER TABLE public.uns_stream_fleet_delivery_receipt ADD COLUMN control_decisions JSONB NOT NULL DEFAULT '[]';
CREATE TABLE public.uns_fleet_control_gate (
 gate_id VARCHAR(64) PRIMARY KEY,
 invocation_id VARCHAR(64) NOT NULL,
 next_allowed_at BIGINT NOT NULL
);
