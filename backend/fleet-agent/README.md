# Tier0 Fleet Agent

Fleet Agent 是随每套 Tier0 Enterprise 安装的宿主机进程。它只承载基础设施管理面，不承载、解析、转发或持久化任何产品业务域、业务 DTO 或业务 payload；UNS 和账号只是禁止示例。

源码位于 `backend/fleet-agent`，与 Backend 共用 `backend/go.mod`，但仍单独构建为 `tier0-fleet-agent` 二进制。Agent 与 Backend 共用的管理面 DTO 位于 `backend/internal/fleetprotocol`。

当前实现包含基础进程、本地状态、中心长连接和容器管理面：

- `GET /v1/health`：公开存活检查。
- `GET /v1/status`：使用 local bearer token 的状态检查。
- `GET /v1/host`：使用 local bearer token 获取 hostname、OS、架构和逻辑 CPU 等安全主机摘要。
- `GET /v1/containers`：只返回当前 installationID 的 Fleet managed 容器快照，并带 Docker Engine/API 版本。
- `GET /v1/containers/{containerID}`：返回安全筛选后的容器详情。
- `POST /v1/containers/{containerID}/logs`：返回有行数、时间和 2 MiB 上限的尾部日志。
- `POST /v1/containers/{containerID}/restart`：按 operationID 持久化幂等执行；当前安装的托管容器默认允许重启，Backend、PostgreSQL/TimescaleDB 和 Redis 永久禁止。
- `POST /v1/ha/sync`：活动节点把配置中的固定 `fileSet` 加入 rsync 队列；不接受任意路径或业务 DTO。
- 使用现有协同 Token 主动连接 Center MQTT，完成 hello、心跳、快照、断线重连和新 session 替换旧 session。
- 只接受固定的容器详情、有限日志和安全重启命令，不提供通用 Docker 代理或 shell。
- 返回 hostname、OS、架构和逻辑 CPU 数。
- 结构化日志和优雅退出。

deploy zip 已支持按 Compose project 安装独立进程级服务，并为每套 Enterprise 隔离 service、端口、配置、数据目录和 installationID：Linux 使用 systemd，Windows 使用原生 SCM service，macOS 使用当前登录用户的 LaunchAgent。打包同时生成 Linux、Windows、macOS 的 amd64/arm64 六个目标；真机验收状态以任务板为准。

Agent 在 `FLEET_AGENT_DATA_DIR` 下使用 SQLite 保存配置和 operationID 去重记录。协同 Token 复用既有 EdgeNode/CloudSync Token 格式，使用 AES-GCM 加密；Windows 的主密钥由 DPAPI 机器级保护，Linux/macOS 主密钥文件权限固定为 `0600`。local API token 只保存 SHA-256 摘要。

容器发现使用 Docker Engine API。归属安全边界同时校验以下两个 label：

- `com.tier0.fleet.managed=true`
- `com.tier0.fleet.installation-id=$TIER0_INSTALLATION_ID`

通过归属校验的容器默认展示；只有部署明确设置
`com.tier0.fleet.hidden=true` 时才从列表、详情、日志和重启入口隐藏。也就是说，
归属使用白名单，归属范围内的展示使用排除法。Compose project 只用于展示，不参与安全判断。
接口不会返回环境变量、mount host path、inspect 原始 JSON、健康检查输出或未列入允许清单的 label。

## 本地运行

```powershell
cd backend/fleet-agent
$env:FLEET_AGENT_LOCAL_TOKEN = 'local-development-token'
go run ./cmd/fleet-agent
```

检查健康：

```powershell
Invoke-RestMethod http://127.0.0.1:19731/v1/health
```

检查状态：

```powershell
$headers = @{ Authorization = 'Bearer local-development-token' }
Invoke-RestMethod http://127.0.0.1:19731/v1/status -Headers $headers
```

读取容器：

```powershell
Invoke-RestMethod http://127.0.0.1:19731/v1/containers -Headers $headers
```

日志请求中的 `since` 是 Unix 毫秒时间，最多回溯 24 小时：

```powershell
$body = @{ tail = 500; timestamps = $true; limitBytes = 2097152 } | ConvertTo-Json
Invoke-RestMethod -Method Post http://127.0.0.1:19731/v1/containers/<64位容器ID>/logs -Headers $headers -ContentType 'application/json' -Body $body
```

## 中心配置

