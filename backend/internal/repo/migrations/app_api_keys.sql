-- 应用级 API Key 表
CREATE TABLE IF NOT EXISTS app_api_keys (
    id BIGSERIAL PRIMARY KEY,
    app_id VARCHAR(36) NOT NULL UNIQUE,
    name VARCHAR(64) NOT NULL DEFAULT 'default',
    api_key VARCHAR(255) NOT NULL,
    project_id BIGINT NOT NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_api_keys_app_id ON app_api_keys(app_id);
CREATE INDEX IF NOT EXISTS idx_app_api_keys_project_id ON app_api_keys(project_id);
