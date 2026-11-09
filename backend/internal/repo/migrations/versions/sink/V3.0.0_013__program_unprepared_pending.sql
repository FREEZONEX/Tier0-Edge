-- Recovery scans must not traverse completed execution history every tick.
CREATE INDEX IF NOT EXISTS idx_program_unprepared_runs
    ON public.uns_stream_run(id) WHERE model='program' AND status='processing';