中心页面创建节点时继续生成既有协同 Token；Token 自带节点身份、HTTP/MQTT 地址和 MQTT 凭证。分支只输入这一项，不再分别输入中心地址或 FleetNodeID。安装器应通过 stdin 调用 `configure`，不能把 Token 放在命令行参数中：

```powershell
$request = @{
  accessToken = (Get-Content -LiteralPath '.\fleet-token.secret' -Raw).Trim()
  installationID = 'installation-id-from-deploy'
  composeProjectName = 'enterprise'
} | ConvertTo-Json -Compress

$result = $request | .\tier0-fleet-agent.exe configure --data-dir 'C:\ProgramData\Tier0\FleetAgent'
```

`configure` 是增量更新：未提供的本机字段保留已有值；更新中心连接只接受 `accessToken`，Agent 从 Token 元数据解析中心地址、FleetNodeID 和 MQTT 凭证。传入 `{"desiredConnected":false}` 会保留加密存储的连接信息并停止自动重连，后续再次提交 Token 会恢复连接；只有显式传入 `{"clearConnection":true}` 才会清除已有中心连接。

首次配置未提供 `localAPIToken` 时，命令会生成高强度 token 并仅在本次响应中返回，安装器负责写入 Enterprise runtime env。后续重复配置不会自动轮换该 token，响应也不会返回协同 Token。

`FLEET_ACCESS_TOKEN` 是唯一的可选中心连接环境变量。为空时状态为 `unconfigured`；存在且能解析出完整身份与 MQTT 地址时依次进入 `connecting`、`connected` 或断线后的 `disconnected`；Token 不完整时拒绝启动。

其他变量：

- `FLEET_AGENT_HTTP_ADDR`，默认 `127.0.0.1:19731`。
- `FLEET_AGENT_LOCAL_TOKEN`，保护本地状态 API。
- `FLEET_AGENT_DATA_DIR`，SQLite、DPAPI/主密钥和 operation 去重数据目录。
- `TIER0_INSTALLATION_ID`，Docker 容器归属安全边界。
- `COMPOSE_PROJECT_NAME`，仅作为容器展示信息。

HA 安装不要求用户配置身份变量。安装器根据 Compose project 与业务 VIP
确定性生成 installation ID；每台 Agent 在本地 SQLite 自动生成并持久化
Member UUID。hostname 只用于展示。

环境变量优先于 SQLite。中心地址、节点 ID 和 MQTT 凭证只从同一个 Token 派生，避免人为拼接出不一致的连接身份。

## deploy 安装

打包后的 deploy zip 在执行安装脚本时会自动识别 OS/架构、校验 checksum，并安装当前 Enterprise 对应的进程级服务。默认端口为 `19731 + PORT_OFFSET`，也可在 `.env` 显式设置 `FLEET_AGENT_PORT`。Linux 支持 rootful/rootless Docker、非默认 bridge 和 `RUNTIME_NETWORK_NAME` 指定的自定义 network；也可用 `FLEET_AGENT_DOCKER_HOST`、`FLEET_AGENT_BIND_HOST`、`FLEET_AGENT_HOST_GATEWAY` 覆盖自动探测。

```bash
bash bin/fleet-agent.sh status
bash bin/fleet-agent.sh logs
bash bin/fleet-agent.sh restart
printf '%s\n' '{"accessToken":"<中心页面创建的协同 Token>"}' | bash bin/fleet-agent.sh connect
bash bin/fleet-agent.sh disconnect
```

协同 Token 只从 stdin 进入 `connect`，不会进入命令行参数。普通卸载保留 Agent SQLite；只有 `bash bin/uninstall.sh --purge-agent-data` 显式删除当前实例的数据。

Fleet Agent 是标准安装组件，不再提供环境变量开关。`install.sh` 和 `upgrade.sh` 都会用目标版本 Bundle 收敛当前实例的 Agent 二进制；首次安装时会自动创建宿主机服务。Agent 镜像、打包二进制或宿主机服务能力不可用时会给出警告，但不会阻断 Backend 和其余容器启动；对应的本地运行时管理能力保持不可用，后续重跑安装或升级即可补齐。Windows 安装要求管理员 PowerShell 7，macOS LaunchAgent 要求由当前登录用户执行而非 root。

## 构建

```powershell
cd backend/fleet-agent
go vet ./...
go build ./cmd/fleet-agent
```

查看版本：

```powershell
go run ./cmd/fleet-agent -version
```
