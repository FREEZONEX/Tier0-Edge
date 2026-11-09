-- Include in both the fresh UNS schema and the versioned UNS upgrade.
-- Capture is transactional with Sample writes; Record changes use Redis hints.
ALTER TABLE public.uns_record ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
COMMENT ON COLUMN public.uns_record.deleted_at IS 'Shared Record tombstone for local and mirrored deletions; retained Sample history keeps its Record identity.';
CREATE INDEX IF NOT EXISTS idx_uns_sample_sync_history
    ON public.uns_sample(collection_id, time, record_id, id);
CREATE TABLE IF NOT EXISTS public.sync_sample_capture (
    mount_id text NOT NULL,
    collection_id varchar(128) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (mount_id, collection_id)
);
CREATE INDEX IF NOT EXISTS idx_sync_sample_capture_collection
    ON public.sync_sample_capture(collection_id) WHERE enabled;
CREATE TABLE IF NOT EXISTS public.sync_outbox (
    id bigserial PRIMARY KEY,
    mount_id text NOT NULL,
    kind text NOT NULL DEFAULT 'sample',
    collection_id varchar(128) NOT NULL,
    record_id varchar(128) NOT NULL,
    sample_id varchar(128) NOT NULL,
    payload jsonb NOT NULL,
    lease_token text NOT NULL DEFAULT '',
    lease_until timestamptz NOT NULL DEFAULT '-infinity',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sync_outbox_claim
    ON public.sync_outbox(mount_id, kind, lease_until, id);
CREATE INDEX IF NOT EXISTS idx_sync_outbox_collection_claim
    ON public.sync_outbox(mount_id, collection_id, kind, lease_until, id);
CREATE OR REPLACE FUNCTION public.sync_capture_sample() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO public.sync_outbox(mount_id, collection_id, record_id, sample_id, payload)
    SELECT c.mount_id, NEW.collection_id, NEW.record_id, NEW.id,
           jsonb_build_object('collectionId', NEW.collection_id, 'recordId', NEW.record_id,
               'id', NEW.id, 'time', (extract(epoch FROM NEW.time) * 1000)::bigint,
               'receivedAt', (extract(epoch FROM NEW.received_at) * 1000)::bigint,
               'quality', NEW.quality, 'data', NEW.data,
               'record', jsonb_build_object('collectionId', r.collection_id, 'id', r.id,
                   'data', r.data, 'revision', r.revision, 'createdAt', r.created_at,
                   'updatedAt', r.updated_at))
    FROM public.sync_sample_capture c
    JOIN public.uns_record r ON r.collection_id=NEW.collection_id AND r.id=NEW.record_id
    WHERE c.collection_id = NEW.collection_id AND c.enabled;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS trg_sync_capture_sample ON public.uns_sample;
CREATE TRIGGER trg_sync_capture_sample AFTER INSERT ON public.uns_sample
    FOR EACH ROW EXECUTE FUNCTION public.sync_capture_sample();


CREATE SEQUENCE IF NOT EXISTS public.sync_send_sequence;
CREATE TABLE IF NOT EXISTS public.sync_mapping (
    mount_id text NOT NULL,
    source_id text NOT NULL,
    target_id text NOT NULL,
    kind text NOT NULL,
    generation text NOT NULL,
    updated_at bigint NOT NULL,
    PRIMARY KEY(mount_id,kind,source_id),
    UNIQUE(target_id)
);
CREATE INDEX IF NOT EXISTS idx_sync_mapping_generation ON public.sync_mapping(mount_id,kind,generation);
CREATE TABLE IF NOT EXISTS public.sync_receipt (
    mount_id text NOT NULL,
    batch_id text NOT NULL,
    payload_hash text NOT NULL,
    sequence bigint NOT NULL,
    scope_revision bigint NOT NULL,
    created_at bigint NOT NULL,
    PRIMARY KEY(mount_id,batch_id)
);
CREATE INDEX IF NOT EXISTS idx_sync_receipt_sequence ON public.sync_receipt(mount_id,sequence DESC);


CREATE TABLE IF NOT EXISTS public.sync_cursor (
 mount_id text NOT NULL,
 collection_id text NOT NULL,
 record_after text NOT NULL DEFAULT '',
 delete_after text NOT NULL DEFAULT '',
 history_from bigint NOT NULL DEFAULT 0,
 history_to bigint NOT NULL DEFAULT 0,
 history_cursor jsonb NOT NULL DEFAULT 'null',
 history_pending boolean NOT NULL DEFAULT false,
 PRIMARY KEY(mount_id,collection_id)
);
CREATE TABLE IF NOT EXISTS public.sync_record_state (
 mount_id text NOT NULL,
 collection_id text NOT NULL,
 record_id text NOT NULL,
 digest text NOT NULL,
 PRIMARY KEY(mount_id,collection_id,record_id)
);
