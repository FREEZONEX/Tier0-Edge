# Tier0 Edge

此候选源码包含新版 UNS 的 Namespace、Collection、Record、Sample、Source/Stream、Web 工作区和 OpenAPI v2，以及 Windows AMD64、macOS ARM64、Ubuntu 24.04 AMD64 的原生安装包构建链。

## 从源码构建

准备 Git、Go 1.25.3、Node.js 22.20 或以上及 pnpm 10.13.1。依赖从公开 Go 与 npm registry 解析。构建时保留 Git checkout，包清单会记录当前提交。

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm build:web
cd ..
cd backend
go build -mod=readonly -tags ts_omit_useproxy ./...
cd ..
```

## 构建原生一键包

从仓库根目录运行。缓存可以复用，输出使用新的绝对路径。Linux AMD64 构建机也可以交叉封装 Windows AMD64 和 macOS ARM64 候选包。平台、依赖组件与产品身份可通过 `package.mjs --plan` 查询。

| 平台 | 准备缓存 | 构建参数 |
| --- | --- | --- |
| Windows AMD64 | `pwsh -File scripts/deploy/fetch-windows-native.ps1 -CacheRoot CACHE` | `--platform windows/amd64 --pnpm PNPM_ROOT` |
| macOS ARM64 | `bash scripts/deploy/fetch-macos-native.sh CACHE` | `--platform darwin/arm64` |
| Ubuntu 24.04 AMD64 | `bash scripts/deploy/fetch-linux-native.sh CACHE` | `--platform linux/amd64` |

```bash
node scripts/deploy/package.mjs --edition edge --deployment native \
  --platform linux/amd64 --cache CACHE --output NEW_OUTPUT
```

- Windows 需要 PowerShell 7、7-Zip、MinGW-w64 GCC 和启用实时防护的 Microsoft Defender。先构建 Web。用 `npm install --prefix TOOLS pnpm@10.13.1` 准备随包分发的 pnpm，再将 `TOOLS/node_modules/pnpm` 作为 `--pnpm` 参数。封装生成安装程序和校验文件。
- macOS 需要 Xcode 工具链和 Python 3。封装生成自用 DMG，正式签名和公证由发布方完成。
- Ubuntu 需要本地编译 PostgreSQL 和 TimescaleDB 的系统工具及开发库。依赖和操作见 [Linux 安装说明](deploy/linux-native/README.md)。封装生成自包含 tar.gz，解包后运行 `payload/install.sh`。
- [Windows 安装说明](deploy/windows-native/README.txt) · [macOS 安装说明](deploy/macos-native/README.txt)

## 数据兼容

本候选用于新版 UNS 新装和同系列原生包构建验证。旧版开源 UNS 的目录、Flow 和历史数据需要单独转换和核对。请使用独立的安装和数据目录验收。数据模型见 [UNS 模型](doc/uns-data-model.md)。

### 在 Linux 上封装 Windows x64 候选包

在 Linux AMD64 上准备 PowerShell 7、7-Zip、MinGW-w64 和 Wine x64。Wine 用于核验包内 Windows 二进制的身份与版本。先按上文构建 Web，再准备 Windows 依赖缓存和 pnpm 10.13.1。

```bash
(cd runtime/source-opcua && npm ci --omit=dev --bin-links=false)
(cd runtime/mqtt-native && npm ci --omit=dev --bin-links=false)
pwsh -NoProfile -File scripts/deploy/fetch-windows-native.ps1 -CacheRoot CACHE
node scripts/deploy/package.mjs --edition edge --platform windows/amd64 \
  --deployment native --cache CACHE --pnpm PNPM_ROOT --output NEW_OUTPUT
```

Wine 若未注册为 `wine` 命令，可通过 `TIER0_WINDOWS_WINE` 指定其可执行文件。交叉封装输出在 `NEW_OUTPUT/candidate/`。在 Windows 上使用 `scripts/deploy/test-windows-native-defender.ps1 -ScanPath` 分别扫描载荷和 EXE，并验证安装启动。

### 在 Linux 上封装 Apple Silicon DMG

准备 Go、Node.js、7-Zip、`rcodesign` 和带 macOS SDK 的 Swift、Clang 交叉工具链。默认命令为 `arm64-apple-macos-swiftc` 和 `arm64-apple-macos-clang`，可通过 `TIER0_MACOS_SWIFTC` 和 `TIER0_MACOS_CLANG` 指定。也可配置 `TIER0_MACOS_SDK`、`TIER0_MACOS_SWIFT_ROOT`、`TIER0_MACOS_SWIFT_RESOURCE` 和 `TIER0_MACOS_LD64` 直接使用 Linux Swift 工具链。

```bash
bash scripts/deploy/prepare-macos-cross-cache.sh CACHE
node scripts/deploy/package.mjs --edition edge --platform darwin/arm64 \
  --deployment native --cache CACHE --output NEW_OUTPUT
```

缓存脚本支持传入 SHA-256 匹配的参考 DMG。交叉封装沿用参考包的 PostgreSQL、TimescaleDB 和 Finder 素材，使用当前源码编译 Tier0 组件。将生成的 DMG 复制到 Apple Silicon，运行 `bash scripts/deploy/verify-macos-cross-dmg.sh DMG` 校验载荷和签名。

### 同步开源打包脚本

从企业源码的已审核提交同步原生打包脚本。默认检查差异，显式指定 `--write` 才在干净的候选 checkout 写入。脚本只处理列出的打包入口，业务源码与依赖升级需要单独审查。

```bash
node scripts/deploy/sync-edge-packagers.mjs /path/to/upstream REF --check
node scripts/deploy/sync-edge-packagers.mjs /path/to/upstream REF --write
```

### 候选来源

新版 UNS 基线 `ae30778be17941f2e001b71952fcdf8ff99341d7`，包含 Linux 交叉封装 Windows 安装包、原生卸载及保留数据重装支持。

Mac 打包链同步自企业基线 `a2a5eb720d3f7398cf86e9b7dc28626eb07cfd90`。
