-- 将业务结果与 HTTP 状态分开保存，兼容历史日志的空结果回退。
ALTER TABLE sys_user_oper_log ADD COLUMN IF NOT EXISTS result VARCHAR(20) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sys_user_oper_log_result_time ON sys_user_oper_log(result, created_time DESC, id DESC);

-- 分别支持专用消费者领取和有界的已完成中转记录清理。
CREATE INDEX IF NOT EXISTS idx_sys_outbox_event_pending_type_time ON sys_outbox_event(event_type, created_time, id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_sys_outbox_event_done_type_time ON sys_outbox_event(event_type, updated_time, id) WHERE status = 'done';
