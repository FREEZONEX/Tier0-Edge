-- 站内信记录发信应用的平台身份。
--
-- OpenAPI 发信时 sender.id 由 SDK 自报，是平台注入容器的 docker app UUID
-- （见 logic/core/project/deploy.go 的 APP_ID 注入），既不是 Launchpad 的应用 id，
-- 也不可信。客户端拿它无法反查出应用，于是应用来源的消息只能显示通用图标与文案。
--
-- 这里存一份服务端解析出来的可信身份：sender_app_id = project_app.id。
-- 读出侧据此给出应用图标地址，客户端也能用它把应用发来的相对路径落到该应用内。
ALTER TABLE sys_notify_message_request
    ADD COLUMN IF NOT EXISTS sender_app_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sys_notify_message
    ADD COLUMN IF NOT EXISTS sender_app_id BIGINT NOT NULL DEFAULT 0;
