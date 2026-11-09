-- 合并重复:schema.sql(先于版本化迁移执行)会插入 static_delimitation_area,
-- 使 V016 的改名守卫跳过,旧 intrusion_detection 残留。此处把任务引用迁移到新行,
-- 再软删旧行。全新安装两条 UPDATE 均为 no-op。
UPDATE sys_vision_task t
SET algorithm_id = s.id
FROM sys_vision_algorithm o, sys_vision_algorithm s
WHERE o.algorithm_code = 'intrusion_detection' AND o.source = 'builtin' AND o.deleted_time = 0
  AND s.algorithm_code = 'static_delimitation_area' AND s.deleted_time = 0
  AND t.algorithm_id = o.id AND t.deleted_time = 0;

UPDATE sys_vision_algorithm
SET deleted_time = (EXTRACT(EPOCH FROM now()) * 1000)::bigint
WHERE algorithm_code = 'intrusion_detection' AND source = 'builtin' AND deleted_time = 0
  AND EXISTS (
    SELECT 1 FROM sys_vision_algorithm s
    WHERE s.algorithm_code = 'static_delimitation_area' AND s.deleted_time = 0
  );
