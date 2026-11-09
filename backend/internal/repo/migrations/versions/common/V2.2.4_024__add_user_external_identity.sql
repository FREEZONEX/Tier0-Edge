CREATE TABLE IF NOT EXISTS sys_user_identity (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES sys_user_info(user_id),
    provider VARCHAR(32) NOT NULL,
    issuer VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    subject_id VARCHAR(128) NOT NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_user_identity_subject
    ON sys_user_identity(provider, issuer, tenant_id, subject_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_user_identity_user_provider
    ON sys_user_identity(user_id, provider, tenant_id);
