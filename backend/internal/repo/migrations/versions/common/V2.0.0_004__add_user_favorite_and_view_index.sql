-- 用户收藏表，用于 Launchpad 收藏持久化
CREATE TABLE IF NOT EXISTS sys_user_favorite (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    res_type VARCHAR(50) NOT NULL,
    res_id VARCHAR(50) NOT NULL,
    res_name VARCHAR(256) NOT NULL DEFAULT '',
    created_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_user_favorite_user_res
    ON sys_user_favorite(user_id, res_type, res_id);

CREATE INDEX IF NOT EXISTS idx_sys_user_favorite_user_type_time
    ON sys_user_favorite(user_id, res_type, created_time DESC);

-- 加速最近应用 View 查询
CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_user_view_time
    ON sys_user_oper_log(user_id, res_type, business_type, created_time DESC, id DESC);
