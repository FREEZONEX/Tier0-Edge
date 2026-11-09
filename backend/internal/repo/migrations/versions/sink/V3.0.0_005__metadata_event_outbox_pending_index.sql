-- The sender orders only pending events. A type/time index otherwise scans
-- every completed JSON payload before finding the next unsent event.
-- Preserve all history and idempotency identities; this is an additive index.
CREATE INDEX IF NOT EXISTS idx_uns_meta_event_outbox_pending
    ON public.uns_meta (created_at, id)
    WHERE kind = 'task'
      AND data->>'type' = 'metadataEventOutbox'
      AND data->'payload'->>'status' = 'pending';
