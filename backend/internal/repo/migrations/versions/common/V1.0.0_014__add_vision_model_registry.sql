-- 模型注册表:模型与算法解耦。模型是感知能力(权重文件+运行时规格),
-- 算法通过 model_id 引用模型;多个算法可共享同一模型。
CREATE TABLE IF NOT EXISTS sys_vision_model (
    id BIGSERIAL PRIMARY KEY,
    model_code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL DEFAULT '',
    version VARCHAR(32) NOT NULL DEFAULT '1.0.0',
    source VARCHAR(16) NOT NULL DEFAULT 'custom',
    format VARCHAR(16) NOT NULL DEFAULT 'onnx',
    file_name VARCHAR(256) NOT NULL DEFAULT '',
    storage_key VARCHAR(512) NOT NULL DEFAULT '',
    sha256 VARCHAR(64) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    license VARCHAR(64) NOT NULL DEFAULT '',
    source_url VARCHAR(512) NOT NULL DEFAULT '',
    model_type VARCHAR(32) NOT NULL DEFAULT '',
    input_width INT NOT NULL DEFAULT 640,
    input_height INT NOT NULL DEFAULT 640,
    labels TEXT NOT NULL DEFAULT '[]',
    status VARCHAR(16) NOT NULL DEFAULT 'available',
    error_message TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_sys_vision_model_code ON sys_vision_model(model_code, deleted_time);

-- 算法引用模型 + 声明式定义(outputs/spatialMode/paramsSchema/rules)。
-- 注:引用新列的回填 UPDATE 放 V1.0.0_015(同迁移批次内看不到刚 ALTER 的列)。
ALTER TABLE sys_vision_algorithm
    ADD COLUMN IF NOT EXISTS model_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS definition TEXT NOT NULL DEFAULT '{}';
