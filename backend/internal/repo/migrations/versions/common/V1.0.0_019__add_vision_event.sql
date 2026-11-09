-- 视觉事件流(Results 页):告警事件与数据事件(如 Line Count Updated),
-- 带证据截图(asset 引用)、结果内容标签与追溯信息。
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
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_event_created ON sys_vision_event(created_time DESC);
CREATE INDEX IF NOT EXISTS idx_sys_vision_event_task ON sys_vision_event(task_id);
CREATE INDEX IF NOT EXISTS idx_sys_vision_event_camera ON sys_vision_event(camera_id);
CREATE INDEX IF NOT EXISTS idx_sys_vision_event_name ON sys_vision_event(event_name);
