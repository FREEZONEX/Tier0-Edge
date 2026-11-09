-- Control contracts belong to UNS metadata. Invocation reservations are
-- runtime receipts, not another configuration or capability registry.
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_kind_check;
ALTER TABLE public.uns_meta ADD CONSTRAINT uns_meta_kind_check CHECK
 (kind IN ('directory','topic','collection','source','stream','source_state','source_plan','source_version','app','subscription','task','index_state','control'));

CREATE INDEX IF NOT EXISTS idx_uns_control_owner ON public.uns_meta
 ((data->'binding'->>'streamId')) WHERE kind='control';

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
