Tier0 Edge · macOS Apple Silicon ARM64

安装：打开 DMG，将 Tier0 Edge.app 拖到 Applications，再从 Applications 打开。
不要直接从 DMG 运行。所有依赖随应用携带，无需 Docker、虚拟机、Homebrew 或 License 激活。
首次启动默认使用 ~/Library/Application Support/Tier0 Edge/，也可选择其他空数据目录。
此选择会持久化。程序包与数据分离；数据库、配置、账号、App、文件、日志及 Fleet 身份都在数据目录内。

菜单栏使用网页 favicon，提供启动进度、打开网页、启动、停止、日志与退出并停止。
服务就绪后访问 http://127.0.0.1:18080 。首次账号 tier0 / tier0；修改后的密码不会被覆盖。
0.1.2 起网页服务监听 0.0.0.0:18080，局域网设备可通过 http://<Mac的局域网IP>:18080 访问，同样需要登录。
数据库、NATS、MQTT 原生适配器和 Fleet 本机管理端口仍仅监听回环地址；菜单栏继续打开本机网页。
App 页面支持 Node.js 源码 ZIP、包内 metadata.json 元数据导入以及同一入口路径前缀访问。
App 自带原生依赖须支持 macOS ARM64；安装 App 的依赖可能需要网络。

版本：业务/镜像使用 X.Y.Z，开发者显式指定时才带 -rN（从 1 开始）；安装包再加 6 位随机短串。
服务窗口和 DMG 显示完整安装包版本，如 3.0.0-a1b2c3；随机串不表示升级先后。
手动升级（支持 0.1.0～0.1.2 及 3.0.0 系列向前升级，数据格式 1、PostgreSQL 17）：
1. 在菜单栏选择“退出并停止”，确认服务停止。
2. 将新 DMG 中的 Tier0 Edge.app 拖到 Applications 替换旧版。
3. 打开新版。应用自动识别已有数据目录，检查版本、运行实例、PG_VERSION、磁盘空间和包完整性。
4. 升级前创建 data/state/backend 的离线物理备份。剩余空间须至少为持久化内容大小加 512 MiB。
5. 完成必要数据库迁移并恢复服务后才写入成功版本。原密码、App、UNS 数据与 Fleet 身份保留。

失败处理：不要清空数据库。查看 logs/launcher.log、state/startup-progress.json 和 state/upgrade-pending.json。
失败或中断会阻止自动重试。backups/<时间>-<版本>/backup.json 标记完整备份。
恢复应把完整备份的 data、state、backend 复制到新的空数据目录，使用与备份匹配的旧版应用。
停止应用后可用 defaults write cn.tier0.edge.native dataDirectory -string '/absolute/restored/path' 选择恢复目录。
保留失败目录用于诊断；不支持任意降级、Windows 原型包数据目录迁入、同一完整包号对应不同源码或 PostgreSQL 跨大版本直接启动。
同一业务版本可更新为带新短串的较新构建。历史未发布 3.0.1～3.0.3 仅对已知源码构建提供归一入口，不代表一般降级支持。
本版不提供自动在线更新。

卸载：先在菜单栏选择“退出并停止”，等待图标消失，再把 Applications 中的 Tier0 Edge.app 移到废纸篓。
关闭服务窗口只是隐藏窗口，不等于停机。包没有安装 launchd 服务或系统扩展，无需卸载 Homebrew 等组件。
应用与数据分离：卸载 .app 不删除数据库、App、账号、文件、日志、备份和 Fleet 身份。
默认数据目录为 ~/Library/Application Support/Tier0 Edge/；如果首次启动选择了其他目录，
卸载前可从服务窗口“数据目录”定位，或用 defaults read cn.tier0.edge.native dataDirectory 查询。
重新安装兼容版本后继续使用原目录，保留密码和身份；原有升级兼容检查仍生效。
如需彻底清理，先备份并确认实际数据目录，再单独移到废纸篓；不要删除其他实例的数据。
可另用 defaults delete cn.tier0.edge.native dataDirectory 清除数据目录选择，下次启动重新选择。
这些操作不会注销云端节点；云端记录需要在中心另行处理。

端口冲突会报错，不会停止宿主机已有程序。Fleet 使用独立用户态 tsnet，不修改系统 Tailscale 身份。
不含 Vision、RustFS、Redis/Garnet、Anchor、AI Agent、Notebook/.NET。
原生 MQTT 临时会话和 retained 消息不保证跨重启保留。

签名为本地 ad-hoc，未使用 Developer ID，未经过 Apple 公证。
若 Gatekeeper 阻止打开，先核对 DMG SHA-256 与来源，再通过系统设置“隐私与安全性”允许打开。
最低构建目标 macOS 13；实际兼容性以验收记录为准，未对所有 macOS 版本实测。
