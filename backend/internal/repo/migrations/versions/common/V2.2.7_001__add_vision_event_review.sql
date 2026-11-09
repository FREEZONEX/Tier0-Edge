ALTER TABLE sys_vision_event
    ADD COLUMN IF NOT EXISTS review_status VARCHAR(24) NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS review_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reviewed_by BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reviewed_time TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_vision_event_review_created
    ON sys_vision_event(review_status, created_time DESC, id DESC)
    WHERE deleted_time = 0 AND event_type = 'alarm';
