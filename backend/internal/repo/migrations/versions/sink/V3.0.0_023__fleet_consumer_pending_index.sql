CREATE INDEX IF NOT EXISTS fleet_inbox_consumer_pending ON public.uns_stream_fleet_inbox(consumer_id,accepted_at,sequence) WHERE committed_at=0 AND attempts<5;
