DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'timescaledb extension is not available, use plain PostgreSQL tables: %', SQLERRM;
END
$$;

DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pg_stat_statements extension is not available: %', SQLERRM;
END
$$;


-- Program execution ownership depends on the versioned uns_stream_run table.
-- V3.0.0_012 installs uns_stream_execution_owner and uns_stream_run_owner after
-- V2.2.7_005 on both fresh installs and upgrades; these are runtime-only tables.
-- V3.0.0_013 adds the partial pending-Run index for bounded retirement scans.
-- V3.0.0_014 adds bounded output-recovery scheduling to the Run association.
-- V3.0.0_015 installs private Checkpoint/commit receipt/metadata Outbox/history
-- tables, lifecycle fields and the forward Meta-task cutover on fresh installs
-- and upgrades. V3.0.0_016 protects those tables, not business Sample rows.
-- Runtime table DDL is versioned after its existing Run/Task dependencies.
-- Store.Migrate applies runtime ACLs after versioned migrations create the helper.

-- Atlas metadata: source + source_state share an ID; acquisition Streams live inside source, script Streams are independent.
CREATE TABLE IF NOT EXISTS public.uns_meta (
    id VARCHAR(256) NOT NULL,
    kind VARCHAR(32) NOT NULL CHECK (kind IN ('directory', 'topic', 'collection', 'source', 'stream', 'source_state', 'source_plan', 'source_version', 'app', 'subscription', 'task', 'index_state', 'control')),
    data JSONB NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    mount_id TEXT,
    source_kind VARCHAR(32) GENERATED ALWAYS AS (CASE WHEN kind='source_state' THEN 'source' END) STORED,
    PRIMARY KEY (kind,id),
    FOREIGN KEY (source_kind,id) REFERENCES public.uns_meta(kind,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_uns_meta_active_name
    ON public.uns_meta ((CASE WHEN kind = 'collection' THEN 'collection' ELSE 'namespace' END),
                     (COALESCE(data->>'parentId', '')), (lower(data->>'name')))
    WHERE deleted_at = 0 AND kind IN ('directory','collection');
CREATE INDEX IF NOT EXISTS idx_uns_meta_parent ON public.uns_meta ((data->>'parentId')) WHERE deleted_at = 0;
-- V3.0.0_006 adds the column on upgrades and installs the write guards.
ALTER TABLE public.uns_meta ADD COLUMN IF NOT EXISTS mount_id TEXT;
CREATE INDEX IF NOT EXISTS idx_uns_meta_mount ON public.uns_meta(mount_id,kind,id) WHERE mount_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_uns_meta_task_status_time ON public.uns_meta
    ((data->>'type'), (data->'payload'->>'status'), created_at, id)
    WHERE kind = 'task';
CREATE INDEX IF NOT EXISTS idx_uns_control_owner ON public.uns_meta
    ((data->'binding'->>'streamId')) WHERE kind='control';
-- Per manual operation reservation: stable references and a request digest only.
-- Never duplicate script code, business bodies or credentials here.
CREATE TABLE IF NOT EXISTS public.uns_control_invocation (
    identity CHAR(64) PRIMARY KEY,
    message_id CHAR(64) NOT NULL UNIQUE,
    control_code VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    stream_id BIGINT NOT NULL CHECK (stream_id>0),
    version_id BIGINT NOT NULL CHECK (version_id>0),
    entry_id VARCHAR(128) NOT NULL,
    contract_version VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS public.uns_record (
    collection_id VARCHAR(128) NOT NULL,
    collection_kind VARCHAR(32) NOT NULL DEFAULT 'collection' CHECK (collection_kind='collection'),
    source_kind VARCHAR(32) NOT NULL DEFAULT 'source' CHECK (source_kind='source'),
    source_id VARCHAR(128),
    id VARCHAR(128) NOT NULL,
    data JSONB NOT NULL CHECK (jsonb_typeof(data) = 'object' AND NOT (data ?| ARRAY['_id','_meta'])),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    source_time TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    lock_token_hash BYTEA,
    lock_principal VARCHAR(256),
    lock_label VARCHAR(64),
    lock_expires_at TIMESTAMPTZ,
    lock_fence BIGINT NOT NULL DEFAULT 0,
    lock_since TIMESTAMPTZ,
    PRIMARY KEY(collection_id,id),
    FOREIGN KEY (collection_kind,collection_id) REFERENCES public.uns_meta(kind,id),
    FOREIGN KEY (source_kind,source_id) REFERENCES public.uns_meta(kind,id)
);
ALTER TABLE public.uns_record ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
CREATE TABLE IF NOT EXISTS public.uns_sample (
    collection_id VARCHAR(128) NOT NULL,
    record_id VARCHAR(128) NOT NULL,
    id VARCHAR(128) NOT NULL,
    binding_id VARCHAR(128) NOT NULL DEFAULT '',
    time TIMESTAMPTZ(3) NOT NULL,
    received_at TIMESTAMPTZ(3) NOT NULL DEFAULT now(),
    quality VARCHAR(64) NOT NULL DEFAULT '',
    data JSONB NOT NULL CHECK (jsonb_typeof(data) = 'object' AND NOT (data ?| ARRAY['_id','_meta'])),
    PRIMARY KEY (collection_id, record_id, id),
    FOREIGN KEY (collection_id, record_id) REFERENCES public.uns_record(collection_id, id)
);
CREATE INDEX IF NOT EXISTS idx_uns_sample_history ON public.uns_sample (collection_id, record_id, time DESC, id DESC);

-- Stream 的可靠性辅助数据与目标数据使用同一个 UNS 事务。
-- 这不是控制配置库与 UNS 数据库之间的跨库事务。
CREATE TABLE IF NOT EXISTS public.stream_receipt (
    receipt_id text PRIMARY KEY,
    stream_id bigint NOT NULL,
    version_id bigint NOT NULL,
    topic_id bigint NOT NULL,
    completed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS public.stream_event_outbox (
    event_id text PRIMARY KEY REFERENCES public.stream_receipt(receipt_id),
    subject text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_stream_event_outbox_pending
    ON public.stream_event_outbox(created_at) WHERE sent_at IS NULL;

CREATE TABLE IF NOT EXISTS public."uns_timeserial" (
    "_id" int8 NOT NULL,
    "_timestamp" timestamptz(3) NOT NULL DEFAULT now(),
    "_quality" int8 DEFAULT 0,
    "double_1" float8 NULL,
    PRIMARY KEY ("_id", "_timestamp")
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'uns_timeserial'
          AND column_name = 'timeStamp'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'uns_timeserial'
          AND column_name = '_timestamp'
    ) THEN
        ALTER TABLE public."uns_timeserial" RENAME COLUMN "timeStamp" TO "_timestamp";
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'uns_timeserial'
          AND column_name = 'quality'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'uns_timeserial'
          AND column_name = '_quality'
    ) THEN
        ALTER TABLE public."uns_timeserial" RENAME COLUMN "quality" TO "_quality";
    END IF;
END
$$;

DO $$
DECLARE
    qualified_table text;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
        RETURN;
    END IF;

    qualified_table := format('%I.%I', 'public', 'uns_timeserial');

    EXECUTE format(
        'SELECT create_hypertable(%L, %L, partitioning_column => %L, number_partitions => 4, chunk_time_interval => %L::interval, if_not_exists => TRUE)',
        qualified_table,
        '_timestamp',
        '_id',
        '7 days'
    );

    EXECUTE format(
        'ALTER TABLE %s SET (timescaledb.compress, timescaledb.compress_segmentby = %L, timescaledb.compress_orderby = %L)',
        qualified_table,
        '_id',
        '"_timestamp" DESC'
    );

    IF NOT EXISTS (
        SELECT 1
        FROM timescaledb_information.jobs
        WHERE hypertable_schema = 'public'
          AND hypertable_name = 'uns_timeserial'
          AND proc_name = 'policy_retention'
    ) THEN
        PERFORM add_retention_policy(qualified_table, INTERVAL '10 years');
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM timescaledb_information.jobs
        WHERE hypertable_schema = 'public'
          AND hypertable_name = 'uns_timeserial'
          AND proc_name = 'policy_compression'
    ) THEN
        PERFORM add_compression_policy(
            qualified_table,
            compress_after => INTERVAL '7 days',
            schedule_interval => INTERVAL '1 day'
        );
    END IF;
END
$$;
-- Enterprise UNS edge synchronization selections and center mount points.
CREATE TABLE IF NOT EXISTS public.sync_mount (
    id text PRIMARY KEY,
    direction text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    fleet_node_id text NOT NULL,
    mount_point_id text NOT NULL DEFAULT '',
    selected_directory_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(selected_directory_ids) = 'array'),
    selected_collection_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(selected_collection_ids) = 'array'),
    scope_revision bigint NOT NULL DEFAULT 0 CHECK (scope_revision >= 0),
    enabled boolean NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_sync_mount_fleet_node ON public.sync_mount(fleet_node_id, direction);

CREATE INDEX IF NOT EXISTS idx_uns_record_source ON public.uns_record(source_id) WHERE source_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_uns_record_updated ON public.uns_record(collection_id,updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_uns_record_lease ON public.uns_record(collection_id,lock_principal,lock_expires_at) WHERE lock_expires_at IS NOT NULL;
-- Fleet payloads share the acquisition checkpoint / Collection transaction DB.
SELECT pg_advisory_xact_lock(814206108);

CREATE TABLE IF NOT EXISTS uns_storage_queue_budget (
 budget_id TEXT PRIMARY KEY, max_events BIGINT NOT NULL CHECK(max_events>0),
 max_deliveries BIGINT NOT NULL CHECK(max_deliveries>0), max_bytes BIGINT NOT NULL CHECK(max_bytes>0)
);
REVOKE ALL ON public.uns_storage_queue_budget FROM PUBLIC;
ALTER TABLE public.uns_storage_queue_budget ENABLE ROW LEVEL SECURITY;
CREATE TABLE IF NOT EXISTS public.uns_collection_storage_outbox (
 id bigserial PRIMARY KEY,event_id varchar(64) NOT NULL UNIQUE,event_type varchar(128) NOT NULL,
 aggregate_type varchar(64) NOT NULL DEFAULT '',aggregate_id varchar(128) NOT NULL DEFAULT '',payload jsonb NOT NULL,
 status varchar(32) NOT NULL DEFAULT 'pending',attempts integer NOT NULL DEFAULT 0,next_retry_time bigint NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT '',locked_by varchar(128) NOT NULL DEFAULT '',locked_until_time bigint NOT NULL DEFAULT 0,
 created_time timestamptz NOT NULL DEFAULT now(),updated_time timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS uns_collection_storage_pending ON public.uns_collection_storage_outbox(next_retry_time,id) WHERE status='pending';
CREATE TABLE IF NOT EXISTS public.uns_collection_storage_receipt (
 event_id text PRIMARY KEY,payload_hash text NOT NULL,committed_at bigint NOT NULL
);
REVOKE ALL ON public.uns_collection_storage_outbox,public.uns_collection_storage_receipt FROM PUBLIC;
ALTER TABLE public.uns_collection_storage_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.uns_collection_storage_receipt ENABLE ROW LEVEL SECURITY;

SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_global_storage_policy (
 id text PRIMARY KEY CHECK(id='edge'), mode text NOT NULL CHECK(mode IN ('local','center','both')),
 revision bigint NOT NULL, center_id text NOT NULL DEFAULT '', updated_at bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS public.uns_storage_collection_mapping (
 collection_id text PRIMARY KEY, center_id text NOT NULL, target_collection_id text NOT NULL,
 source_revision bigint NOT NULL DEFAULT 0, target_revision bigint NOT NULL DEFAULT 0,
 last_sync_at bigint NOT NULL DEFAULT 0, last_error text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS public.uns_storage_inbound_collection (
 node_id text NOT NULL, collection_id text NOT NULL, target_collection_id text NOT NULL UNIQUE,
 source_revision bigint NOT NULL, target_revision bigint NOT NULL, source_hash text NOT NULL,
 PRIMARY KEY(node_id,collection_id)
);
REVOKE ALL ON public.uns_global_storage_policy,public.uns_storage_collection_mapping,public.uns_storage_inbound_collection FROM PUBLIC;
ALTER TABLE public.uns_global_storage_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.uns_storage_collection_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.uns_storage_inbound_collection ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.uns_storage_collection_mapping ADD COLUMN IF NOT EXISTS last_attempt_at bigint NOT NULL DEFAULT 0;
SELECT pg_advisory_xact_lock(814206108);
ALTER TABLE public.uns_meta ADD COLUMN IF NOT EXISTS origin_node_id text NOT NULL DEFAULT '';
UPDATE public.uns_meta m SET origin_node_id=i.node_id FROM public.uns_storage_inbound_collection i
 WHERE m.kind='collection' AND m.id=i.target_collection_id AND m.origin_node_id='';
DROP INDEX IF EXISTS public.uk_uns_meta_active_name;
CREATE UNIQUE INDEX uk_uns_meta_active_name ON public.uns_meta
 ((CASE WHEN kind IN ('directory','topic') THEN 'namespace' ELSE kind END),
 (CASE WHEN kind='collection' THEN COALESCE(mount_id,NULLIF(origin_node_id,''),'') ELSE '' END),
 (CASE WHEN mount_id IS NOT NULL THEN id ELSE '' END),
 COALESCE(data->>'parentId',''),lower(data->>'name'))
 WHERE deleted_at=0 AND kind IN ('directory','topic','collection','source');

CREATE TABLE IF NOT EXISTS public.uns_storage_sync_scope (
 id varchar(16) PRIMARY KEY,
 mode varchar(16) NOT NULL DEFAULT 'all' CHECK (mode IN ('all','selected')),
 collection_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
 revision bigint NOT NULL DEFAULT 0
);
REVOKE ALL ON public.uns_storage_sync_scope FROM PUBLIC;
ALTER TABLE public.uns_storage_sync_scope ENABLE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION public.uns_collection_storage_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cid text;
BEGIN
 IF TG_OP='DELETE' THEN cid:=OLD.collection_id; ELSE cid:=NEW.collection_id; END IF;
 IF EXISTS(SELECT 1 FROM public.uns_global_storage_policy WHERE id='edge' AND mode<>'local')
 AND NOT EXISTS(SELECT 1 FROM public.uns_storage_sync_scope WHERE id='edge' AND mode='selected' AND NOT collection_ids @> jsonb_build_array(cid))
 AND NOT EXISTS(SELECT 1 FROM public.uns_storage_inbound_collection WHERE target_collection_id=cid)
 AND NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='collection' AND id=cid AND mount_id IS NOT NULL)
 AND COALESCE(current_setting('public.collection_storage_write',true),'')<>cid THEN
 RAISE EXCEPTION 'COLLECTION_STORAGE_UNSUPPORTED: writer must use global storage policy' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
DROP TRIGGER IF EXISTS uns_collection_storage_record ON public.uns_record;
CREATE TRIGGER uns_collection_storage_record BEFORE INSERT OR UPDATE OR DELETE ON public.uns_record FOR EACH ROW EXECUTE FUNCTION public.uns_collection_storage_guard();
DROP TRIGGER IF EXISTS uns_collection_storage_sample ON public.uns_sample;
CREATE TRIGGER uns_collection_storage_sample BEFORE INSERT OR UPDATE OR DELETE ON public.uns_sample FOR EACH ROW EXECUTE FUNCTION public.uns_collection_storage_guard();

-- First valid EDI content observations, independent of cloud delivery/processing.
CREATE TABLE IF NOT EXISTS public.source_edi_content_identity (
    source_id bigint NOT NULL,
    selection_id varchar(63) NOT NULL,
    identity_sha256 char(64) NOT NULL,
    content_sha256 char(64) NOT NULL,
    observed_at bigint NOT NULL,
    PRIMARY KEY (source_id, selection_id, identity_sha256),
    CHECK (identity_sha256 ~ '^[0-9a-f]{64}$'),
    CHECK (content_sha256 ~ '^[0-9a-f]{64}$')
);
REVOKE ALL ON public.source_edi_content_identity FROM PUBLIC;
ALTER TABLE public.source_edi_content_identity ENABLE ROW LEVEL SECURITY;
