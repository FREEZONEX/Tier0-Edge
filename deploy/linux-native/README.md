# Tier0 Edge — Linux AMD64 原生包

目标系统：Ubuntu 24.04 x86_64（glibc 2.39），普通非 root 用户，至少 2 GB 可用内存及 2 GB 可用磁盘；升级还需要一份完整数据冷备份空间。系统需有 Bash、coreutils、util-linux（flock）、tar、gzip、CA 证书，以及 `libssl3t64`、`zlib1g`、`libstdc++6`、`libgcc-s1`。桌面可选安装 `xdg-utils`。系统依赖由管理员通过发行版包管理器预装，安装器不执行 sudo、不修改防火墙。

包内含 PostgreSQL 17 / TimescaleDB、Node.js/npm/pnpm、NATS、Backend/Web、独立 Fleet Agent 及公共运行组件。无需 Docker、Redis 或 RustFS。Edition 由二进制编译绑定为 Edge，免激活，但没有中心管理能力。当前验证范围为 Ubuntu 24.04 AMD64。

## 安装

校验下载目录中的 `SHA256SUMS`，解压到安装目录之外：

```bash
sha256sum -c SHA256SUMS
tar -xzf Tier0-Edge-<完整安装包版本>-linux-amd64.tar.gz
bash payload/install.sh --install-dir "$HOME/tier0-edge/program" \
  --data-dir "$HOME/tier0-edge/data" --port-offset 1000
```

必须使用非 root 用户；程序目录必须是新目录。路径支持空格和中文。安装默认后台启动并等待 ready；各服务状态存于数据目录 `state/startup-progress.json`。若只准备程序，加 `--no-start`，这不表示安装或升级完成。可加 `--desktop` 创建本用户桌面菜单入口，启动终端显示状态；不需要图形界面即可完整安装和运行。

默认账号 `tier0`、初始密码 `tier0`，首次登录后修改。已有账号密码不重置。示例偏移 1000 时访问 `http://服务器地址:19080`；未传偏移时默认 web 18080。内部服务只监听 loopback。安装预检通过后保存端口偏移，升级必须沿用。如果新装在初始化前因端口占用而停止，可在新程序目录使用其他偏移，并继续使用原数据目录。不支持通过环境变量修改产品身份。

| 服务 | 默认端口（加安装偏移） |
| --- | --- |
| Web | 18080 |
| PostgreSQL | 15432 |
| NATS | 14222 |
| MQTT / WS | 11883 / 18083 |
| Fleet local / ingress / SOCKS | 19732 / 19443 / 19733 |

## 日常操作

```bash
"$HOME/tier0-edge/program/tier0" status
"$HOME/tier0-edge/program/tier0" logs backend
"$HOME/tier0-edge/program/tier0" stop
"$HOME/tier0-edge/program/tier0" start
```

`logs` 只显示最近 200 行，可选择 launcher/backend/postgres/nats/mqtt/opcua/modbus/executor/fleet。停止保留所有数据。重启整套实例先 stop 再 start；独立服务控制、操作查询和有界日志复用产品 RuntimeProvider。依赖服务和管理链路受保护。`run` 是前台模式，适合接入现有服务管理器。

无桌面长期运行可由管理员配置 systemd 服务，以安装用户执行绝对路径 `.../program/tier0 run`，设置 `KillMode=control-group`、`TimeoutStopSec=180`、`Restart=on-failure`。不要同时用 systemd 和 `tier0 start` 启动同一数据目录；systemd 管理时使用 `systemctl stop/start`。本包不自动创建系统账号、systemd 服务或修改登录用户的 linger 配置。

Fleet 节点完成组网后，中心可通过该节点的组网地址和短期 SSH 证书打开 Linux Bash 终端。终端以启动 Tier0 的本机账号运行，沿用该账号的系统权限。监听仅在安装实例的组网内，宿主机 OpenSSH 的配置独立管理。

## 卸载

```bash
"$HOME/tier0-edge/program/tier0" uninstall
# 自动化调用（仍保留全部持久化数据）：
"$HOME/tier0-edge/program/tier0" uninstall --yes
```

以原安装用户执行。确认后先正常停机，持有本实例运行锁，再按安装清单删除程序文件及本安装的桌面入口。数据库、App、配置、Fleet 身份、日志和备份仍保留在安装时指定的 `--data-dir` 中；程序目录中的额外用户文件也保留。路径链接、清单越界、数据库未干净关闭或实例正在重启时拒绝删除。失败时保留现场供排查。

如果自行配置了 systemd 或其他进程管理器，先由管理该服务的用户停止并禁用自动重启，再执行卸载；本包不会删除外部配置的服务、系统账号或 Docker 资源。重新安装使用新的程序目录及原 `--data-dir`，继续走版本兼容检查。彻底删除数据需要另外确认备份和实际目录后手动操作；卸载不注销云端节点。

## 升级和恢复

1. 用旧程序 `tier0 stop` 干净停机；保留旧程序和数据。不要覆盖正在运行的程序。
2. 新包解压在其他目录，运行新 `install.sh`，使用新的 `--install-dir` 和原来的 `--data-dir`，不修改端口偏移。
3. 安装器核验平台、编译身份、校验和、PostgreSQL major、数据格式、业务版本与构建时间；同一安装包版本不同源码、较旧构建、降级、跨 Edition/平台均拒绝。
4. 需要升级时，先把 data/state/backend 冷备份到数据目录 `backups/`，然后启动新版。ready 后写入 `state/install-state.json`；密码、App、UNS、文件和 Fleet 身份均从原数据继承。
5. 失败时保留 `state/upgrade-pending.json`、日志和备份，禁止删除这些标记后强行重试。数据库迁移不自动回滚。检查备份 `backup.json` 的 complete 状态，恢复到一个新数据目录，再使用匹配旧版本的程序；不要直接用旧程序打开已迁移数据。

通过上述版本和数据格式检查的旧版 Linux 原生包可以原地升级数据。Windows/macOS 数据目录及 Docker 卷需要独立的迁移流程。
