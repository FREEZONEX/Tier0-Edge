CREATE TABLE IF NOT EXISTS sys_ai_provider_config (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL DEFAULT 1,
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'openai_compatible',
    base_url VARCHAR(512) NOT NULL,
    model VARCHAR(128) NOT NULL DEFAULT '',
    api_key_ciphertext TEXT NOT NULL,
    key_suffix VARCHAR(16) NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_ai_provider_config_name
    ON sys_ai_provider_config(workspace_id, name, deleted_time);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_ai_provider_config_default
    ON sys_ai_provider_config(workspace_id) WHERE is_default = true AND enabled = true AND deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_sys_ai_provider_config_workspace
    ON sys_ai_provider_config(workspace_id, enabled, deleted_time);
