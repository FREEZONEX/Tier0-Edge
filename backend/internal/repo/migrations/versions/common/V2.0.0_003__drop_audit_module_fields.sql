DROP INDEX IF EXISTS idx_sys_user_oper_log_module_key_time;

UPDATE sys_user_oper_log
SET res_id = module_key
WHERE LOWER(module) = 'fleet'
  AND module_key <> ''
  AND res_id <> module_key;

ALTER TABLE sys_user_oper_log
    DROP COLUMN IF EXISTS module,
    DROP COLUMN IF EXISTS module_key;

CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_resource_time
    ON sys_user_oper_log(res_type, res_id, created_time DESC, id DESC);
