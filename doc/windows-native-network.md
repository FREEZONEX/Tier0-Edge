# Windows 原生 Fleet 网络

原生包采用 Fleet Agent 内的 tsnet 用户态网络，每套安装独立持有网络身份，
不调用宿主机 Tailscale CLI、不切换宿主机 profile，不修改系统路由/DNS。
Backend 继续独占业务协议、签名认证、元数据和数据同步；Agent 只提供网络传输。

入口仍为已有连接 Token 配置接口。先通过中心 HTTPS bootstrap 验证接入凭证，
再用认证过的 control URL 与 Fleet Node ID 的 SHA256 选择独立状态目录。
目录位于安装根目录 `state/fleet-network`，继承安装器当前用户/SYSTEM 的私有 ACL。
已绑定的中心网络 ID 必须匹配保留状态；缺少状态时明确失败，不重新创建身份。
候选连接失败保留当前网络，成功持久化 Agent 配置后才切换本地代理并关闭旧网络。
不复制 MachineKey；重启从同一目录恢复。此变更不涉及数据库重置或迁移。

入站仅开放 tailnet TCP 9443 到本机 Fleet TLS 19443；TLS pin 和业务鉴权保持不变。
出站通过 127.0.0.1:19733 的 SOCKS5 进入所选网络，只接受 Tailscale 地址段 TCP。
HTTP 管理页面、数据库和普通宿主机端口不发布到 tailnet。
Windows 原生 SSH 由 Fleet Agent 在该安装的 tsnet 网络内提供，使用中心短期证书和当前 Windows 用户权限，不安装系统 OpenSSH 服务。主机密钥随安装网络状态保留；终端初始化失败时不宣称可用，也不阻断基本组网。实现与权限边界见 [原生终端](windows-native-terminal.md)。

验证顺序：构建、私有目录检查、宿主机身份不变、中心注册及双向鉴权、
Collection 定义/Record/Sample 同步、重启身份和连接恢复。结果记录在交付报告。
网络进程生命周期仍由原生启动器管理；关闭整套安装会退出组网。
原生 Agent 使用官方 `ts_omit_useproxy` 构建选项，与 Backend 一样直连中心，
不隐式继承桌面 WinHTTP 代理及其集成认证；启动器还会仅为 Fleet 子进程
过滤 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY，避免克隆 Go 默认
Transport 的取钥匙等请求仍走环境代理；不修改宿主机或其他组件的代理设置。
原生启动器为自身子进程启用 HTTPS/443 Noise 传输，避免 HTTP/80 升级探测。
当前验证包要求中心和 Headscale 可直连；仅代理可达环境尚未适配。
短暂网络故障继续使用原身份重试。在线判定必须同时满足 Running 和
`Self.Online`（已收到控制流响应），不能把进程运行状态当作控制连接状态。
周期检查只读取状态，不重复调用会清理 ServeConfig 的 `tsnet.Up`。
连续观察到控制连接断开 2 分钟后，关闭本安装的 tsnet 实例，下轮检查从
同一私有状态目录恢复；不 logout、不删除身份、不申请替代节点密钥。
控制连接正常仍不等于中心回调可达，验收必须从中心执行真实受鉴权回调。
