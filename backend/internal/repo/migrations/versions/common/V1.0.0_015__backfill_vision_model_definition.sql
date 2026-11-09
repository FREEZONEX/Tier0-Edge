-- 回填:1) 把算法上已挂的模型文件迁成模型注册表条目并建立引用;
--       2) 为内置算法灌入声明式 definition(outputs/spatialMode/rules)。

-- 1) 已有模型文件的算法 → 每个生成一条模型记录(model_code 复用 algorithm_code)。
INSERT INTO sys_vision_model
    (model_code, name, version, source, format, file_name, storage_key, model_type, status, created_time, updated_time)
SELECT algorithm_code, name || ' Model', '1.0.0', source, model_format, model_file, model_key,
       'yolo_generic', model_status, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM sys_vision_algorithm
WHERE deleted_time = 0 AND model_key <> ''
  AND NOT EXISTS (SELECT 1 FROM sys_vision_model m WHERE m.model_code = sys_vision_algorithm.algorithm_code AND m.deleted_time = 0);

UPDATE sys_vision_algorithm a
SET model_id = m.id
FROM sys_vision_model m
WHERE a.deleted_time = 0 AND a.model_id = 0 AND m.deleted_time = 0 AND m.model_code = a.algorithm_code;

-- 2) 内置算法 definition(规则契约对齐 vision-worker postprocess enrich)。
UPDATE sys_vision_algorithm SET definition =
'{"outputs":["person_count","bounding_boxes"],"spatialMode":"zones","showPushInterval":false,"paramsSchema":{},"rules":{"metrics":[{"name":"person_count","type":"count","match":{"labels":["person"]}}]}}'
WHERE algorithm_code = 'object_detection' AND source = 'builtin' AND deleted_time = 0;

UPDATE sys_vision_algorithm SET definition =
'{"outputs":["person_count","helmet_count","no_helmet_count"],"spatialMode":"zones","showPushInterval":true,"paramsSchema":{},"rules":{"metrics":[{"name":"person_count","type":"count","match":{"labels":["person"]}},{"name":"helmet_count","type":"count","match":{"labels":["helmet","hardhat"]}},{"name":"no_helmet_count","type":"count","match":{"labels":["no_helmet","no-hardhat"]}}],"states":[{"name":"helmet_violation","when":{"type":"count_gte","match":{"labels":["no_helmet","no-hardhat"]},"value":1},"value":true,"else":false}],"alarm":{"when":{"type":"state_true","state":"helmet_violation"},"alarmType":"helmet_violation","level":"warning","message":"Helmet violation detected"}}}'
WHERE algorithm_code = 'hard_hat' AND source = 'builtin' AND deleted_time = 0;

UPDATE sys_vision_algorithm SET definition =
'{"outputs":["intrusion_count","intrusion_state","intrusion_alert"],"spatialMode":"zones","showPushInterval":true,"paramsSchema":{},"rules":{"metrics":[{"name":"person_count","type":"count","match":{"labels":["person"]}},{"name":"intrusion_count","type":"count","match":{"labels":["person"]}}],"states":[{"name":"intrusion_state","when":{"type":"count_gte","match":{"labels":["person"]},"value":1},"value":true,"else":false}],"alarm":{"when":{"type":"state_true","state":"intrusion_state"},"alarmType":"intrusion","level":"warning","message":"Intrusion detected in restricted zone"}}}'
WHERE algorithm_code = 'intrusion_detection' AND source = 'builtin' AND deleted_time = 0;

UPDATE sys_vision_algorithm SET definition =
'{"outputs":["total_count","in_count","out_count"],"spatialMode":"line","showPushInterval":true,"paramsSchema":{},"rules":{"metrics":[{"name":"person_count","type":"count","match":{"labels":["person"]}}]}}'
WHERE algorithm_code = 'line_counting' AND source = 'builtin' AND deleted_time = 0;
