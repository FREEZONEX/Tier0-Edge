Tier0 Enterprise — Windows 原生可行性验证包

本包不需要 Docker、WSL、Node.js 或 .NET 的全局安装。仅支持 Windows x64。
Vision 不在包内。当前是原生部署验证，不是正式版，也不是现有安装的升级包。

首次双击安装程序，默认优先选择可写且空间充足的 D 盘等非 C 固定磁盘。
可直接编辑完整安装路径或点击浏览；必须为新目录。安装窗口显示解压和各服务启动进度。
点击安装前不会创建安装目录。命令行 --target 可指定精确目录；已有目录不会被覆盖。
之后双击安装目录中的 Start.exe 启动；重复运行会显示已有的服务管理窗口。
关闭服务管理窗口只隐藏到右下角托盘，不会停止程序，也不需要保留命令行窗口。
准备就绪后自动打开 http://127.0.0.1:18080。
产品类型见 package.json 的 productEdition；业务版本为 productVersion，完整安装包版本为 packageVersion。
业务/镜像版本默认 X.Y.Z，开发者显式指定才带 -rN（从 1 开始）。安装包另加 6 位随机短串，如 3.0.0-a1b2c3。
安装器和服务窗口显示完整包版本；短串不参与升级排序，同一业务版本可以更新较新的安装器构建。
Edge 版安装即用，无需 License 激活；Enterprise 版仍需有效授权。
Edge 版可以接入中心，不包含 Cluster 中心管理授权。不能通过修改配置把 Enterprise 变为 Edge。
默认登录用户名 tier0、初始密码 tier0，首次初始化后可在页面修改密码。
state/LOGIN.txt 记录初始登录信息；重启不会覆盖已经修改的账号密码。
通过托盘或服务窗口可打开平台、查看日志、启动或停止服务。双击 Stop.cmd 也可停止服务。
托盘“退出并停止服务”停止本套进程并退出托盘；数据库和 App 数据保留在 data/、state/。
本包不注册系统服务，不设置开机自启，不修改防火墙；管理网页与中间件只绑定本机。
协同默认启用；Fleet Agent 内置独立 tsnet 用户态网络，无需另装 Tailscale。
在 Connect → Edge 填入中心接入 Token 后自动组网，不接管宿主机已有 Tailscale。
网络身份保存在 state/fleet-network，重启自动恢复；不要删除该目录来重试连接。
中心与 Headscale 必须可直连，当前包不继承桌面系统代理，也不修改宿主机代理。
独立 Fleet TLS 19443 仅监听本机，由 tailnet 9443 转发，仍须业务鉴权，不提供管理网页。
异常退出可再次运行 Start.exe；启动失败查看 logs/，不要删除数据库来绕过错误。
新版安装器选择已有目录可更新：显示当前/目标版本，先完整备份，再更新程序和检查服务。
保留 data/、state/、logs/，包含数据库、App、账号配置和云端身份；同盘需足够备份空间。
请暂停外部业务写入：启动检查失败会恢复升级前整套快照，试运行新增数据仅保留在失败目录。
更新中断时重新运行安装器并选原目录恢复。备份保留在旁边的 .backup-时间戳 目录，不自动删除。
命令行：安装器 --target 原目录 --upgrade；恢复：安装器 --target 原目录 --recover。
不支持 PostgreSQL 跨主版本、降级构建或含重解析点的安装目录。

卸载：Windows 设置 → 应用 → 已安装的应用 → Tier0 Edge / Tier0 enterprise → 卸载，
或双击安装目录的 Uninstall.exe。入口按当前用户、安装目录独立注册，无需管理员权限。
确认后正常停止本套服务和数据库，仅删除安装包清单拥有的程序文件，并移除系统卸载登记。
数据库、App、配置、Fleet 身份、日志、备份和额外用户文件保留在原目录；不会清理其他安装或云端节点。
卸载窗口会报告完成或错误。命令行 Uninstall.exe --yes 免确认并启动后台卸载，
检查 logs/uninstall.log 和 .tier0-uninstalled；以程序入口消失且系统卸载登记移除判断完成，不只看启动进程退出码。
失败时保留现场及卸载入口，排查后重试。存在未完成更新时，应先运行安装器恢复。
卸载后可用相同源码和业务版本的安装包选择原目录重装，保留账号密码、数据和节点身份。
跨版本需先装回原版本，再走带完整备份的正常更新流程；重装不会绕过版本/数据兼容检查。
如需彻底清除数据，先确认备份和实际目录，再由用户手动删除保留目录；此操作会删除数据库与 App 数据。

功能边界：
- 真实新版 UNS Backend/前端、PostgreSQL + TimescaleDB、NATS。
- 缓存默认使用进程内存适配器，不运行 Redis/Garnet，不依赖 .NET。
- 文件存储使用本地目录和原有鉴权/元数据，不安装 RustFS。
- 携带 Source Modbus、OPC UA、Stream executor 和 Fleet Agent 原生进程。
- MQTT 使用原生 Node.js/Aedes 适配器，沿用 Backend 认证和 Topic ACL。
  仅 MQTT 3.1.1；会话和 retained 数据当前不跨重启持久化。无 EMQX Dashboard。
- 左侧 App 页面支持上传便携 Node.js 源码 ZIP，安装并通过 /apps/<id>/ 运行。
  共享携带一份 Node.js、npm/pnpm；不需要用户在系统中预装。
  ZIP 中的旧 UNS/Flow/Role 声明不导入；云端 App 下载待实现。
- 不提供 Anchor、AI worker、Notebook、Vision，不运行 Docker 容器。
- 配套新版云端 Fleet 页面按安装渠道显示“服务”：查看状态和日志，启动、停止或重启允许操作的服务。
  Fleet Agent 保留管理连接；Backend 仅允许重启；数据库有活动依赖时不能单独停止。
- 已在 dev.tier0.cn 验证真实组网、中心回调、Collection 定义、Record/Sample 上传及中心读回。
- 每套安装拥有独立网络身份；关闭整套安装会退出组网，后台系统服务和开机自启仍未实现。
- 原生 Stream executor 没有容器的网络/文件系统隔离，仅供可信脚本验证。
- 具体实测通过项以随包 VALIDATION.md 为准，组件存在不等同于功能验收通过。

独立端口：18080 HTTP，15432 PG，14222 NATS，11883 MQTT，
18083 MQTT WS/管理，19732 Fleet Agent，19733 仅 tailnet 出站 SOCKS，19443 Fleet TLS。
App 子进程仅使用临时回环端口，浏览器统一通过平台路径访问。
端口占用会明确失败，不停止或修改已有服务。

本包为未签名的内部验证程序。Windows 可能显示发布者未知；文件完整性可用随附 SHA-256 校验。
