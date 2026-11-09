-- 对齐产品文档 7.13 的算法编码:intrusion_detection → static_delimitation_area。
-- 任务引用按算法 id,不受影响;catalog 同步按新 code upsert。
UPDATE sys_vision_algorithm
SET algorithm_code = 'static_delimitation_area', name = 'Area Intrusion'
WHERE algorithm_code = 'intrusion_detection' AND source = 'builtin' AND deleted_time = 0
  AND NOT EXISTS (
    SELECT 1 FROM sys_vision_algorithm a2
    WHERE a2.algorithm_code = 'static_delimitation_area' AND a2.deleted_time = 0
  );
