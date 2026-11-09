CREATE TABLE IF NOT EXISTS sys_user_login_log (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL DEFAULT 0,
    user_name VARCHAR(100) NOT NULL DEFAULT '',
    ip_addr VARCHAR(64) NOT NULL DEFAULT '',
    login_location VARCHAR(100) NOT NULL DEFAULT '',
    browser VARCHAR(50) NOT NULL DEFAULT '',
    os VARCHAR(50) NOT NULL DEFAULT '',
    code BIGINT NOT NULL DEFAULT 200,
    msg VARCHAR(255) NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sys_user_login_log_created_time ON sys_user_login_log(created_time DESC);
CREATE INDEX IF NOT EXISTS idx_sys_user_login_log_user_time ON sys_user_login_log(user_name, created_time DESC);
