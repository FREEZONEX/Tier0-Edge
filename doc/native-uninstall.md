# 原生包卸载契约

适用于 Windows x64 native、macOS ARM64 native 和 Linux AMD64 native；不处理 Docker 安装、系统 Tailscale、用户配置的 systemd/launchd 服务或云端节点记录。

- Windows 在当前用户的“已安装的应用”注册每个安装目录独立的卸载入口，并携带 `Uninstall.exe`。卸载先阻止新启动、正常停止本套进程和数据库，再按包文件清单删除程序；保留 `data/`、`state/`、`logs/`、备份和未归属于包的用户文件。路径、链接、版本、运行归属或停机检查失败时停止，不按进程名杀进程、不递归删除客户目录。升级与卸载共用安装变更锁。
- Windows 保留包清单和卸载完成标记，匹配产品/平台/数据格式且源码、业务版本相同的安装器可在原目录重装，继续使用原数据；跨版本先装回原版，再走带完整备份的更新事务。中断卸载可重复运行卸载程序。安装/升级暂存目录不得注册系统卸载入口。
- Linux 提供 `tier0 uninstall [--yes]`，以安装的普通用户执行。停机后持有现有 supervisor 文件锁，仅删除清单拥有的程序文件和本安装的桌面入口；数据目录始终保留。系统服务由配置它的管理员先停止并禁用，卸载器不自动操作 systemd。
- macOS 使用系统惯例：菜单栏“退出并停止”后，将应用移到废纸篓。独立数据目录及所选目录偏好保留；完全清理数据是单独、明确的用户操作。

验证入口：Windows 执行 `pwsh -File scripts/deploy/test-windows-native-uninstall.ps1`；Linux 执行 `node --test scripts/deploy/test-linux-native-uninstall.mjs`。覆盖路径越界、数据保留、重复清理、安装变更互斥及原目录重装保护。发行前另用真实安装包验证系统登记、运行中卸载、数据库保留与同版重装。没有 Mac/Linux 实机时，必须区分静态/隔离验证和实机验证。
