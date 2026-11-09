-- Queue claims must not walk processed history or sort all matching tasks.
-- Keep kind literal in taskQuery so generic prepared plans can use this index.
CREATE INDEX IF NOT EXISTS idx_uns_meta_task_status_time ON public.uns_meta
    ((data->>'type'), (data->'payload'->>'status'), created_at, id)
    WHERE kind = 'task';
