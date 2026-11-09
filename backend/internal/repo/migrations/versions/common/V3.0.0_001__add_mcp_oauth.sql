-- MCP 公共客户端与用户授权；只保存随机令牌的 SHA-256，不存原始凭据。
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
