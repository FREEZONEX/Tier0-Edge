-- desired_status 索引单独成一个迁移:V1.0.0_009 已提交列后,这里才能引用它
-- (同一迁移的单事务批次内看不到刚 ALTER 新增的列)。
CREATE INDEX IF NOT EXISTS idx_sys_vision_task_desired ON sys_vision_task(desired_status);
