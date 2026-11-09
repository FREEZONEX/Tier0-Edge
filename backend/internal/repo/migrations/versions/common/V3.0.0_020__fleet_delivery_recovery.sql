ALTER TABLE public.fleet_shared_delivery ADD COLUMN replay_requested_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.fleet_shared_delivery ADD COLUMN replay_requested_by BIGINT NOT NULL DEFAULT 0;
