-- 每个任务-相机的最新一帧识别结果(供 Results 页展示与实时刷新)。
CREATE TABLE IF NOT EXISTS sys_vision_latest_result (
    task_id BIGINT NOT NULL,
    camera_id BIGINT NOT NULL,
    event_id VARCHAR(256) NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    object_count INT NOT NULL DEFAULT 0,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, camera_id)
);
