-- Vision 任务"触发运行"能力:双状态模型 + worker registry + assignment 持久化。
-- desired_status 表达用户意图(只 API 写),observed_status 由 reconcile 循环和 worker
-- heartbeat/ingest 写;保留 status 作为 legacy 兼容镜像。

-- 新增列默认 'stopped';存量任务本就处于 stopped,无需回填(且回填 UPDATE 与
-- ALTER 处于同一执行批次会看不到刚加的列,故不在本迁移内回填)。
ALTER TABLE sys_vision_task
    ADD COLUMN IF NOT EXISTS desired_status VARCHAR(16) NOT NULL DEFAULT 'stopped',
    ADD COLUMN IF NOT EXISTS observed_status VARCHAR(16) NOT NULL DEFAULT 'stopped',
    ADD COLUMN IF NOT EXISTS observed_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

-- 注:desired_status 上的索引放到 V1.0.0_010,因为同一迁移(单事务批次)内
-- 后续语句看不到刚 ALTER 新增的列。

-- 执行面 worker 注册表。worker 自注册并周期心跳;master 侧健康检查刷新 status。
CREATE TABLE IF NOT EXISTS sys_vision_worker (
    id BIGSERIAL PRIMARY KEY,
    worker_id VARCHAR(128) NOT NULL,
    url VARCHAR(512) NOT NULL DEFAULT '',
    capabilities TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'offline',
    last_seen_at TIMESTAMPTZ,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_sys_vision_worker_worker_id ON sys_vision_worker(worker_id);

-- job -> worker assignment 持久化。一个 task 同时只有一条 assignment(task_id 唯一)。
-- generation 单调递增,用于区分被 master 替换后的旧任务重启(防僵尸)。
CREATE TABLE IF NOT EXISTS sys_vision_job_assignment (
    task_id BIGINT PRIMARY KEY,
    worker_id VARCHAR(128) NOT NULL DEFAULT '',
    worker_task_id VARCHAR(128) NOT NULL DEFAULT '',
    generation INT NOT NULL DEFAULT 1,
    state VARCHAR(16) NOT NULL DEFAULT 'assigned',
    last_error TEXT NOT NULL DEFAULT '',
    assigned_at TIMESTAMPTZ,
    verified_at TIMESTAMPTZ,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sys_vision_job_assignment_worker ON sys_vision_job_assignment(worker_id);
