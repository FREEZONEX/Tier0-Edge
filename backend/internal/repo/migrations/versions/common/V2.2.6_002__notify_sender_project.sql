-- 站内信补记发信应用所属的项目。
--
-- V2.2.6_001 只存了 sender_app_id,而企业版按 App 取详情/预检的接口都是
-- /launchpad/projects/{projectId}/apps/{appId},少了 projectId 查不了。客户端虽然
-- 能从 Launchpad 列表按 appId 反查,但用户对该应用没权限时列表里根本没有它,
-- 于是只能显示 "App not found",而不是更准确的 "App access required"。
--
-- 与 sender_app_id 同为建信时固化的快照,0 表示非应用来源或解析不到。
ALTER TABLE sys_notify_message_request
    ADD COLUMN IF NOT EXISTS sender_project_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_notify_message
    ADD COLUMN IF NOT EXISTS sender_project_id BIGINT NOT NULL DEFAULT 0;
