-- sys_api_key 增加 app_id：应用级凭证的结构化归属标识（与 Cloud 侧同构）。
-- 只加列，不做数据回填：存量 app key（owner_type='service'、name LIKE 'app-%'）
-- 按迁移策略保持原样，正在运行的实例不受影响；应用重新部署时换签为 app 型新 key
-- 并写入 app_id。查询应用凭证请使用双兼容条件：
--   owner_type='app' OR (owner_type='service' AND usage_type='internal' AND name LIKE 'app-%')
ALTER TABLE sys_api_key ADD COLUMN IF NOT EXISTS app_id VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sys_api_key_app_id ON sys_api_key (app_id);
