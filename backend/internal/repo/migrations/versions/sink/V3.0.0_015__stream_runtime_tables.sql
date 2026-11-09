-- This replaces an unpublished Record/Sample candidate, not an applied release.
-- All runtime state remains private in the same UNS database/transaction.
SET LOCAL lock_timeout = '10s';
-- Upgrade requires all old Backend workers to be stopped. Freeze legacy DML
-- before taking snapshots; fail rather than invert a live writer's gate order.
LOCK TABLE public.uns_meta IN SHARE ROW EXCLUSIVE MODE;
DO $write_gate$
BEGIN
 IF NOT pg_try_advisory_xact_lock(814206108) THEN
  RAISE EXCEPTION 'stop UNS writers before Stream runtime cutover' USING ERRCODE='55P03';
 END IF;
END $write_gate$;
DO $candidate_guard$
BEGIN
 IF EXISTS (SELECT 1 FROM public.sys_schema_migration WHERE scope='sink' AND version='3.0.0'
     AND sequence=15 AND name='stream_runtime_records' AND success) THEN
  RAISE EXCEPTION 'unpublished Stream Record/Sample candidate detected; explicit restoration is required';
 END IF;
END $candidate_guard$;

-- Preserve the original task identity, JSONB, revision and timestamps. The
-- envelope is an internal adapter, not an extensible generic job registry.
CREATE TABLE public.uns_stream_checkpoint (
    id varchar(256) PRIMARY KEY,
    data jsonb NOT NULL CHECK (data ?& ARRAY['type','payload'] AND data->>'type' IS NOT NULL AND jsonb_typeof(data)='object' AND data->>'type'='programCheckpoint'
        AND jsonb_typeof(data->'payload')='object'),
    revision bigint NOT NULL CHECK (revision>0),
    created_at bigint NOT NULL, updated_at bigint NOT NULL
);
CREATE TABLE public.uns_stream_commit_receipt (
    id varchar(256) PRIMARY KEY,
    data jsonb NOT NULL CHECK (data ?& ARRAY['type','payload'] AND data->>'type' IS NOT NULL AND jsonb_typeof(data)='object'
        AND data->>'type' IN ('ingestReceipt','programMetadataCommit','programCheckpointCommit')
        AND jsonb_typeof(data->'payload')='object'
        AND (data->'payload') ? 'digest' AND data->'payload'->>'digest' IS NOT NULL AND data->'payload'->>'digest' ~ '^[0-9a-f]{64}$'),
    revision bigint NOT NULL CHECK (revision>0),
    created_at bigint NOT NULL, updated_at bigint NOT NULL
);
CREATE TABLE public.uns_metadata_event_outbox (
    id varchar(256) PRIMARY KEY,
    data jsonb NOT NULL CHECK (data ?& ARRAY['type','payload'] AND data->>'type' IS NOT NULL AND jsonb_typeof(data)='object' AND data->>'type'='metadataEventOutbox'
        AND jsonb_typeof(data->'payload')='object'
        AND (data->'payload') ? 'status' AND data->'payload'->>'status' IS NOT NULL AND data->'payload'->>'status' IN ('pending','sent','expired')),
    revision bigint NOT NULL CHECK (revision>0),
    created_at bigint NOT NULL, updated_at bigint NOT NULL
);
CREATE INDEX idx_uns_metadata_outbox_pending ON public.uns_metadata_event_outbox(created_at,id)
    WHERE data->'payload'->>'status'='pending';

INSERT INTO public.uns_stream_checkpoint
    SELECT id,data,revision,created_at,updated_at FROM public.uns_meta
    WHERE kind='task' AND data->>'type'='programCheckpoint';
INSERT INTO public.uns_stream_commit_receipt
    SELECT id,data,revision,created_at,updated_at FROM public.uns_meta
    WHERE kind='task' AND data->>'type' IN ('ingestReceipt','programMetadataCommit','programCheckpointCommit');
INSERT INTO public.uns_metadata_event_outbox
    SELECT id,data,revision,created_at,updated_at FROM public.uns_meta
    WHERE kind='task' AND data->>'type'='metadataEventOutbox';

-- Retain cutover evidence privately, but never dual-read or dual-write it.
CREATE TABLE public.uns_stream_meta_task_archive AS
    SELECT * FROM public.uns_meta WHERE kind='task' AND data->>'type' IN
        ('programCheckpoint','ingestReceipt','programMetadataCommit','programCheckpointCommit','metadataEventOutbox');
