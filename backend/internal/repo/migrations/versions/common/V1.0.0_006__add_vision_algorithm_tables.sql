CREATE TABLE IF NOT EXISTS sys_vision_algorithm (
    id BIGSERIAL PRIMARY KEY,
    algorithm_code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    source VARCHAR(16) NOT NULL DEFAULT 'custom',
    algo_type VARCHAR(32) NOT NULL DEFAULT 'object_detection',
    description TEXT NOT NULL DEFAULT '',
    scene VARCHAR(256) NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '[]',
    default_output TEXT NOT NULL DEFAULT '[]',
    model_format VARCHAR(16) NOT NULL DEFAULT 'onnx',
    model_status VARCHAR(16) NOT NULL DEFAULT 'missing',
    model_file VARCHAR(256) NOT NULL DEFAULT '',
    model_error TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sys_vision_algorithm_code_deleted
    ON sys_vision_algorithm(algorithm_code, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_vision_algorithm_source ON sys_vision_algorithm(source);

-- 第一期内置四种算法模板（文档 5.3）。内置算法不可删除，模型待上传时状态为 missing。
INSERT INTO sys_vision_algorithm
    (algorithm_code, name, source, algo_type, description, labels, default_output, model_status)
VALUES
    ('object_detection', 'Object Detection', 'builtin', 'object_detection',
     'Detects people and common objects with labels, boxes, and confidence scores.',
     '["person"]', '["person"]', 'missing'),
    ('hard_hat', 'Helmet Detection', 'builtin', 'helmet',
     'Detects whether workers are wearing helmets and outputs violation alerts.',
     '["person","helmet","no_helmet"]', '["person","helmet","no_helmet"]', 'missing'),
    ('intrusion_detection', 'Intrusion Detection', 'builtin', 'intrusion',
     'Detects people entering a restricted ROI and outputs an intrusion alert state.',
     '["person"]', '["intrusion"]', 'missing'),
    ('line_counting', 'Line Counting', 'builtin', 'line_counting',
     'Tracks targets crossing a counting line and reports cumulative in/out counts.',
     '["person"]', '["in","out"]', 'missing')
ON CONFLICT (algorithm_code, deleted_time) DO NOTHING;
