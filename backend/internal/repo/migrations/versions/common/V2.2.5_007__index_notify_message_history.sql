-- Support recent-first global message history, App/source filtering, and recipient/read aggregates.
CREATE INDEX IF NOT EXISTS idx_notify_message_history
    ON sys_notify_message(created_time DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_notify_message_sender_history
    ON sys_notify_message(sender_type, sender_id, created_time DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_user_message_history
    ON sys_user_message(message_id, user_id);

CREATE INDEX IF NOT EXISTS idx_project_app_runtime_app_id
    ON project_app ((manifest_json->>'app_id'));
