-- Support fair, ordered Collection batches without scanning completed events.
CREATE INDEX IF NOT EXISTS idx_collection_storage_pending_head ON public.uns_collection_storage_outbox(aggregate_id,id) WHERE status='pending';
