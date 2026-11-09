# Fleet Agent 开发规则

本目录是 Enterprise `backend/go.mod` 下的独立命令和进程实现。开始修改前先读：

1. `../../doc/fleet/main.md`
2. HA、文件同步或主备切换任务再读 `../../doc/fleet/cluster-deployment.md`
3. 容器管理任务再读 `../../doc/fleet/container-management.md`

## 术语和边界

- Cluster 是 Enterprise 的 License 版本，不是传统多主机集群。
- Fleet Agent 是每套 Enterprise 安装的宿主机进程，不是业务容器。
- Agent 只实现基础设施管理面，不承载、解析、转发或持久化任何产品业务域、业务 DTO 或业务 payload；UNS 和账号只是禁止示例。
- 协同 Token 必须复用既有 EdgeNode/CloudSync Token 格式；明文只展示一次是页面行为，不得另建 `fleet_` Token 或独立 credential schema。
- Agent 复用协同 Token 解析节点身份和 HTTPS 接入地址，通过认证的内部 HTTPS 协议准备 Tailscale 与 SSH；已配置 HTTPS 组网的安装不再建立 MQTT 管理连接。不得恢复自定义 WebSocket 管理协议，设备 MQTT 采集不属于此边界。
- Agent 本地 HTTP 使用独立 local token，不得复用协同 Token。

## 安全硬规则

- 不在日志、状态 API、错误详情或命令行参数中输出 Token。
- 不实现任意 Shell、任意 Docker 代理、任意容器创建或删除。
- 容器操作必须校验 managed label 和 installationID；Compose project 只用于展示。
- 通过归属校验的容器默认展示；只有显式 `com.tier0.fleet.hidden=true` 才从管理面隐藏，禁止维护按服务名称逐项放行的展示白名单。
- 未实现能力返回真实错误或真实状态，不允许 mock success。

## 代码规则

- 保持独立进程职责；除 `backend/internal/fleetprotocol` 外，不导入 Backend 业务 domain、repo、配置或权限包。
- Agent 和 Backend 的管理面 DTO、协议版本、消息类型和错误码统一维护在 `backend/internal/fleetprotocol`，不得在 Agent 内手抄第二份。
- JSON 字段使用小驼峰。
- 默认保持 `CGO_ENABLED=0` 可交叉编译。
- 默认不新增测试文件；协议、安全边界或用户明确要求时除外。
- 新依赖必须验证 linux/amd64、linux/arm64、windows/amd64、windows/arm64、darwin/amd64、darwin/arm64。

## 验证

```powershell
cd backend/fleet-agent
go vet ./...
go build ./cmd/fleet-agent
```