DELETE FROM public.uns_meta WHERE kind='task' AND data->>'type' IN
    ('programCheckpoint','ingestReceipt','programMetadataCommit','programCheckpointCommit','metadataEventOutbox');

-- An old binary must not silently recreate a second authority after cutover.
-- This write fence is not a substitute for stopping old readers/workers.
CREATE FUNCTION public.reject_legacy_stream_meta_task() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $legacy_fence$
BEGIN
 IF NEW.kind='task' AND NEW.data->>'type' IN
     ('programCheckpoint','ingestReceipt','programMetadataCommit','programCheckpointCommit','metadataEventOutbox') THEN
  RAISE EXCEPTION 'private Stream runtime storage requires the updated Backend' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $legacy_fence$;
CREATE TRIGGER reject_legacy_stream_meta_task BEFORE INSERT OR UPDATE ON public.uns_meta
 FOR EACH ROW EXECUTE FUNCTION public.reject_legacy_stream_meta_task();
REVOKE ALL ON FUNCTION public.reject_legacy_stream_meta_task() FROM PUBLIC;

-- No fabricated completion dates for existing rows. The fields describe
-- transitions observed after this migration, not inferred historical events.
ALTER TABLE public.uns_stream_run
    ADD COLUMN updated_at bigint,
    ADD COLUMN finished_at bigint,
    ADD COLUMN state_revision bigint NOT NULL DEFAULT 1 CHECK (state_revision>0);
ALTER TABLE public.uns_stream_task
    ADD COLUMN updated_at bigint,
    ADD COLUMN finished_at bigint,
    ADD COLUMN payload_compacted_at bigint;
CREATE INDEX idx_stream_result_active ON public.uns_stream_task(created_at,id)
    WHERE status IN ('pending','partial-failure');
CREATE INDEX idx_stream_run_terminal ON public.uns_stream_run(finished_at,id)
    WHERE finished_at IS NOT NULL;

-- No Sample parent/FK or Timescale dependency. History may outlive hot state;
-- retention still requires explicit policy and recovery-dependency checks.
CREATE TABLE public.uns_stream_run_history (
    run_id varchar(64) NOT NULL,
    state_revision bigint NOT NULL CHECK (state_revision>0),
    stream_id bigint NOT NULL, version_id bigint NOT NULL,
    entry_id varchar(128) NOT NULL, model varchar(24) NOT NULL,
    status varchar(32) NOT NULL, duration_ms bigint NOT NULL,
    occurred_at bigint NOT NULL,
    PRIMARY KEY(run_id,state_revision)
);
CREATE INDEX idx_stream_history_stream_time ON public.uns_stream_run_history(stream_id,occurred_at DESC,run_id);

CREATE FUNCTION public.stamp_stream_runtime_state() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    NEW.updated_at := (extract(epoch FROM clock_timestamp())*1000)::bigint;
    IF TG_TABLE_NAME='uns_stream_run' AND TG_OP='UPDATE' THEN
        NEW.state_revision := OLD.state_revision + CASE WHEN OLD.status IS DISTINCT FROM NEW.status THEN 1 ELSE 0 END;
    END IF;
    IF TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status THEN
        NEW.finished_at := CASE WHEN NEW.status IN ('succeeded','failed','dropped','canceled','unknown','rejected')
            THEN NEW.updated_at ELSE NULL END;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER stream_run_lifecycle BEFORE INSERT OR UPDATE ON public.uns_stream_run
    FOR EACH ROW EXECUTE FUNCTION public.stamp_stream_runtime_state();
CREATE TRIGGER stream_task_lifecycle BEFORE INSERT OR UPDATE ON public.uns_stream_task
    FOR EACH ROW EXECUTE FUNCTION public.stamp_stream_runtime_state();

CREATE FUNCTION public.capture_stream_runtime_history() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
        INSERT INTO public.uns_stream_run_history
            (run_id,state_revision,stream_id,version_id,entry_id,model,status,duration_ms,occurred_at)
        VALUES(NEW.id,NEW.state_revision,NEW.stream_id,NEW.version_id,NEW.entry_id,NEW.model,
            NEW.status,NEW.duration_ms,NEW.updated_at);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER stream_runtime_history AFTER INSERT OR UPDATE ON public.uns_stream_run
    FOR EACH ROW EXECUTE FUNCTION public.capture_stream_runtime_history();
REVOKE ALL ON FUNCTION public.stamp_stream_runtime_state() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.capture_stream_runtime_history() FROM PUBLIC;
