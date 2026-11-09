-- 平台级系统设置键值表;当前用于登录有效期(login_session_duration),后续系统级配置可复用。
CREATE TABLE IF NOT EXISTS sys_setting (
    setting_key VARCHAR(128) PRIMARY KEY,
    setting_value TEXT NOT NULL DEFAULT '',
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
