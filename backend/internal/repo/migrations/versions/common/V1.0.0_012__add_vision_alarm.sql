-- 视觉分析告警(alarm.triggered 时落库),供告警列表与 Results 页展示。
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
