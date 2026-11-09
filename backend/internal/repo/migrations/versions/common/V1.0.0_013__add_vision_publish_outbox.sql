-- MQTT/UNS 发布事务性 outbox:ingest 时先写 pending,后台 flush 循环发布后标记 sent。
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
