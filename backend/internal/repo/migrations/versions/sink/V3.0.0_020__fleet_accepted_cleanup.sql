SELECT pg_advisory_xact_lock(814206108);
CREATE INDEX IF NOT EXISTS fleet_outbox_accepted ON public.uns_stream_fleet_outbox(accepted_at) WHERE accepted_at>0;
