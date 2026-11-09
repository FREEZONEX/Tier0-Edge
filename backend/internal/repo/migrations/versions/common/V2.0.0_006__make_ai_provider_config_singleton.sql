WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY workspace_id
               ORDER BY is_default DESC, enabled DESC, id DESC
           ) AS position
    FROM sys_ai_provider_config
    WHERE deleted_time = 0
)
UPDATE sys_ai_provider_config AS config
SET enabled = false,
    is_default = false,
    updated_time = CURRENT_TIMESTAMP,
    deleted_time = (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT + config.id
FROM ranked
WHERE config.id = ranked.id
  AND ranked.position > 1;

UPDATE sys_ai_provider_config
SET is_default = enabled,
    updated_time = CURRENT_TIMESTAMP
WHERE deleted_time = 0
  AND is_default IS DISTINCT FROM enabled;

CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_ai_provider_config_singleton
    ON sys_ai_provider_config(workspace_id) WHERE deleted_time = 0;
