-- App Gateway 路由表
CREATE TABLE IF NOT EXISTS app_gateway_routes (
    id BIGSERIAL PRIMARY KEY,
    app_id VARCHAR(36) NOT NULL UNIQUE,
    project_id BIGINT NOT NULL,
    access_mode VARCHAR(20) NOT NULL DEFAULT 'port', -- 'port' | 'subdomain'
    port INT UNIQUE,
    domain VARCHAR(255),
    container_host VARCHAR(50) NOT NULL DEFAULT '172.18.0.1',
    container_port INT NOT NULL,
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_app_gateway_routes_port ON app_gateway_routes(port);
CREATE INDEX IF NOT EXISTS idx_app_gateway_routes_domain ON app_gateway_routes(domain);
CREATE INDEX IF NOT EXISTS idx_app_gateway_routes_project_id ON app_gateway_routes(project_id);

-- domain partial unique index（只对非空 domain 生效）
CREATE UNIQUE INDEX IF NOT EXISTS idx_app_gateway_routes_domain_unique
    ON app_gateway_routes (domain) WHERE domain != '';

-- updated_time 触发器
CREATE OR REPLACE FUNCTION set_app_gateway_routes_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_time = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_app_gateway_routes_updated_at ON app_gateway_routes;
CREATE TRIGGER trg_app_gateway_routes_updated_at
    BEFORE UPDATE ON app_gateway_routes
    FOR EACH ROW EXECUTE FUNCTION set_app_gateway_routes_updated_at();
