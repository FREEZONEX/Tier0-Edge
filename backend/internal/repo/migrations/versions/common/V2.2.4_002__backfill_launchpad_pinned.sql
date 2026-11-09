-- Launchpad 收藏统一改为置顶（Pinned），与云端 saas、移动端共用同一套 resType
-- （LaunchpadPinnedApp / LaunchpadPinnedProject，见前端 pages/launchpad/pinned-res-type.ts）。
--
-- 存量记录的 res_type 是小写的 app / project（web 端只写 app，移动端企业模式两种都写），
-- 这里各复制一份为置顶记录，created_time 沿用原值，保证升级当天用户的置顶区不会一夜清空。
-- 原记录保留不删：滚动发布期间旧版本前端仍在读它们，删除会让老客户端瞬间丢标记。
INSERT INTO sys_user_favorite (user_id, res_type, res_id, res_name, created_time)
SELECT user_id, 'LaunchpadPinnedApp', res_id, res_name, created_time
FROM sys_user_favorite
WHERE res_type = 'app'
ON CONFLICT (user_id, res_type, res_id) DO NOTHING;

INSERT INTO sys_user_favorite (user_id, res_type, res_id, res_name, created_time)
SELECT user_id, 'LaunchpadPinnedProject', res_id, res_name, created_time
FROM sys_user_favorite
WHERE res_type = 'project'
ON CONFLICT (user_id, res_type, res_id) DO NOTHING;
