CREATE TABLE IF NOT EXISTS sys_vision_task (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    camera_ids TEXT NOT NULL DEFAULT '[]',
    algorithm_id BIGINT NOT NULL DEFAULT 0,
    output_categories TEXT NOT NULL DEFAULT '[]',
    config TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'stopped',
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
CREATE INDEX IF NOT EXISTS idx_sys_vision_task_algorithm ON sys_vision_task(algorithm_id);
