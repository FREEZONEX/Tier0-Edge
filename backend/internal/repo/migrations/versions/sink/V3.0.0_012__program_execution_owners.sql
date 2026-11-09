-- Runtime coordination only: never copy scripts, credentials or business input.
CREATE TABLE IF NOT EXISTS public.uns_stream_execution_owner (
    id UUID PRIMARY KEY,
    lock_id INTEGER NOT NULL UNIQUE,
    state VARCHAR(16) NOT NULL CHECK (state IN ('active', 'lost')),
    created_at BIGINT NOT NULL,
    lost_at BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS public.uns_stream_run_owner (
    run_id VARCHAR(64) PRIMARY KEY REFERENCES public.uns_stream_run(id),
    owner_id UUID NOT NULL REFERENCES public.uns_stream_execution_owner(id),
    lineage JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(lineage) = 'array')
);
CREATE INDEX IF NOT EXISTS idx_program_run_owner
    ON public.uns_stream_run_owner(owner_id, run_id);
