ALTER TABLE uns_cloud_sync_log
    ADD COLUMN IF NOT EXISTS result_code VARCHAR(64) NOT NULL DEFAULT '';

UPDATE uns_cloud_sync_log
SET result_code = CASE
    WHEN sync_type = 'user' AND status = 'success' THEN 'user.applied'
    WHEN sync_type = 'user' AND status = 'partial' THEN 'user.partial'
    WHEN sync_type = 'user' AND status = 'failed' THEN 'user.failed'
    WHEN sync_type = 'uns' AND status = 'success' THEN 'uns.applied'
    WHEN sync_type = 'uns' AND status = 'partial' THEN 'uns.partial'
    WHEN sync_type = 'uns' AND status = 'skipped' THEN 'uns.duplicate'
    WHEN sync_type = 'uns' AND status = 'failed' THEN 'uns.failed'
    ELSE result_code
END
WHERE result_code = '';

CREATE INDEX IF NOT EXISTS idx_uns_cloud_sync_log_result_time
    ON uns_cloud_sync_log(result_code, created_time DESC, id DESC);
