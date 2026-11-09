# 主应用后端语言包规则

本目录的 `zh_CN.json` 和 `en_US.json` 是 Enterprise 主应用运行时语言包的唯一业务主源。

## 硬规则

- 新增或修改用户可见文案时，中英文两份 JSON 必须同时更新并保持相同 key 集。
- 普通登录后业务页面的词条只维护在本目录，不要同步写入前端 locale。
- 禁止把本目录语言包全量复制到 `frontend/apps/web/src/locale/`。
- 前端两份 locale JSON 是生成器维护的有限 fallback，禁止手工修改。
- 只有登录、License、403/404 等无法请求后端语言包的页面，或明确选定的公共 UI 基础文案，才能进入前端 fallback。

## 前端 fallback 扩展流程

确实需要新增前端 fallback 时：

1. 先更新本目录中英文 JSON。
2. 将 key 加入 `frontend/apps/web/scripts/i18n-fallback-keys.mjs` 的明确白名单。
3. 从仓库根执行 `node scripts/generate-i18n-fallback.mjs`。
4. 从仓库根执行 `node scripts/check-i18n.mjs`。

生成器只能按白名单提取词条，不得恢复或新增后端到前端的全量同步脚本。
