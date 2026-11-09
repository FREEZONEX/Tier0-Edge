-- Scheduling metadata only; the immutable output remains in uns_stream_task.
ALTER TABLE public.uns_stream_run_owner
    ADD COLUMN IF NOT EXISTS recovery_state VARCHAR(16) NOT NULL DEFAULT 'waiting'
        CHECK (recovery_state IN ('waiting','pending','claimed','completed','quarantined')),
    ADD COLUMN IF NOT EXISTS recovery_attempts INTEGER NOT NULL DEFAULT 0
        CHECK (recovery_attempts BETWEEN 0 AND 8),
    ADD COLUMN IF NOT EXISTS recovery_after BIGINT NOT NULL DEFAULT 0 CHECK (recovery_after >= 0),
    ADD COLUMN IF NOT EXISTS recovery_token UUID
        CHECK ((recovery_state='claimed')=(recovery_token IS NOT NULL));

UPDATE public.uns_stream_run_owner binding SET
    recovery_state=CASE
        WHEN run.status='succeeded' AND task.status='succeeded' THEN 'completed'
        WHEN run.status IN ('processing','partial-failure') AND task.status IN ('pending','partial-failure') THEN 'pending'
        ELSE 'waiting' END,
    recovery_after=0
FROM public.uns_stream_run run, public.uns_stream_task task
WHERE run.id=binding.run_id AND task.id=run.id AND run.model='program'
    AND task.model='program' AND binding.recovery_state='waiting';

CREATE INDEX IF NOT EXISTS idx_program_output_recovery_due
    ON public.uns_stream_run_owner(recovery_after,run_id)
    WHERE recovery_state IN ('pending','claimed');
