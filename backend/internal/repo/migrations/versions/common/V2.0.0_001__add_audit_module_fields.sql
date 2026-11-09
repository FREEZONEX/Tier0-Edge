ALTER TABLE sys_user_oper_log
    ADD COLUMN IF NOT EXISTS module VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS module_key VARCHAR(128) NOT NULL DEFAULT '';

UPDATE sys_user_oper_log
SET module = LOWER(res_type),
    module_key = CASE
        WHEN res_type = 'Fleet' THEN COALESCE(
            NULLIF(SUBSTRING(detail FROM '"fleetNodeID"[[:space:]]*:[[:space:]]*"([^"]+)"'), ''),
            res_id
        )
        ELSE res_id
    END
WHERE module = '' OR module_key = '';

CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_module_key_time
    ON sys_user_oper_log(module, module_key, created_time DESC, id DESC);
