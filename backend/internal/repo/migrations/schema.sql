CREATE TABLE IF NOT EXISTS sys_user_info (
    user_id BIGSERIAL PRIMARY KEY,
    user_name VARCHAR(60) NOT NULL,
    nick_name VARCHAR(60) NOT NULL DEFAULT '',
    password VARCHAR(128) NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    github_pid VARCHAR(128) NOT NULL DEFAULT '',
    google_pid VARCHAR(128) NOT NULL DEFAULT '',
    last_ip VARCHAR(128) NOT NULL DEFAULT '',
    reg_ip VARCHAR(128) NOT NULL DEFAULT '',
    avatar VARCHAR(256) NOT NULL DEFAULT '',
    pf_role_code VARCHAR(60) NOT NULL DEFAULT 'member',
    status BIGINT NOT NULL DEFAULT 1,
    last_login BIGINT NOT NULL DEFAULT 0,
    first_login BIGINT NOT NULL DEFAULT 0,
    is_random_pwd BOOLEAN NOT NULL DEFAULT false,
    statistic_workspace_joined_count BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_user_info_tc_un ON sys_user_info(user_name, deleted_time);
ALTER TABLE sys_user_info ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS idx_sys_user_info_tc_email;

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

CREATE TABLE IF NOT EXISTS sys_user_group (
    id BIGSERIAL PRIMARY KEY,
    group_name VARCHAR(63) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_user_group_name ON sys_user_group(group_name, deleted_time);

CREATE TABLE IF NOT EXISTS sys_user_group_member (
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_sys_user_group_member_user ON sys_user_group_member(user_id, group_id);

CREATE TABLE IF NOT EXISTS sys_user_config (
    user_id BIGINT PRIMARY KEY,
    home_page VARCHAR(255) NOT NULL DEFAULT '/home',
    main_language VARCHAR(32) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sys_setting (
    setting_key VARCHAR(128) PRIMARY KEY,
    setting_value TEXT NOT NULL DEFAULT '',
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sys_role_info (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    code VARCHAR(100) NOT NULL,
    "desc" VARCHAR(200) NOT NULL DEFAULT '',
    type BIGINT NOT NULL DEFAULT 1,
    status BIGINT NOT NULL DEFAULT 1,
    default_home_page VARCHAR(255) NOT NULL DEFAULT '/home',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE sys_role_info ADD COLUMN IF NOT EXISTS default_home_page VARCHAR(255) NOT NULL DEFAULT '/home';
ALTER TABLE sys_role_info ADD COLUMN IF NOT EXISTS "desc" VARCHAR(200) NOT NULL DEFAULT '';
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'sys_role_info' AND column_name = 'description'
    ) THEN
        EXECUTE 'UPDATE sys_role_info SET "desc" = description WHERE "desc" = '''' AND description <> ''''';
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_role_info_name ON sys_role_info(name, deleted_time);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_role_info_code ON sys_role_info(code, deleted_time);

CREATE TABLE IF NOT EXISTS sys_workspace_user (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL DEFAULT 1,
    user_id BIGINT NOT NULL,
    role_code VARCHAR(100) NOT NULL DEFAULT '',
    role_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_workspace_user_uid ON sys_workspace_user(workspace_id, user_id, deleted_time);

CREATE TABLE IF NOT EXISTS sys_resource_info (
    id BIGSERIAL PRIMARY KEY,
    parent_id BIGINT NOT NULL DEFAULT 0,
    resource_key VARCHAR(100) NOT NULL,
    name VARCHAR(100) NOT NULL,
    type BIGINT NOT NULL DEFAULT 5,
    route_path VARCHAR(255) NOT NULL DEFAULT '',
    url_type BIGINT NOT NULL DEFAULT 1,
    open_type BIGINT NOT NULL DEFAULT 0,
    icon VARCHAR(100) NOT NULL DEFAULT '',
    sort BIGINT NOT NULL DEFAULT 0,
    enabled BIGINT NOT NULL DEFAULT 1,
    system_generated BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE sys_resource_info ADD COLUMN IF NOT EXISTS url_type BIGINT NOT NULL DEFAULT 1;
ALTER TABLE sys_resource_info ADD COLUMN IF NOT EXISTS open_type BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_resource_info_key ON sys_resource_info(resource_key, deleted_time);

CREATE TABLE IF NOT EXISTS sys_resource_action (
    id BIGSERIAL PRIMARY KEY,
    resource_id BIGINT NOT NULL,
    action_type VARCHAR(20) NOT NULL,
    action_value VARCHAR(200) NOT NULL,
    methods VARCHAR(50) NOT NULL DEFAULT '',
    enabled BIGINT NOT NULL DEFAULT 1,
    system_generated BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_resource_action_entry ON sys_resource_action(action_type, action_value, methods, deleted_time);

CREATE TABLE IF NOT EXISTS sys_role_resource (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL,
    resource_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_role_resource_pair ON sys_role_resource(role_id, resource_id, deleted_time);

CREATE TABLE IF NOT EXISTS sys_api_key (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    key_hash VARCHAR(128) NOT NULL,
    key_prefix VARCHAR(32) NOT NULL DEFAULT '',
    key_suffix VARCHAR(16) NOT NULL DEFAULT '',
    owner_id BIGINT NOT NULL DEFAULT 0,
    owner_type VARCHAR(32) NOT NULL DEFAULT 'personal',
    app_id VARCHAR(64) NOT NULL DEFAULT '',
    usage_type VARCHAR(32) NOT NULL DEFAULT 'external',
    permission VARCHAR(32) NOT NULL DEFAULT 'read_only',
    resource_keys TEXT NOT NULL DEFAULT '',
    status BIGINT NOT NULL DEFAULT 1,
    last_used_time BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE sys_api_key DROP COLUMN IF EXISTS key_type;
ALTER TABLE sys_api_key ALTER COLUMN permission SET DEFAULT 'read_only';
ALTER TABLE sys_api_key ALTER COLUMN owner_id TYPE BIGINT USING CASE WHEN owner_id::text ~ '^[0-9]+$' THEN owner_id::text::BIGINT ELSE 0 END;
ALTER TABLE sys_api_key ALTER COLUMN owner_id SET DEFAULT 0;
ALTER TABLE sys_api_key ADD COLUMN IF NOT EXISTS app_id VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sys_api_key_app_id ON sys_api_key (app_id);
DO $$
DECLARE
    max_api_key_id BIGINT;
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sys_api_key'
          AND column_name = 'id'
          AND column_default IS NULL
    ) THEN
        CREATE SEQUENCE IF NOT EXISTS sys_api_key_id_seq OWNED BY sys_api_key.id;
        SELECT COALESCE(MAX(id), 0) INTO max_api_key_id FROM sys_api_key;
        PERFORM setval('sys_api_key_id_seq', GREATEST(max_api_key_id, 1), max_api_key_id > 0);
        ALTER TABLE sys_api_key ALTER COLUMN id SET DEFAULT nextval('sys_api_key_id_seq');
    END IF;
END $$;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND indexname = 'idx_sys_api_key_hash'
          AND indexdef LIKE '%key_hash, deleted_time%'
    ) THEN
        DROP INDEX idx_sys_api_key_hash;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_api_key_hash ON sys_api_key(key_hash);
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND indexname = 'idx_sys_api_key_name_owner'
          AND indexdef NOT LIKE '%deleted_time%'
    ) THEN
        DROP INDEX idx_sys_api_key_name_owner;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_api_key_name_owner ON sys_api_key(name, owner_id, owner_type, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_api_key_owner_id ON sys_api_key(owner_id);
CREATE INDEX IF NOT EXISTS idx_sys_api_key_status ON sys_api_key(status);
CREATE INDEX IF NOT EXISTS idx_sys_api_key_created_time ON sys_api_key(created_time DESC);

CREATE TABLE IF NOT EXISTS sys_ai_provider_config (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL DEFAULT 1,
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'openai_compatible',
    base_url VARCHAR(512) NOT NULL,
    model VARCHAR(128) NOT NULL DEFAULT '',
    api_key_ciphertext TEXT NOT NULL,
    key_suffix VARCHAR(16) NOT NULL DEFAULT '',
    embedding_mode VARCHAR(16) NOT NULL DEFAULT 'inherit',
    embedding_base_url VARCHAR(512) NOT NULL DEFAULT '',
    embedding_api_key_ciphertext TEXT NOT NULL DEFAULT '',
    embedding_key_suffix VARCHAR(16) NOT NULL DEFAULT '',
    embedding_model VARCHAR(128) NOT NULL DEFAULT '',
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_ai_provider_config_singleton
    ON sys_ai_provider_config(workspace_id) WHERE deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_sys_ai_provider_config_workspace
    ON sys_ai_provider_config(workspace_id, enabled, deleted_time);

CREATE TABLE IF NOT EXISTS sys_gateway_route (
    id BIGSERIAL PRIMARY KEY,
    route_key VARCHAR(96) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    target_type VARCHAR(32) NOT NULL,
    match_type VARCHAR(32) NOT NULL,
    path_pattern VARCHAR(256) NOT NULL,
    methods JSONB NOT NULL DEFAULT '[]',
    target_url VARCHAR(512) NOT NULL DEFAULT '',
    target_path VARCHAR(256) NOT NULL DEFAULT '',
    strip_prefix BOOLEAN NOT NULL DEFAULT false,
    rewrite_path VARCHAR(256) NOT NULL DEFAULT '',
    auth_policy VARCHAR(32) NOT NULL,
    resource_key VARCHAR(128) NOT NULL DEFAULT '',
    timeout_ms INTEGER NOT NULL DEFAULT 10000,
    priority INTEGER NOT NULL DEFAULT 100,
    enabled BOOLEAN NOT NULL DEFAULT true,
    system_builtin BOOLEAN NOT NULL DEFAULT false,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_gateway_route_key ON sys_gateway_route(route_key, deleted_time);

CREATE TABLE IF NOT EXISTS uns_data_quality_issue (
    id BIGSERIAL PRIMARY KEY,
    issue_key VARCHAR(192) NOT NULL,
    uns_id BIGINT NOT NULL,
    rule_code VARCHAR(64) NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'warning',
    state VARCHAR(16) NOT NULL DEFAULT 'open',
    first_observed_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL,
    occurrences BIGINT NOT NULL DEFAULT 1,
    summary VARCHAR(512) NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    acknowledged_by BIGINT NOT NULL DEFAULT 0,
    acknowledged_at TIMESTAMPTZ NULL,
    resolved_at TIMESTAMPTZ NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_data_quality_issue_active
    ON uns_data_quality_issue(uns_id, rule_code)
    WHERE state IN ('open', 'ack');
CREATE INDEX IF NOT EXISTS idx_uns_data_quality_issue_list
    ON uns_data_quality_issue(state, severity, last_observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_uns_data_quality_issue_recovery
    ON uns_data_quality_issue(uns_id, rule_code, resolved_at DESC, id DESC)
    WHERE state = 'resolved';

CREATE TABLE IF NOT EXISTS sys_oauth_client (
    id BIGSERIAL PRIMARY KEY,
    client_id VARCHAR(128) NOT NULL,
    client_secret_hash VARCHAR(256) NOT NULL DEFAULT '',
    client_name VARCHAR(200) NOT NULL,
    client_type VARCHAR(32) NOT NULL DEFAULT 'public',
    app_id BIGINT NOT NULL DEFAULT 0,
    app_key VARCHAR(128) NOT NULL DEFAULT '',
    redirect_uris JSONB NOT NULL DEFAULT '[]',
    allowed_origins JSONB NOT NULL DEFAULT '[]',
    scopes JSONB NOT NULL DEFAULT '[]',
    trusted BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    system_builtin BOOLEAN NOT NULL DEFAULT false,
    access_token_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    refresh_token_enabled BOOLEAN NOT NULL DEFAULT false,
    refresh_token_ttl_seconds INTEGER NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE sys_oauth_client DROP CONSTRAINT IF EXISTS uni_sys_oauth_client_client_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_oauth_client_client_id ON sys_oauth_client(client_id);
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS client_secret_hash VARCHAR(256) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS client_name VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS client_type VARCHAR(32) NOT NULL DEFAULT 'public';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS app_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS app_key VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS redirect_uris JSONB NOT NULL DEFAULT '[]';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS allowed_origins JSONB NOT NULL DEFAULT '[]';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS scopes JSONB NOT NULL DEFAULT '[]';
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS trusted BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS system_builtin BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS access_token_ttl_seconds INTEGER NOT NULL DEFAULT 3600;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS refresh_token_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS refresh_token_ttl_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS updated_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS created_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_client ADD COLUMN IF NOT EXISTS updated_time BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS sys_oauth_token (
    id BIGSERIAL PRIMARY KEY,
    token_hash VARCHAR(256) NOT NULL,
    token_type VARCHAR(32) NOT NULL,
    client_id VARCHAR(128) NOT NULL,
    user_id BIGINT NOT NULL,
    redirect_uri VARCHAR(512) NOT NULL DEFAULT '',
    scope VARCHAR(512) NOT NULL DEFAULT '',
    code_challenge VARCHAR(256) NOT NULL DEFAULT '',
    code_challenge_method VARCHAR(16) NOT NULL DEFAULT '',
    token_family VARCHAR(64) NOT NULL DEFAULT '',
    expires_time BIGINT NOT NULL DEFAULT 0,
    used_time BIGINT NOT NULL DEFAULT 0,
    revoked_time BIGINT NOT NULL DEFAULT 0,
    rotated_time BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE sys_oauth_token DROP CONSTRAINT IF EXISTS uni_sys_oauth_token_token_hash;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_oauth_token_hash ON sys_oauth_token(token_hash);
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS redirect_uri VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS scope VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS code_challenge VARCHAR(256) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS code_challenge_method VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS token_family VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS expires_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS used_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS revoked_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS rotated_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_oauth_token ADD COLUMN IF NOT EXISTS created_time BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS sys_outbox_event (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL DEFAULT '',
    aggregate_id VARCHAR(128) NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_time BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    locked_by VARCHAR(128) NOT NULL DEFAULT '',
    locked_until_time BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_outbox_event_event_id ON sys_outbox_event(event_id);
CREATE INDEX IF NOT EXISTS idx_sys_outbox_event_pending_type_time ON sys_outbox_event(event_type, created_time, id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_sys_outbox_event_done_type_time ON sys_outbox_event(event_type, updated_time, id) WHERE status = 'done';

CREATE TABLE IF NOT EXISTS sys_async_job (
    id BIGSERIAL PRIMARY KEY,
    job_key VARCHAR(64) NOT NULL,
    job_type VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    progress INTEGER NOT NULL DEFAULT 0,
    request_json JSONB NOT NULL DEFAULT '{}',
    result_json JSONB NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    started_time BIGINT NOT NULL DEFAULT 0,
    finished_time BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_async_job_job_key ON sys_async_job(job_key);

CREATE TABLE IF NOT EXISTS sys_user_oper_log (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    res_type VARCHAR(50) NOT NULL DEFAULT '',
    res_id VARCHAR(50) NOT NULL DEFAULT '',
    res_name VARCHAR(256) NOT NULL DEFAULT '',
    business_type BIGINT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    code BIGINT NOT NULL DEFAULT 200,
    is_show_in_recent BIGINT NOT NULL DEFAULT 1,
    operator_name VARCHAR(100) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE sys_user_oper_log ALTER COLUMN detail TYPE TEXT;
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS scope_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS scope_name VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS request_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS ip VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS result_msg TEXT NOT NULL DEFAULT '';
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS resource_path TEXT NOT NULL DEFAULT '';
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS result VARCHAR(20) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_result_time ON sys_user_oper_log(result, created_time DESC, id DESC);
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS event_id VARCHAR(64) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_event ON sys_user_oper_log(event_id) WHERE event_id <> '';
CREATE INDEX IF NOT EXISTS idx_audit_scope_time ON sys_user_oper_log(scope_type, scope_id, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_module_time ON sys_user_oper_log(res_type, business_type, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_code_time ON sys_user_oper_log(code, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_operator_time ON sys_user_oper_log(user_id, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_request ON sys_user_oper_log(request_id) WHERE request_id <> '';

CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_created_time ON sys_user_oper_log(created_time DESC);
CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_user_recent ON sys_user_oper_log(user_id, is_show_in_recent, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_resource_time ON sys_user_oper_log(res_type, res_id, created_time DESC, id DESC);

CREATE TABLE IF NOT EXISTS sys_user_login_log (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL DEFAULT 0,
    user_name VARCHAR(100) NOT NULL DEFAULT '',
    ip_addr VARCHAR(64) NOT NULL DEFAULT '',
    login_location VARCHAR(100) NOT NULL DEFAULT '',
    browser VARCHAR(50) NOT NULL DEFAULT '',
    os VARCHAR(50) NOT NULL DEFAULT '',
    event_type VARCHAR(20) NOT NULL DEFAULT 'login',
    code BIGINT NOT NULL DEFAULT 200,
    msg VARCHAR(255) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sys_user_login_log_created_time ON sys_user_login_log(created_time DESC);
CREATE INDEX IF NOT EXISTS idx_sys_user_login_log_user_time ON sys_user_login_log(user_name, created_time DESC);

CREATE TABLE IF NOT EXISTS uns_namespace_node_info (
    id BIGINT PRIMARY KEY,
    id_path VARCHAR(1024) NOT NULL DEFAULT '',
    parent_id BIGINT NOT NULL DEFAULT 0,
    name VARCHAR(128) NOT NULL,
    display_name VARCHAR(256) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    namespace VARCHAR(512) NOT NULL,
    alias VARCHAR(128) NOT NULL DEFAULT '',
    type SMALLINT NOT NULL DEFAULT 1,
    topic_type SMALLINT NOT NULL DEFAULT 0,
    sort_key BIGINT NOT NULL DEFAULT 0,
    schema JSONB NOT NULL DEFAULT '[]'::jsonb,
    extendproperties JSONB NOT NULL DEFAULT '{}',
    data_quality_policy JSONB DEFAULT NULL,
    enable_history SMALLINT NOT NULL DEFAULT 2,
    mock_data SMALLINT NOT NULL DEFAULT 2,
    template_id BIGINT DEFAULT NULL,
    is_favorite SMALLINT NOT NULL DEFAULT 2,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    recycle_is_del SMALLINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    deleted_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS recycle_is_del SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS id_path VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS sort_key BIGINT NOT NULL DEFAULT 0;
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS extendproperties JSONB NOT NULL DEFAULT '{}';
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS data_quality_policy JSONB DEFAULT NULL;
CREATE INDEX IF NOT EXISTS idx_uns_namespace_node_data_quality_policy
    ON uns_namespace_node_info USING GIN (data_quality_policy jsonb_path_ops)
    WHERE data_quality_policy IS NOT NULL;
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS enable_history SMALLINT NOT NULL DEFAULT 2;
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS mock_data SMALLINT NOT NULL DEFAULT 2;
ALTER TABLE uns_namespace_node_info ADD COLUMN IF NOT EXISTS is_favorite SMALLINT NOT NULL DEFAULT 2;
ALTER TABLE uns_namespace_node_info ALTER COLUMN schema SET DEFAULT '[]'::jsonb;
UPDATE uns_namespace_node_info SET schema = '[]'::jsonb WHERE schema IS NULL OR jsonb_typeof(schema) = 'null';
UPDATE uns_namespace_node_info SET schema = COALESCE(schema->'fields', '[]'::jsonb) WHERE jsonb_typeof(schema) = 'object';
ALTER TABLE uns_namespace_node_info ALTER COLUMN schema SET NOT NULL;
ALTER TABLE uns_namespace_node_info ALTER COLUMN template_id DROP DEFAULT;
ALTER TABLE uns_namespace_node_info ALTER COLUMN template_id DROP NOT NULL;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'uns_namespace_node_info' AND column_name = 'extend_properties'
    ) THEN
        EXECUTE 'UPDATE uns_namespace_node_info SET extendproperties = extend_properties WHERE extendproperties = ''{}''::jsonb';
    END IF;
END $$;
UPDATE uns_namespace_node_info SET template_id = NULL WHERE template_id = 0;
WITH RECURSIVE tree AS (
    SELECT id, parent_id, id::text AS path FROM uns_namespace_node_info WHERE parent_id = 0
    UNION ALL
    SELECT child.id, child.parent_id, tree.path || '/' || child.id::text
    FROM uns_namespace_node_info child
    JOIN tree ON child.parent_id = tree.id
)
UPDATE uns_namespace_node_info node SET id_path = tree.path FROM tree WHERE node.id = tree.id AND (node.id_path = '' OR node.id_path IS NULL);
UPDATE uns_namespace_node_info SET id_path = id::text WHERE id_path = '' OR id_path IS NULL;
ALTER TABLE uns_namespace_node_info DROP COLUMN IF EXISTS extend_properties;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'uns_namespace_node_info'
          AND indexname = 'idx_uns_node_namespace'
          AND indexdef NOT ILIKE '%WHERE%deleted_time = 0%'
    ) THEN
        DROP INDEX idx_uns_node_namespace;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'uns_namespace_node_info'
          AND indexname = 'idx_uns_node_alias'
          AND (indexdef NOT ILIKE '%WHERE%deleted_time = 0%' OR indexdef NOT ILIKE '%alias%<>%')
    ) THEN
        DROP INDEX idx_uns_node_alias;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'uns_namespace_node_info'
          AND indexname = 'idx_uns_node_parent_name'
          AND indexdef NOT ILIKE '%WHERE%deleted_time = 0%'
    ) THEN
        DROP INDEX idx_uns_node_parent_name;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_node_namespace ON uns_namespace_node_info(namespace) WHERE deleted_time = 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_node_alias ON uns_namespace_node_info(alias) WHERE deleted_time = 0 AND alias <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_node_parent_name ON uns_namespace_node_info(parent_id, name) WHERE deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_uns_node_id_path ON uns_namespace_node_info(id_path);

CREATE TABLE IF NOT EXISTS uns_namespace_node_version (
    id BIGSERIAL PRIMARY KEY,
    version BIGINT NOT NULL DEFAULT 0,
    etag VARCHAR(64) NOT NULL DEFAULT '',
    checksum VARCHAR(64) NOT NULL DEFAULT '',
    unstree JSONB NOT NULL DEFAULT 'null'::jsonb,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE uns_namespace_node_version ADD COLUMN IF NOT EXISTS etag VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE uns_namespace_node_version ADD COLUMN IF NOT EXISTS checksum VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE uns_namespace_node_version ADD COLUMN IF NOT EXISTS unstree JSONB NOT NULL DEFAULT 'null'::jsonb;
ALTER TABLE uns_namespace_node_version ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE uns_namespace_node_version ADD COLUMN IF NOT EXISTS updated_by BIGINT NOT NULL DEFAULT 0;
DO $$
DECLARE
    max_uns_namespace_node_version_id BIGINT;
BEGIN
    CREATE SEQUENCE IF NOT EXISTS uns_namespace_node_version_id_seq OWNED BY uns_namespace_node_version.id;
    SELECT COALESCE(MAX(id), 0) INTO max_uns_namespace_node_version_id FROM uns_namespace_node_version;
    PERFORM setval(
        'uns_namespace_node_version_id_seq',
        GREATEST(max_uns_namespace_node_version_id, 1),
        max_uns_namespace_node_version_id > 0
    );
    ALTER TABLE uns_namespace_node_version ALTER COLUMN id SET DEFAULT nextval('uns_namespace_node_version_id_seq');
END $$;
CREATE INDEX IF NOT EXISTS idx_uns_namespace_node_version_version ON uns_namespace_node_version(version);
CREATE INDEX IF NOT EXISTS idx_uns_namespace_node_version_etag ON uns_namespace_node_version(etag);

CREATE TABLE IF NOT EXISTS uns_namespace_template_info (
    id BIGINT PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    topic_type SMALLINT NOT NULL,
    schema JSONB NOT NULL DEFAULT '[]'::jsonb,
    extend_properties JSONB NOT NULL DEFAULT '{}',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    deleted_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE uns_namespace_template_info ALTER COLUMN schema SET DEFAULT '[]'::jsonb;
UPDATE uns_namespace_template_info SET schema = '[]'::jsonb WHERE schema IS NULL OR jsonb_typeof(schema) = 'null';
UPDATE uns_namespace_template_info SET schema = COALESCE(schema->'fields', '[]'::jsonb) WHERE jsonb_typeof(schema) = 'object';
ALTER TABLE uns_namespace_template_info ALTER COLUMN schema SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_template_name ON uns_namespace_template_info(name) WHERE deleted_time = 0;

CREATE TABLE IF NOT EXISTS uns_namespace_label_info (
    id BIGINT PRIMARY KEY,
    label_key VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    color VARCHAR(32) NOT NULL DEFAULT '',
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_label_key ON uns_namespace_label_info(label_key) WHERE deleted_time = 0;

CREATE TABLE IF NOT EXISTS uns_namespace_label_nodeid (
    label_id BIGINT NOT NULL,
    node_id BIGINT NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(label_id, node_id)
);

CREATE TABLE IF NOT EXISTS uns_nodered_flow (
    id BIGINT PRIMARY KEY,
    flow_id VARCHAR(64) NOT NULL DEFAULT '',
    connect_client_id BIGINT NOT NULL DEFAULT 0,
    flow_name VARCHAR(512) NOT NULL,
    current_version_id BIGINT NOT NULL DEFAULT 0,
    flow_data TEXT NOT NULL DEFAULT '',
    flow_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    template VARCHAR(64) NOT NULL DEFAULT '',
    description VARCHAR(512) NOT NULL DEFAULT '',
    flow_type INTEGER NOT NULL DEFAULT 1,
    node_type INTEGER NOT NULL DEFAULT 1,
    parent_id BIGINT NOT NULL DEFAULT 0,
    sort_key BIGINT NOT NULL DEFAULT 0,
    is_favorite SMALLINT NOT NULL DEFAULT 2,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
DROP INDEX IF EXISTS idx_uns_flow_name;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'uns_nodered_flow'
          AND indexname = 'idx_uns_flow_parent_name_del'
          AND (
              indexdef NOT ILIKE '%WHERE%deleted_time = 0%'
              OR indexdef NOT ILIKE '%flow_type%'
          )
    ) THEN
        DROP INDEX idx_uns_flow_parent_name_del;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_flow_parent_name_del ON uns_nodered_flow(flow_type, parent_id, flow_name) WHERE deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_uns_nodered_flow_flow_id ON uns_nodered_flow(flow_id);
CREATE INDEX IF NOT EXISTS idx_uns_nodered_flow_connect_client_id ON uns_nodered_flow(connect_client_id);
CREATE INDEX IF NOT EXISTS idx_uns_flow_parent ON uns_nodered_flow(parent_id);
CREATE INDEX IF NOT EXISTS idx_uns_flow_sort ON uns_nodered_flow(sort_key);

CREATE TABLE IF NOT EXISTS uns_nodered_flow_node (
    parent_id BIGINT NOT NULL,
    node_id BIGINT NOT NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(parent_id, node_id)
);
CREATE INDEX IF NOT EXISTS idx_uns_flow_node_parent ON uns_nodered_flow_node(parent_id);
CREATE INDEX IF NOT EXISTS idx_uns_flow_node_node_id ON uns_nodered_flow_node(node_id);

CREATE TABLE IF NOT EXISTS uns_nodered_flow_version (
    id BIGINT PRIMARY KEY,
    flow_id BIGINT NOT NULL,
    version_name VARCHAR(63) NOT NULL,
    version_type INTEGER NOT NULL DEFAULT 1,
    description VARCHAR(512) NOT NULL DEFAULT '',
    snapshot_data TEXT NOT NULL DEFAULT '',
    checksum VARCHAR(64) NOT NULL DEFAULT '',
    source_rev VARCHAR(64) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_flow_version_flow_name ON uns_nodered_flow_version(flow_id, version_name);
CREATE INDEX IF NOT EXISTS idx_uns_flow_version_flow_created ON uns_nodered_flow_version(flow_id, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_uns_flow_version_type ON uns_nodered_flow_version(version_type);

CREATE TABLE IF NOT EXISTS uns_connect_client (
    id BIGINT PRIMARY KEY,
    name VARCHAR(128),
    connect_status INTEGER NOT NULL DEFAULT 2,
    auth_status INTEGER NOT NULL DEFAULT 1,
    description TEXT NOT NULL DEFAULT '',
    password VARCHAR(255) NOT NULL DEFAULT '',
    client_id VARCHAR(64) NOT NULL,
    client_id_random_suffix_enabled BOOLEAN NOT NULL DEFAULT false,
    username VARCHAR(64) NOT NULL,
    type INTEGER NOT NULL DEFAULT 1,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    connected_time BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    deleted_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE uns_connect_client ADD COLUMN IF NOT EXISTS name VARCHAR(128);
ALTER TABLE uns_connect_client ADD COLUMN IF NOT EXISTS client_id_random_suffix_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS uns_dashboard (
    id VARCHAR(128) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type SMALLINT NOT NULL DEFAULT 1,
    group_id BIGINT NOT NULL DEFAULT 0,
    node_type INTEGER NOT NULL DEFAULT 2,
    parent_id VARCHAR(128) NOT NULL DEFAULT '',
    need_init BOOLEAN NOT NULL DEFAULT false,
    description TEXT NOT NULL DEFAULT '',
    creator VARCHAR(128) NOT NULL DEFAULT '',
    create_time BIGINT NOT NULL DEFAULT 0,
    update_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE uns_dashboard ADD COLUMN IF NOT EXISTS node_type INTEGER NOT NULL DEFAULT 2;
ALTER TABLE uns_dashboard ADD COLUMN IF NOT EXISTS parent_id VARCHAR(128) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_uns_dashboard_parent ON uns_dashboard(node_type, parent_id, create_time);

CREATE TABLE IF NOT EXISTS uns_dashboard_ref (
    dashboard_id VARCHAR(128) NOT NULL,
    uns_alias VARCHAR(128) NOT NULL,
    create_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(dashboard_id, uns_alias)
);

CREATE TABLE IF NOT EXISTS uns_dashboard_top_recodes (
    id VARCHAR(128) NOT NULL,
    user_id VARCHAR(128) NOT NULL,
    mark SMALLINT NOT NULL DEFAULT 0,
    mark_time BIGINT NOT NULL DEFAULT 0,
    update_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_dashboard_top_record ON uns_dashboard_top_recodes(id, user_id);

CREATE TABLE IF NOT EXISTS uns_folder (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    parent_id BIGINT NOT NULL DEFAULT 0,
    path VARCHAR(1024) NOT NULL DEFAULT '',
    owner VARCHAR(128) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND indexname = 'idx_uns_folder_name_parent'
          AND indexdef NOT LIKE '%deleted_time%'
    ) THEN
        DROP INDEX idx_uns_folder_name_parent;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_folder_name_parent ON uns_folder(name, parent_id, deleted_time);
CREATE INDEX IF NOT EXISTS idx_uns_folder_parent ON uns_folder(parent_id);
CREATE INDEX IF NOT EXISTS idx_uns_folder_owner ON uns_folder(owner);

CREATE TABLE IF NOT EXISTS uns_notebook (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    folder_id BIGINT NOT NULL DEFAULT 0,
    file_path VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'idle',
    owner VARCHAR(128) NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    marimo_url VARCHAR(512) NOT NULL DEFAULT '',
    last_run_at BIGINT NOT NULL DEFAULT 0,
    current_version_id BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
DROP INDEX IF EXISTS idx_uns_notebook_name;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND indexname = 'idx_uns_notebook_name_folder'
          AND indexdef NOT LIKE '%deleted_time%'
    ) THEN
        DROP INDEX idx_uns_notebook_name_folder;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_notebook_name_folder ON uns_notebook(name, folder_id, deleted_time);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_status ON uns_notebook(status);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_owner ON uns_notebook(owner);

CREATE TABLE IF NOT EXISTS uns_notebook_snapshot (
    id BIGSERIAL PRIMARY KEY,
    notebook_id BIGINT NOT NULL,
    version_name VARCHAR(63) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    snapshot_type VARCHAR(16) NOT NULL DEFAULT 'manual',
    content TEXT NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    creator VARCHAR(128) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_snapshot_notebook ON uns_notebook_snapshot(notebook_id);

CREATE TABLE IF NOT EXISTS uns_notebook_tag (
    id BIGSERIAL PRIMARY KEY,
    notebook_id BIGINT NOT NULL,
    tag VARCHAR(64) NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_notebook_tag_entry ON uns_notebook_tag(notebook_id, tag);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_tag_notebook ON uns_notebook_tag(notebook_id);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_tag_tag ON uns_notebook_tag(tag);

CREATE TABLE IF NOT EXISTS uns_notebook_session (
    id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL,
    notebook_id BIGINT NOT NULL,
    session_type VARCHAR(32) NOT NULL DEFAULT 'server',
    compute_profile VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'starting',
    auto_shutdown_minutes INTEGER NOT NULL DEFAULT 0,
    started_at BIGINT NOT NULL DEFAULT 0,
    stopped_at BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uns_notebook_session_sid ON uns_notebook_session(session_id);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_session_notebook ON uns_notebook_session(notebook_id);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_session_type ON uns_notebook_session(session_type);
CREATE INDEX IF NOT EXISTS idx_uns_notebook_session_status ON uns_notebook_session(status);

DO $$
DECLARE
    tbl text;
BEGIN
    FOREACH tbl IN ARRAY ARRAY[
        'sys_user_info',
        'sys_user_config',
        'user_config',
        'sys_role_info',
        'sys_workspace_user',
        'sys_resource_info',
        'sys_resource_action',
        'sys_role_resource',
        'sys_api_key',
        'sys_gateway_route',
        'sys_oauth_client',
        'sys_oauth_token',
        'sys_outbox_event',
        'sys_async_job',
        'uns_namespace_node_info',
        'uns_namespace_node_version',
        'uns_namespace_template_info',
        'uns_namespace_label_info',
        'uns_namespace_label_nodeid',
        'uns_nodered_flow',
        'uns_nodered_flow_node',
        'uns_nodered_flow_version',
        'uns_connect_client',
        'uns_folder',
        'uns_notebook',
        'uns_notebook_snapshot',
        'uns_notebook_tag',
        'uns_notebook_session'
    ]
    LOOP
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = tbl
              AND column_name = 'created_time'
              AND data_type = 'bigint'
        ) THEN
            EXECUTE format('ALTER TABLE %I ALTER COLUMN created_time DROP DEFAULT', tbl);
            EXECUTE format(
                'ALTER TABLE %I ALTER COLUMN created_time TYPE TIMESTAMPTZ USING CASE WHEN created_time > 0 THEN to_timestamp(created_time / 1000.0) ELSE CURRENT_TIMESTAMP END',
                tbl
            );
            EXECUTE format('ALTER TABLE %I ALTER COLUMN created_time SET DEFAULT CURRENT_TIMESTAMP', tbl);
            EXECUTE format('ALTER TABLE %I ALTER COLUMN created_time SET NOT NULL', tbl);
        END IF;

        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = tbl
              AND column_name = 'updated_time'
              AND data_type = 'bigint'
        ) THEN
            EXECUTE format('ALTER TABLE %I ALTER COLUMN updated_time DROP DEFAULT', tbl);
            EXECUTE format(
                'ALTER TABLE %I ALTER COLUMN updated_time TYPE TIMESTAMPTZ USING CASE WHEN updated_time > 0 THEN to_timestamp(updated_time / 1000.0) ELSE CURRENT_TIMESTAMP END',
                tbl
            );
            EXECUTE format('ALTER TABLE %I ALTER COLUMN updated_time SET DEFAULT CURRENT_TIMESTAMP', tbl);
            EXECUTE format('ALTER TABLE %I ALTER COLUMN updated_time SET NOT NULL', tbl);
        END IF;
    END LOOP;
END $$;

-- 主键统一为复合主键 (parent_id, node_id)：同一节点可同时绑定 source 与 event flow 各一条
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
          ON tc.constraint_name = kcu.constraint_name
         AND tc.table_schema = kcu.table_schema
         AND tc.table_name = kcu.table_name
        WHERE tc.table_schema = current_schema()
          AND tc.table_name = 'uns_nodered_flow_node'
          AND tc.constraint_type = 'PRIMARY KEY'
          AND kcu.column_name = 'parent_id'
    ) OR NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
          ON tc.constraint_name = kcu.constraint_name
         AND tc.table_schema = kcu.table_schema
         AND tc.table_name = kcu.table_name
        WHERE tc.table_schema = current_schema()
          AND tc.table_name = 'uns_nodered_flow_node'
          AND tc.constraint_type = 'PRIMARY KEY'
          AND kcu.column_name = 'node_id'
    ) THEN
        ALTER TABLE uns_nodered_flow_node DROP CONSTRAINT IF EXISTS uns_nodered_flow_node_pkey;
        ALTER TABLE uns_nodered_flow_node ADD CONSTRAINT uns_nodered_flow_node_pkey PRIMARY KEY(parent_id, node_id);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS asset_file (
    id BIGSERIAL PRIMARY KEY,
    file_key VARCHAR(512) NOT NULL,
    original_name VARCHAR(256) NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 VARCHAR(64) NOT NULL DEFAULT '',
    storage_driver VARCHAR(32) NOT NULL DEFAULT 'local',
    storage_key VARCHAR(512) NOT NULL DEFAULT '',
    visibility VARCHAR(32) NOT NULL DEFAULT 'private',
    status VARCHAR(32) NOT NULL DEFAULT 'temp',
    project_id BIGINT NOT NULL DEFAULT 0,
    app_instance_id VARCHAR(64) NOT NULL DEFAULT '',
    session_id VARCHAR(64) NOT NULL DEFAULT '',
    op_id VARCHAR(128),
    expire_at TIMESTAMPTZ,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS project_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS app_instance_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS session_id VARCHAR(64) NOT NULL DEFAULT '';
-- bucket records the bucket chosen at upload time so later config changes
-- (e.g. enabling FILESTORE_S3_PUBLIC_BUCKET) never point existing files at
-- the wrong bucket; rows with '' fall back to visibility-derived resolution.
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS bucket VARCHAR(128) NOT NULL DEFAULT '';
-- multipart 分片上传：op_id 存对象存储 uploadId，expire_at 为上传会话过期时间；
-- 两列均可空，存量行不受影响（仅分片上传的 temp 记录使用）。
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS op_id VARCHAR(128);
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS expire_at TIMESTAMPTZ;
-- file_key mirrors the project-scoped storage key ({projectID}/assets/...), which
-- exceeds the legacy 64-char limit once appInstanceId/sessionId segments are present.
ALTER TABLE asset_file ALTER COLUMN file_key TYPE VARCHAR(512);
CREATE INDEX IF NOT EXISTS idx_asset_file_project_id ON asset_file(project_id);
CREATE INDEX IF NOT EXISTS idx_asset_file_app_instance_id ON asset_file(app_instance_id);
CREATE INDEX IF NOT EXISTS idx_asset_file_storage_key ON asset_file(storage_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_file_key ON asset_file(file_key) WHERE deleted_time = 0;

CREATE TABLE IF NOT EXISTS asset_binding (
    id BIGSERIAL PRIMARY KEY,
    asset_id BIGINT NOT NULL,
    owner_type VARCHAR(32) NOT NULL,
    owner_id BIGINT NOT NULL DEFAULT 0,
    usage VARCHAR(32) NOT NULL DEFAULT 'attachment',
    sort_key INTEGER NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

-- edition:project:start
CREATE TABLE IF NOT EXISTS project (
    id BIGSERIAL PRIMARY KEY,
    project_key VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    owner_user_id BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    deleted_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_key ON project(project_key) WHERE deleted_time = 0;
ALTER TABLE project ADD COLUMN IF NOT EXISTS database_name VARCHAR(128) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS project_member (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_member_project_user ON project_member(project_id, user_id);

CREATE TABLE IF NOT EXISTS project_role (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL,
    role_key VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    system_builtin BOOLEAN NOT NULL DEFAULT false,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS project_member_role (
    project_id BIGINT NOT NULL,
    member_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(project_id, member_id, role_id)
);

CREATE TABLE IF NOT EXISTS project_user_group (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    UNIQUE(project_id, group_id)
);

CREATE TABLE IF NOT EXISTS project_user_group_role (
    project_id BIGINT NOT NULL,
    project_user_group_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(project_id, project_user_group_id, role_id)
);
CREATE INDEX IF NOT EXISTS idx_project_user_group_group ON project_user_group(group_id, project_id);
CREATE INDEX IF NOT EXISTS idx_project_user_group_role_role ON project_user_group_role(project_id, role_id);

CREATE TABLE IF NOT EXISTS project_role_app (
    project_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(role_id, app_id)
);
CREATE INDEX IF NOT EXISTS idx_project_role_app_project_id ON project_role_app(project_id);
CREATE INDEX IF NOT EXISTS idx_project_role_app_app_id ON project_role_app(app_id);

CREATE TABLE IF NOT EXISTS project_role_permission (
    project_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    action_key VARCHAR(128) NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(project_id, role_id, action_key)
);

CREATE TABLE IF NOT EXISTS project_app (
    id BIGSERIAL PRIMARY KEY,
    app_principal_id UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id BIGINT NOT NULL,
    app_key VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    site_type VARCHAR(32) NOT NULL DEFAULT 'sameOriginStatic',
    entry_path VARCHAR(512) NOT NULL DEFAULT '',
    entry_url VARCHAR(512) NOT NULL DEFAULT '',
    route_prefix VARCHAR(256) NOT NULL DEFAULT '',
    gateway_route_key VARCHAR(96) NOT NULL DEFAULT '',
    oauth_client_id VARCHAR(128) NOT NULL DEFAULT '',
    icon_asset_id BIGINT NOT NULL DEFAULT 0,
    static_asset_id BIGINT NOT NULL DEFAULT 0,
    manifest_json JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS site_type VARCHAR(32) NOT NULL DEFAULT 'sameOriginStatic';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS entry_path VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS entry_url VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS route_prefix VARCHAR(256) NOT NULL DEFAULT '';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS gateway_route_key VARCHAR(96) NOT NULL DEFAULT '';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS oauth_client_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS icon_asset_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS static_asset_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS manifest_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS status VARCHAR(32) NOT NULL DEFAULT 'active';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS schema_name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS updated_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS created_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS updated_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS deleted_time BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_app_project_key ON project_app(project_id, app_key) WHERE deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_project_app_runtime_app_id ON project_app ((manifest_json->>'app_id'));
-- 清理同一项目下重复名称的活跃应用，保留 id 最小的一条，避免后续唯一索引创建失败
DELETE FROM project_app a
USING project_app b
WHERE a.id > b.id
  AND a.project_id = b.project_id
  AND a.name = b.name
  AND a.deleted_time = 0
  AND b.deleted_time = 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_app_project_name ON project_app(project_id, name) WHERE deleted_time = 0;
-- 应用 schema 预占唯一：同一 project 内 schema_name 唯一，且只对未删除、非空 schema 生效（静态应用 schema_name 为空不参与）
CREATE UNIQUE INDEX IF NOT EXISTS uk_project_app_schema ON project_app(project_id, schema_name) WHERE deleted_time = 0 AND schema_name <> '';

CREATE TABLE IF NOT EXISTS fleet_node (
    id BIGSERIAL PRIMARY KEY,
    fleet_node_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    center_type VARCHAR(32) NOT NULL DEFAULT 'enterprise',
    status VARCHAR(16) NOT NULL DEFAULT 'unjoined',
    active_session_id VARCHAR(64) NOT NULL DEFAULT '',
    first_joined_at BIGINT NOT NULL DEFAULT 0,
    last_online_at BIGINT NOT NULL DEFAULT 0,
    last_offline_at BIGINT NOT NULL DEFAULT 0,
    agent_version VARCHAR(64) NOT NULL DEFAULT '',
    protocol_version VARCHAR(32) NOT NULL DEFAULT '',
    os VARCHAR(32) NOT NULL DEFAULT '',
    arch VARCHAR(32) NOT NULL DEFAULT '',
    connect_client_id BIGINT NOT NULL DEFAULT 0,
    assignment_revision BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_fleet_node_status CHECK (status IN ('unjoined', 'online', 'offline', 'deleted'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_node_identity ON fleet_node(fleet_node_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_node_active_name ON fleet_node(LOWER(name)) WHERE status <> 'deleted';
CREATE INDEX IF NOT EXISTS idx_fleet_node_status_id ON fleet_node(status, id DESC);
ALTER TABLE fleet_node ADD COLUMN IF NOT EXISTS connect_client_id BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_node_connect_client ON fleet_node(connect_client_id) WHERE connect_client_id > 0;

CREATE TABLE IF NOT EXISTS fleet_container_snapshot (
    fleet_node_id VARCHAR(64) PRIMARY KEY,
    snapshot_id VARCHAR(64) NOT NULL,
    observed_at BIGINT NOT NULL,
    installation_id VARCHAR(128) NOT NULL DEFAULT '',
    payload JSONB NOT NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_fleet_container_snapshot_node FOREIGN KEY (fleet_node_id) REFERENCES fleet_node(fleet_node_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_container_snapshot_identity ON fleet_container_snapshot(fleet_node_id, snapshot_id);
CREATE INDEX IF NOT EXISTS idx_fleet_container_snapshot_observed ON fleet_container_snapshot(observed_at DESC);

CREATE TABLE IF NOT EXISTS fleet_container_detail (
    id BIGSERIAL PRIMARY KEY,
    fleet_node_id VARCHAR(64) NOT NULL,
    container_id VARCHAR(128) NOT NULL,
    observed_at BIGINT NOT NULL DEFAULT 0,
    payload JSONB NOT NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_fleet_container_detail_node FOREIGN KEY (fleet_node_id) REFERENCES fleet_node(fleet_node_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_container_detail_identity ON fleet_container_detail(fleet_node_id, container_id);

CREATE TABLE IF NOT EXISTS fleet_operation (
    id BIGSERIAL PRIMARY KEY,
    operation_id VARCHAR(64) NOT NULL,
    fleet_node_id VARCHAR(64) NOT NULL,
    container_id VARCHAR(128) NOT NULL,
    operation_type VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    actor_id BIGINT NOT NULL DEFAULT 0,
    request_json JSONB NOT NULL DEFAULT '{}',
    result_json JSONB NOT NULL DEFAULT '{}',
    error_code VARCHAR(128) NOT NULL DEFAULT '',
    started_at BIGINT NOT NULL DEFAULT 0,
    finished_at BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_fleet_operation_node FOREIGN KEY (fleet_node_id) REFERENCES fleet_node(fleet_node_id),
    CONSTRAINT chk_fleet_operation_status CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'timedOut', 'canceled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_operation_identity ON fleet_operation(operation_id);
CREATE INDEX IF NOT EXISTS idx_fleet_operation_node_time ON fleet_operation(fleet_node_id, created_time DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_operation_active_restart ON fleet_operation(fleet_node_id, container_id)
    WHERE operation_type = 'container.restart' AND status IN ('queued', 'running');

CREATE TABLE IF NOT EXISTS fleet_quota_lock (
    id SMALLINT PRIMARY KEY,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO fleet_quota_lock(id) VALUES (1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS fleet_account_assignment (
    id BIGSERIAL PRIMARY KEY,
    fleet_node_id VARCHAR(64) NOT NULL,
    center_user_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'assigned',
    assignment_revision BIGINT NOT NULL DEFAULT 0,
    assigned_by BIGINT NOT NULL DEFAULT 0,
    assigned_at BIGINT NOT NULL DEFAULT 0,
    unassigned_at BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_fleet_account_assignment_node FOREIGN KEY (fleet_node_id) REFERENCES fleet_node(fleet_node_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_account_assignment_node_user ON fleet_account_assignment(fleet_node_id, center_user_id);
CREATE INDEX IF NOT EXISTS idx_fleet_account_assignment_active ON fleet_account_assignment(fleet_node_id, status, center_user_id);

CREATE TABLE IF NOT EXISTS fleet_user_binding (
    id BIGSERIAL PRIMARY KEY,
    center_identity VARCHAR(128) NOT NULL,
    center_type VARCHAR(16) NOT NULL DEFAULT '',
    fleet_node_id VARCHAR(64) NOT NULL,
    source_user_id BIGINT NOT NULL,
    local_user_id BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    last_source_revision BIGINT NOT NULL DEFAULT 0,
    last_applied_at BIGINT NOT NULL DEFAULT 0,
    last_error_code VARCHAR(128) NOT NULL DEFAULT '',
    last_error_message VARCHAR(512) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_fleet_user_binding_status CHECK (status IN ('active', 'unassigned', 'conflict'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_user_binding_source ON fleet_user_binding(center_identity, fleet_node_id, source_user_id);
CREATE INDEX IF NOT EXISTS idx_fleet_user_binding_local ON fleet_user_binding(local_user_id);
CREATE INDEX IF NOT EXISTS idx_fleet_user_binding_status ON fleet_user_binding(center_identity, fleet_node_id, status);

CREATE TABLE IF NOT EXISTS fleet_user_sync_state (
    center_identity VARCHAR(128) NOT NULL,
    fleet_node_id VARCHAR(64) NOT NULL,
    center_type VARCHAR(16) NOT NULL DEFAULT '',
    last_attempt_revision BIGINT NOT NULL DEFAULT 0,
    last_applied_revision BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT '',
    last_error_code VARCHAR(128) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (center_identity, fleet_node_id)
);
CREATE INDEX IF NOT EXISTS idx_fleet_user_sync_state_revision ON fleet_user_sync_state(center_identity, fleet_node_id, last_applied_revision);

CREATE TABLE IF NOT EXISTS uns_cloud_sync_log (
    id BIGSERIAL PRIMARY KEY,
    sync_type VARCHAR(32) NOT NULL DEFAULT 'uns',
    direction VARCHAR(16) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT '',
    sync_mode VARCHAR(32) NOT NULL DEFAULT '',
    connect_client_key VARCHAR(128) NOT NULL DEFAULT '',
    node_id VARCHAR(128) NOT NULL DEFAULT '',
    node_name VARCHAR(128) NOT NULL DEFAULT '',
    snapshot_version BIGINT NOT NULL DEFAULT 0,
    snapshot_hash VARCHAR(64) NOT NULL DEFAULT '',
    result_code VARCHAR(64) NOT NULL DEFAULT '',
    summary VARCHAR(512) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    environment JSONB NOT NULL DEFAULT '{}',
    details JSONB NOT NULL DEFAULT '{}',
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0
);
ALTER TABLE uns_cloud_sync_log ADD COLUMN IF NOT EXISTS sync_type VARCHAR(32) NOT NULL DEFAULT 'uns';
ALTER TABLE uns_cloud_sync_log ADD COLUMN IF NOT EXISTS result_code VARCHAR(64) NOT NULL DEFAULT '';
UPDATE uns_cloud_sync_log SET sync_type = 'uns' WHERE sync_type = '';
CREATE INDEX IF NOT EXISTS idx_uns_cloud_sync_log_type_time ON uns_cloud_sync_log(sync_type, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_uns_cloud_sync_log_result_time ON uns_cloud_sync_log(result_code, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_uns_cloud_sync_log_client_time ON uns_cloud_sync_log(connect_client_key, created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_uns_cloud_sync_log_node_time ON uns_cloud_sync_log(node_id, created_time DESC, id DESC);

-- edition:cluster:end

DO $$
DECLARE
    tbl text;
BEGIN
    FOREACH tbl IN ARRAY ARRAY[
        'asset_file',
        'asset_binding',
        'project',
        'project_role',
        'project_app',
        'fleet_node',
        'fleet_account_assignment'
    ]
    LOOP
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = tbl
              AND column_name = 'created_time'
              AND data_type = 'bigint'
        ) THEN
            EXECUTE format('ALTER TABLE %I ALTER COLUMN created_time DROP DEFAULT', tbl);
            EXECUTE format(
                'ALTER TABLE %I ALTER COLUMN created_time TYPE TIMESTAMPTZ USING CASE WHEN created_time > 0 THEN to_timestamp(created_time / 1000.0) ELSE CURRENT_TIMESTAMP END',
                tbl
            );
            EXECUTE format('ALTER TABLE %I ALTER COLUMN created_time SET DEFAULT CURRENT_TIMESTAMP', tbl);
            EXECUTE format('ALTER TABLE %I ALTER COLUMN created_time SET NOT NULL', tbl);
        END IF;

        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = tbl
              AND column_name = 'updated_time'
              AND data_type = 'bigint'
        ) THEN
            EXECUTE format('ALTER TABLE %I ALTER COLUMN updated_time DROP DEFAULT', tbl);
            EXECUTE format(
                'ALTER TABLE %I ALTER COLUMN updated_time TYPE TIMESTAMPTZ USING CASE WHEN updated_time > 0 THEN to_timestamp(updated_time / 1000.0) ELSE CURRENT_TIMESTAMP END',
                tbl
            );
            EXECUTE format('ALTER TABLE %I ALTER COLUMN updated_time SET DEFAULT CURRENT_TIMESTAMP', tbl);
            EXECUTE format('ALTER TABLE %I ALTER COLUMN updated_time SET NOT NULL', tbl);
        END IF;
    END LOOP;
END $$;

-- Anchor 模型库
CREATE TABLE IF NOT EXISTS anchor_model (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    origin_file VARCHAR(512) NOT NULL DEFAULT '',
    file_asset_id BIGINT NOT NULL DEFAULT 0,
    thumbnail_asset_id BIGINT NOT NULL DEFAULT 0,
    file_size BIGINT NOT NULL DEFAULT 0,
    nodes_json JSONB NOT NULL DEFAULT '[]',
    status SMALLINT NOT NULL DEFAULT 2,
    error_msg TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_anchor_model_name ON anchor_model(name);
CREATE INDEX IF NOT EXISTS idx_anchor_model_created ON anchor_model(created_time);

CREATE TABLE IF NOT EXISTS anchor_instance (
    id BIGSERIAL PRIMARY KEY,
    model_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    uns_node_id VARCHAR(128) NOT NULL DEFAULT '',
    topic VARCHAR(512) NOT NULL DEFAULT '',
    binding_json JSONB NOT NULL DEFAULT '{}',
    height REAL NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 2,
    error_msg TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_anchor_instance_model ON anchor_instance(model_id);
CREATE INDEX IF NOT EXISTS idx_anchor_instance_created ON anchor_instance(created_time);

CREATE TABLE IF NOT EXISTS anchor_scene (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config_json JSONB NOT NULL DEFAULT '{}',
    thumbnail_asset_id BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_anchor_scene_created ON anchor_scene(created_time);

CREATE TABLE IF NOT EXISTS anchor_scene_item (
    id BIGSERIAL PRIMARY KEY,
    scene_id BIGINT NOT NULL,
    instance_id BIGINT NOT NULL,
    placement_json JSONB NOT NULL DEFAULT '{}',
    sort INT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_anchor_scene_item_scene ON anchor_scene_item(scene_id);
CREATE INDEX IF NOT EXISTS idx_anchor_scene_item_instance ON anchor_scene_item(instance_id);

-- 视频摄像头与接入通道
CREATE TABLE IF NOT EXISTS sys_video_camera (
    id BIGSERIAL PRIMARY KEY,
    camera_code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    location VARCHAR(256) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status SMALLINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sys_video_camera_code_deleted
    ON sys_video_camera(camera_code, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_video_camera_name ON sys_video_camera(name);

CREATE TABLE IF NOT EXISTS sys_video_channel (
    id BIGSERIAL PRIMARY KEY,
    camera_id BIGINT NOT NULL,
    channel_code VARCHAR(128) NOT NULL,
    transport VARCHAR(32) NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    credential_ciphertext TEXT NOT NULL DEFAULT '',
    rtp_type VARCHAR(16) NOT NULL DEFAULT 'tcp',
    app VARCHAR(64) NOT NULL DEFAULT 'live',
    stream VARCHAR(128) NOT NULL,
    live_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    resolution VARCHAR(32) NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    last_tested_at TIMESTAMPTZ,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sys_video_channel_code_deleted
    ON sys_video_channel(channel_code, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_video_channel_camera_deleted
    ON sys_video_channel(camera_id, deleted_time);

CREATE TABLE IF NOT EXISTS sys_vision_algorithm (
    id BIGSERIAL PRIMARY KEY,
    algorithm_code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    source VARCHAR(16) NOT NULL DEFAULT 'custom',
    algo_type VARCHAR(32) NOT NULL DEFAULT 'object_detection',
    description TEXT NOT NULL DEFAULT '',
    scene VARCHAR(256) NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '[]',
    default_output TEXT NOT NULL DEFAULT '[]',
    model_format VARCHAR(16) NOT NULL DEFAULT 'onnx',
    model_status VARCHAR(16) NOT NULL DEFAULT 'missing',
    model_file VARCHAR(256) NOT NULL DEFAULT '',
    model_key VARCHAR(512) NOT NULL DEFAULT '',
    model_error TEXT NOT NULL DEFAULT '',
    model_id BIGINT NOT NULL DEFAULT 0,
    definition TEXT NOT NULL DEFAULT '{}',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sys_vision_algorithm_code_deleted
    ON sys_vision_algorithm(algorithm_code, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_vision_algorithm_source ON sys_vision_algorithm(source);

CREATE TABLE IF NOT EXISTS sys_vision_task (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    camera_ids TEXT NOT NULL DEFAULT '[]',
    algorithm_id BIGINT NOT NULL DEFAULT 0,
    output_categories TEXT NOT NULL DEFAULT '[]',
    config TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'stopped',
    desired_status VARCHAR(16) NOT NULL DEFAULT 'stopped',
    observed_status VARCHAR(16) NOT NULL DEFAULT 'stopped',
    observed_reason TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TIMESTAMPTZ,
    processed_frames BIGINT NOT NULL DEFAULT 0,
    detected_objects BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    uns_topic VARCHAR(512) NOT NULL DEFAULT '',
    last_run_at TIMESTAMPTZ,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_task_status ON sys_vision_task(status);
CREATE INDEX IF NOT EXISTS idx_sys_vision_task_desired ON sys_vision_task(desired_status);
CREATE INDEX IF NOT EXISTS idx_sys_vision_task_algorithm ON sys_vision_task(algorithm_id);

CREATE TABLE IF NOT EXISTS sys_vision_worker (
    id BIGSERIAL PRIMARY KEY,
    worker_id VARCHAR(128) NOT NULL,
    url VARCHAR(512) NOT NULL DEFAULT '',
    capabilities TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'offline',
    last_seen_at TIMESTAMPTZ,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_sys_vision_worker_worker_id ON sys_vision_worker(worker_id);

CREATE TABLE IF NOT EXISTS sys_vision_job_assignment (
    task_id BIGINT PRIMARY KEY,
    worker_id VARCHAR(128) NOT NULL DEFAULT '',
    worker_task_id VARCHAR(128) NOT NULL DEFAULT '',
    generation INT NOT NULL DEFAULT 1,
    state VARCHAR(16) NOT NULL DEFAULT 'assigned',
    last_error TEXT NOT NULL DEFAULT '',
    assigned_at TIMESTAMPTZ,
    verified_at TIMESTAMPTZ,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_job_assignment_worker ON sys_vision_job_assignment(worker_id);

CREATE TABLE IF NOT EXISTS sys_vision_latest_result (
    task_id BIGINT NOT NULL,
    camera_id BIGINT NOT NULL,
    event_id VARCHAR(256) NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    object_count INT NOT NULL DEFAULT 0,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, camera_id)
);

CREATE TABLE IF NOT EXISTS sys_vision_alarm (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL DEFAULT 0,
    camera_id BIGINT NOT NULL DEFAULT 0,
    algorithm_code VARCHAR(128) NOT NULL DEFAULT '',
    alarm_type VARCHAR(64) NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    event_id VARCHAR(256) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_alarm_task ON sys_vision_alarm(task_id);
CREATE INDEX IF NOT EXISTS idx_sys_vision_alarm_created ON sys_vision_alarm(created_time DESC);

CREATE TABLE IF NOT EXISTS sys_vision_publish_outbox (
    id BIGSERIAL PRIMARY KEY,
    topic VARCHAR(512) NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    event_id VARCHAR(256) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_outbox_status ON sys_vision_publish_outbox(status);

CREATE TABLE IF NOT EXISTS sys_vision_event (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL DEFAULT 0,
    camera_id BIGINT NOT NULL DEFAULT 0,
    algorithm_code VARCHAR(128) NOT NULL DEFAULT '',
    event_type VARCHAR(16) NOT NULL DEFAULT 'alarm',
    event_name VARCHAR(128) NOT NULL DEFAULT '',
    level VARCHAR(16) NOT NULL DEFAULT 'warning',
    message TEXT NOT NULL DEFAULT '',
    result_content TEXT NOT NULL DEFAULT '[]',
    payload TEXT NOT NULL DEFAULT '{}',
    screenshot_asset_id BIGINT NOT NULL DEFAULT 0,
    event_id VARCHAR(256) NOT NULL DEFAULT '',
    uns_topic VARCHAR(512) NOT NULL DEFAULT '',
    retention VARCHAR(16) NOT NULL DEFAULT 'permanent',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0,
    review_status VARCHAR(24) NOT NULL DEFAULT 'pending',
    review_revision BIGINT NOT NULL DEFAULT 0,
    reviewed_by BIGINT NOT NULL DEFAULT 0,
    reviewed_time TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_event_created ON sys_vision_event(created_time DESC);
CREATE INDEX IF NOT EXISTS idx_sys_vision_event_task ON sys_vision_event(task_id);
CREATE INDEX IF NOT EXISTS idx_sys_vision_event_camera ON sys_vision_event(camera_id);
CREATE INDEX IF NOT EXISTS idx_sys_vision_event_name ON sys_vision_event(event_name);

CREATE TABLE IF NOT EXISTS sys_vision_model (
    id BIGSERIAL PRIMARY KEY,
    model_code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL DEFAULT '',
    version VARCHAR(32) NOT NULL DEFAULT '1.0.0',
    source VARCHAR(16) NOT NULL DEFAULT 'custom',
    format VARCHAR(16) NOT NULL DEFAULT 'onnx',
    file_name VARCHAR(256) NOT NULL DEFAULT '',
    storage_key VARCHAR(512) NOT NULL DEFAULT '',
    sha256 VARCHAR(64) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    license VARCHAR(64) NOT NULL DEFAULT '',
    source_url VARCHAR(512) NOT NULL DEFAULT '',
    model_type VARCHAR(32) NOT NULL DEFAULT '',
    input_width INT NOT NULL DEFAULT 640,
    input_height INT NOT NULL DEFAULT 640,
    labels TEXT NOT NULL DEFAULT '[]',
    preprocess TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'available',
    error_message TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_sys_vision_model_code ON sys_vision_model(model_code, deleted_time);

INSERT INTO sys_vision_algorithm
    (algorithm_code, name, source, algo_type, description, labels, default_output, model_status)
VALUES
    ('object_detection', 'Object Detection', 'builtin', 'object_detection',
     'Detects people and common objects with labels, boxes, and confidence scores.',
     '["person"]', '["person"]', 'missing'),
    ('hard_hat', 'Helmet Detection', 'builtin', 'helmet',
     'Detects whether workers are wearing helmets and outputs violation alerts.',
     '["person","helmet","no_helmet"]', '["person","helmet","no_helmet"]', 'missing'),
    ('static_delimitation_area', 'Area Intrusion', 'builtin', 'intrusion',
     'Detects people entering a restricted ROI and outputs an intrusion alert state.',
     '["person"]', '["intrusion"]', 'missing'),
    ('line_counting', 'Line Counting', 'builtin', 'line_counting',
     'Tracks targets crossing a counting line and reports cumulative in/out counts.',
     '["person"]', '["in","out"]', 'missing')
ON CONFLICT (algorithm_code, deleted_time) DO NOTHING;

-- ==================== TASK-030 企业版消息通知（P0） ====================
-- 语义基线：云端 sys_notify_message_request / sys_message_info / sys_user_message / sys_notification。
-- 时间列毫秒 BIGINT DEFAULT 0；软删 deleted_time=0 表示活跃（企业版惯例）。

CREATE TABLE IF NOT EXISTS sys_notify_message_request (
    id BIGSERIAL PRIMARY KEY,
    key_id BIGINT NOT NULL DEFAULT 0,
    recipient_user_id BIGINT NOT NULL DEFAULT 0,
    type VARCHAR(16) NOT NULL DEFAULT 'inbox',
    mode VARCHAR(8) NOT NULL DEFAULT 'live',
    title VARCHAR(64) NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    channels VARCHAR(16) NOT NULL DEFAULT '',
    sender_type VARCHAR(8) NOT NULL DEFAULT 'other',
    sender_id VARCHAR(128) NOT NULL DEFAULT '',
    sender_name VARCHAR(100) NOT NULL DEFAULT '',
    sender_detail VARCHAR(512) NOT NULL DEFAULT '',
    -- 服务端按 API Key 解析出的 Launchpad 应用与项目 id（0=非应用来源或解析不到）
    sender_app_id BIGINT NOT NULL DEFAULT 0,
    sender_project_id BIGINT NOT NULL DEFAULT 0,
    action_type VARCHAR(16) NOT NULL DEFAULT '',
    action_path VARCHAR(500) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'accepted',
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    message VARCHAR(512) NOT NULL DEFAULT '',
    request_digest VARCHAR(64) NOT NULL DEFAULT '',
    idempotency_key VARCHAR(128) NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    completed_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

-- 幂等第二防线：同 Key 同幂等键仅一条活跃记录（退休后可插新行）
CREATE UNIQUE INDEX IF NOT EXISTS uk_notify_request_idem
    ON sys_notify_message_request(key_id, idempotency_key) WHERE deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_notify_request_status
    ON sys_notify_message_request(status, created_time);

CREATE TABLE IF NOT EXISTS sys_notify_message (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(16) NOT NULL DEFAULT 'inbox',
    group_code VARCHAR(32) NOT NULL DEFAULT 'system',
    title VARCHAR(128) NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    link VARCHAR(500) NOT NULL DEFAULT '',
    banner_start BIGINT NOT NULL DEFAULT 0,
    banner_end BIGINT NOT NULL DEFAULT 0,
    source VARCHAR(16) NOT NULL DEFAULT '',
    mode VARCHAR(8) NOT NULL DEFAULT '',
    sender_type VARCHAR(8) NOT NULL DEFAULT '',
    sender_id VARCHAR(128) NOT NULL DEFAULT '',
    sender_name VARCHAR(100) NOT NULL DEFAULT '',
    sender_detail VARCHAR(512) NOT NULL DEFAULT '',
    -- 服务端按 API Key 解析出的 Launchpad 应用与项目 id（0=非应用来源或解析不到）
    sender_app_id BIGINT NOT NULL DEFAULT 0,
    sender_project_id BIGINT NOT NULL DEFAULT 0,
    notify_request_id BIGINT NOT NULL DEFAULT 0,
    notification_id BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

-- 幂等守卫：一条 OpenAPI 请求只能建一条消息
CREATE UNIQUE INDEX IF NOT EXISTS uk_notify_message_request
    ON sys_notify_message(notify_request_id) WHERE notify_request_id > 0;
CREATE INDEX IF NOT EXISTS idx_notify_message_banner
    ON sys_notify_message(type, banner_start, banner_end) WHERE type = 'banner';
CREATE INDEX IF NOT EXISTS idx_notify_message_history
    ON sys_notify_message(created_time DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_notify_message_sender_history
    ON sys_notify_message(sender_type, sender_id, created_time DESC, id DESC);

CREATE TABLE IF NOT EXISTS sys_user_message (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    is_read BIGINT NOT NULL DEFAULT 0,
    is_handled BIGINT NOT NULL DEFAULT 0,
    read_time BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_user_message_pair
    ON sys_user_message(user_id, message_id) WHERE deleted_time = 0;
CREATE INDEX IF NOT EXISTS idx_user_message_list
    ON sys_user_message(user_id, deleted_time, is_read, id);
CREATE INDEX IF NOT EXISTS idx_user_message_history
    ON sys_user_message(message_id, user_id);

CREATE TABLE IF NOT EXISTS sys_notification (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(16) NOT NULL DEFAULT 'inbox',
    title VARCHAR(128) NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    link VARCHAR(500) NOT NULL DEFAULT '',
    as_modal BIGINT NOT NULL DEFAULT 0,
    audience_type BIGINT NOT NULL DEFAULT 1,
    audience_user_ids TEXT NOT NULL DEFAULT '',
    schedule_time BIGINT NOT NULL DEFAULT 0,
    status BIGINT NOT NULL DEFAULT 1,
    send_statistics TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_notification_status
    ON sys_notification(status, schedule_time);

-- TASK-030 补充：站内信强提醒标记（管理端公告 as_modal 透传到消息级，供全局 Modal 展示）
ALTER TABLE sys_notify_message ADD COLUMN IF NOT EXISTS as_modal BIGINT NOT NULL DEFAULT 0;

-- 存量 API Key 回填通知资源键（Codex review P1：档位映射只作用于新建/更新，
-- 升级前签发的 Key 缺 notifications.* 会被鉴权交集拒绝；NOT LIKE 保证幂等）
UPDATE sys_api_key SET resource_keys = resource_keys || ',notifications.read'
WHERE deleted_time = 0 AND permission = 'read_only'
  AND resource_keys NOT LIKE '%notifications.read%';
UPDATE sys_api_key SET resource_keys = resource_keys || ',notifications.read,notifications.send'
WHERE deleted_time = 0 AND permission IN ('data_writer', 'full_access')
  AND resource_keys NOT LIKE '%notifications.send%';

-- Source/Stream configuration is stored only in public.uns_meta.
-- Runtime ledgers are created in the UNS schema by its versioned migrations.

-- MCP 授权为平台数据，与原 OAuth 和 UNS 数据表隔离。
CREATE TABLE IF NOT EXISTS sys_mcp_oauth_client (
    client_id VARCHAR(80) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    redirect_uris JSONB NOT NULL,
    scope TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS sys_mcp_oauth_request (
    request_id VARCHAR(80) PRIMARY KEY,
    client_id VARCHAR(80) NOT NULL REFERENCES sys_mcp_oauth_client(client_id),
    redirect_uri TEXT NOT NULL,
    resource TEXT NOT NULL,
    issuer TEXT NOT NULL,
    scope TEXT NOT NULL,
    state TEXT NOT NULL,
    challenge VARCHAR(64) NOT NULL,
    browser_hash VARCHAR(64) NOT NULL,
    expires_at BIGINT NOT NULL,
    used_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS sys_mcp_oauth_grant (
    grant_id VARCHAR(80) PRIMARY KEY,
    client_id VARCHAR(80) NOT NULL REFERENCES sys_mcp_oauth_client(client_id),
    user_id BIGINT NOT NULL,
    resource TEXT NOT NULL,
    issuer TEXT NOT NULL,
    scope TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    revoked_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_mcp_oauth_grant_owner ON sys_mcp_oauth_grant(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS sys_mcp_oauth_token (
    token_hash VARCHAR(64) PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    token_type VARCHAR(16) NOT NULL CHECK (token_type IN ('code','access','refresh')),
    grant_id VARCHAR(80) NOT NULL REFERENCES sys_mcp_oauth_grant(grant_id),
    redirect_uri TEXT NOT NULL DEFAULT '',
    challenge VARCHAR(64) NOT NULL DEFAULT '',
    scope TEXT NOT NULL,
    expires_at BIGINT NOT NULL,
    used_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_mcp_oauth_token_grant ON sys_mcp_oauth_token(grant_id);
-- Collaboration registration is separate from legacy MQTT and UNS mirror state.
CREATE TABLE IF NOT EXISTS fleet_collaboration_node (
    node_id VARCHAR(64) PRIMARY KEY REFERENCES fleet_node(fleet_node_id),
    registration_status VARCHAR(24) NOT NULL DEFAULT 'unregistered',
    public_key TEXT NOT NULL DEFAULT '',
    key_id VARCHAR(128) NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    network_node_id VARCHAR(128),
    registered_at BIGINT NOT NULL DEFAULT 0,
    checked_at BIGINT NOT NULL DEFAULT 0,
    network_status VARCHAR(24) NOT NULL DEFAULT 'unchecked',
    business_status VARCHAR(24) NOT NULL DEFAULT 'unchecked',
    CHECK (registration_status IN ('unregistered', 'registered', 'revoked'))
);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_collaboration_network_identity ON fleet_collaboration_node(network_node_id) WHERE network_node_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS fleet_collaboration_public_key ON fleet_collaboration_node(public_key) WHERE public_key <> '';
CREATE TABLE IF NOT EXISTS fleet_collaboration_enrollment (
    code_hash VARCHAR(64) PRIMARY KEY,
    node_id VARCHAR(64) NOT NULL REFERENCES fleet_node(fleet_node_id),
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    proof_hash VARCHAR(64) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS fleet_collaboration_enrollment_node ON fleet_collaboration_enrollment(node_id);
CREATE TABLE IF NOT EXISTS fleet_service_grant (
 grant_id text PRIMARY KEY,
 node_id text NOT NULL,
 scope text NOT NULL,
 resource_id text NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 enabled boolean NOT NULL DEFAULT true,
 created_by bigint NOT NULL,
 created_at bigint NOT NULL,
 UNIQUE(node_id,scope,resource_id)
);
CREATE TABLE IF NOT EXISTS fleet_proof_nonce (
 node_id text NOT NULL,
 nonce text NOT NULL,
 expires_at bigint NOT NULL,
 PRIMARY KEY(node_id,nonce)
);
CREATE INDEX IF NOT EXISTS fleet_proof_nonce_expiry ON fleet_proof_nonce(expires_at);

ALTER TABLE fleet_collaboration_node ADD COLUMN IF NOT EXISTS installation_id text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS fleet_collaboration_installation ON fleet_collaboration_node(installation_id) WHERE installation_id <> '';
-- Retain revoked grants and orphan backlog; a later consumer has its own identity.
ALTER TABLE fleet_service_grant DROP CONSTRAINT IF EXISTS fleet_service_grant_node_id_scope_resource_id_key;
CREATE INDEX IF NOT EXISTS fleet_service_grant_lookup ON fleet_service_grant(node_id,scope,resource_id) WHERE enabled;

CREATE TABLE IF NOT EXISTS fleet_ssh_profile (
 node_id VARCHAR(128) PRIMARY KEY, host TEXT NOT NULL, port INTEGER NOT NULL,
 username TEXT NOT NULL, auth_type TEXT NOT NULL, credential_ref TEXT NOT NULL,
 host_key_fingerprint TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT false, revision BIGINT NOT NULL
);
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS default_username TEXT NOT NULL DEFAULT '';
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS default_port INTEGER NOT NULL DEFAULT 0;
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS connection_mode TEXT NOT NULL DEFAULT 'automatic';
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS managed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS setup_state TEXT NOT NULL DEFAULT '';
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS setup_error TEXT NOT NULL DEFAULT '';
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS last_checked_at BIGINT NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS fleet_ssh_execution (
 id VARCHAR(128) PRIMARY KEY, node_id VARCHAR(128) NOT NULL, actor_id BIGINT NOT NULL,
 api_key_id BIGINT NOT NULL, process_id VARCHAR(128) NOT NULL, script TEXT NOT NULL,
 status TEXT NOT NULL, output TEXT NOT NULL DEFAULT '', truncated BOOLEAN NOT NULL DEFAULT false,
 exit_code INTEGER, created_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS fleet_ssh_execution_actor ON fleet_ssh_execution(node_id,actor_id,created_at);

CREATE TABLE IF NOT EXISTS fleet_ssh_terminal (
 id VARCHAR(128) PRIMARY KEY,node_id VARCHAR(128) NOT NULL,actor_id BIGINT NOT NULL,
 api_key_id BIGINT NOT NULL,process_id VARCHAR(128) NOT NULL,state TEXT NOT NULL,
 output BYTEA NOT NULL DEFAULT '',output_offset BIGINT NOT NULL DEFAULT 0,sequence BIGINT NOT NULL DEFAULT 0,
 created_at BIGINT NOT NULL,expires_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS fleet_ssh_terminal_actor ON fleet_ssh_terminal(node_id,actor_id,created_at);

-- Confirmed business reconnect time, distinct from Agent heartbeat and probe time.
ALTER TABLE fleet_collaboration_node ADD COLUMN IF NOT EXISTS last_connected_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE fleet_ssh_terminal ADD COLUMN IF NOT EXISTS container_id VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE fleet_ssh_terminal ADD COLUMN IF NOT EXISTS username VARCHAR(128) NOT NULL DEFAULT '';
-- Pin only the dedicated callback origin authenticated by node enrollment.
ALTER TABLE fleet_collaboration_node ADD COLUMN IF NOT EXISTS tls_fingerprint text NOT NULL DEFAULT '';

ALTER TABLE fleet_ssh_terminal ADD COLUMN IF NOT EXISTS error_code VARCHAR(64) NOT NULL DEFAULT '';

-- Immutable V3.0.0_002 expects the retired sync-config baseline table.
-- Bootstrap only until that migration has run; retirement removes the bridge.
DO $bridge$
BEGIN
 IF to_regclass('public.cluster_sync_config') IS NOT NULL THEN RETURN; END IF;
 IF to_regclass('public.sys_schema_migration') IS NOT NULL THEN
  IF EXISTS(SELECT 1 FROM public.sys_schema_migration WHERE scope='common'
   AND version='3.0.0' AND sequence=2 AND name='uns_sync_config' AND success) THEN RETURN; END IF;
 END IF;
 CREATE TABLE public.cluster_sync_config (id bigint PRIMARY KEY);
END $bridge$;
