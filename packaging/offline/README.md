# 内网离线构建环境

本目录提供「联网准备机 → 介质 → 内网构建机」流程，不把巨大的依赖缓存提交进 Git。准备机下载并生成缓存包，内网机器使用同一版本的源码、固定锁文件、离线模式和 SHA256 清单构建。
服务端依赖缓存、Ubuntu 工具链、Docker 基础镜像、Windows 原生客户端缓存是不同介质，可按需要分别制作；不要把 Windows Electron 缓存拿到 Ubuntu 使用。源码仓库也必须通过受控介质导入，缓存包本身不包含源码。

## 文件

- `prepare-connected.sh`：联网 Ubuntu 24.04 准备机下载 Go modules、Yarn/PNPM/Corepack 缓存并记录工具链。
- `prepare-ubuntu-toolchain.sh`：下载固定版本的 Go/Node Linux 工具链和离线安装脚本。
- `prepare-docker-images.sh`：联网准备机拉取服务端 Docker 构建和运行所需的基础镜像并导出 tar（webadmin 使用 Debian/glibc Node，与 Ubuntu npm 缓存平台一致）。
- `export-server-image.sh` / `import-server-image.sh`：将已构建的服务端镜像导出到介质，再在 Ubuntu 部署机校验并导入。
- `prepare-windows-connected.ps1`：联网 Windows 准备机安装依赖并预热 Yarn、Electron、electron-builder 缓存。
- `prepare-windows-toolchain.ps1`：单独下载 Node.js Windows x64 工具链及校验/安装脚本。
- `build-server-offline.sh`：内网 Ubuntu 24.04 构建 webadmin、Go 服务端和可选 Docker 镜像。
- `build-windows.ps1`：内网 Windows 原生构建机生成 unsigned Windows x64 NSIS 安装包。
- `verify-bundle.sh`：导入前校验 SHA256。
- `verify-bundle.ps1`：Windows 导入前校验 SHA256。
- `register-windows-installer.sh`：在 Unix 发布机上登记 Windows 安装包并自动生成清单。

Windows 离线缓存同时包含 Yarn 压缩包缓存、PNPM store、Electron 运行时下载缓存和 electron-builder 的 NSIS/winCodeSign 工具缓存。联网准备机需运行与目标包完全相同的安装及原生 Windows NSIS 打包；只预热依赖而没有成功产出 Setup `.exe` 时，不会生成可用离线包。

## 用法

联网准备机（Ubuntu 24.04 x86_64；缓存含平台相关构建内容）：

```bash
./packaging/offline/prepare-connected.sh /tmp/picoaide-build-bundle "$PWD"
# prepare-connected.sh 会自动生成递归 SHA256SUMS
cat /tmp/picoaide-build-bundle/toolchain.txt

# 如果内网构建机没有 Go/Node/Corepack，另外制作工具链包：
# 参数必须与 toolchain.txt 里记录的 GO_VERSION、NODE_VERSION 一致。
./packaging/offline/prepare-ubuntu-toolchain.sh /tmp/picoaide-ubuntu-toolchain 1.27.1 22.19.0 amd64
# 在交付前验证工具链包的校验、解包和版本：
/tmp/picoaide-ubuntu-toolchain/verify-toolchain.sh

# 如果内网要在本地构建 Docker 镜像，再单独制作 amd64 基础镜像包：
./packaging/offline/prepare-docker-images.sh /tmp/picoaide-docker-images linux/amd64
```

内网 Ubuntu：

```bash
./packaging/offline/verify-bundle.sh /media/picoaide-build-bundle
/media/picoaide-build-bundle/restore-offline-cache.sh "$PWD"
BUILD_IMAGE=1 DOCKER_IMAGE_BUNDLE=/media/picoaide-docker-images \
  WINDOWS_INSTALLER=/media/windows-build/PicoAide-Harness-Setup.exe \
  VERSION=2.8.3 CLIENT_VERSION=2.8.3 TAG=2.8.3 \
  ./packaging/offline/build-server-offline.sh /media/picoaide-build-bundle "$PWD"

# 构建完成后导出服务端镜像：
./packaging/offline/export-server-image.sh \
  picoaide-harness-server:2.8.3 /media/picoaide-server-image
```

`restore-offline-cache.sh` 必须在与联网准备机**完全相同的源码版本**上执行；如果源码、patch、`yarn.lock` 或 `deepseek-harness/pnpm-lock.yaml` 改过，必须重新运行联网准备流程。Yarn 4 的离线门禁使用 `--immutable-cache` 与 `YARN_ENABLE_NETWORK=0`，不会使用不存在的 `yarn --offline` 参数。

把 `/media/picoaide-server-image` 带到部署机后：

```bash
./packaging/offline/import-docker-images.sh /media/picoaide-docker-images
./packaging/offline/import-server-image.sh /media/picoaide-server-image
```

# 首次初始化没有工具链的机器（默认写入 /opt/picoaide/toolchain，不覆盖系统 PATH）：
```bash
sudo /media/picoaide-ubuntu-toolchain/install-ubuntu-toolchain.sh
source /opt/picoaide/toolchain/activate-1.27.1-22.19.0.sh
```

离线构建要求：在 Ubuntu 24.04 x86_64 构建机上准备并构建，构建机源码必须与联网准备机使用的仓库版本一致。`build-server-offline.sh` 会严格核对 Go、Node、Yarn、PNPM 版本；不一致时先安装配套工具链或重新准备缓存。若仅需给部署服务器交付，优先联网构建后导出镜像，不必在部署服务器安装 Go/Node。

Windows：

```powershell
powershell -ExecutionPolicy Bypass -File .\packaging\offline\build-windows.ps1 `
  -BundleRoot D:\picoaide-build-bundle `
  -RepoRoot D:\picoaide-harness
```

Windows 联网准备机先执行一次（该步骤会实际构建并验证一个 NSIS 安装包，再收集离线缓存）：

```powershell
powershell -ExecutionPolicy Bypass -File .\packaging\offline\prepare-windows-connected.ps1 `
  -OutputRoot D:\picoaide-build-bundle `
  -RepoRoot D:\picoaide-harness
```

内网 Windows 构建机如果没有 Node.js/Corepack，联网准备机另外生成 Node 工具链包：

```powershell
powershell -ExecutionPolicy Bypass -File .\packaging\offline\prepare-windows-toolchain.ps1 `
  -OutputRoot D:\picoaide-windows-toolchain -NodeVersion 22.19.0 -Architecture x64
```

内网 Windows 首次安装时执行安装脚本后，在同一 PowerShell 会话激活：

```powershell
powershell -ExecutionPolicy Bypass -File D:\picoaide-windows-toolchain\install-windows-toolchain.ps1
. "$env:LOCALAPPDATA\PicoAide\Toolchain\activate-picoaide-toolchain.ps1"
```

Windows 打包必须在 Windows 原生机执行；Electron 原生模块和 NSIS 不能依赖 Ubuntu 交叉编译。
Ubuntu 缓存包和 Windows 缓存包应分别制作，不能混用。
Windows 构建产生的 `*Setup*.exe` 需要通过受控介质带到 Ubuntu 构建机；镜像构建时通过 `WINDOWS_INSTALLER` 指定该文件。脚本会从文件本体生成 `CLIENT-RELEASE.json`（版本、SHA256、字节数），只包含 Windows x64 安装包。

`prepare-windows-toolchain.ps1` 同时下载 Node 官方 `SHASUMS256.txt`，在生成工具链包前校验 Node 压缩包；工具链包只接受空的 `OutputRoot`，避免把旧文件或旧校验清单混入新介质。`build-windows.ps1` 和缓存恢复脚本会检查外部 `tar.exe`、Corepack 和 Yarn/PNPM 命令的退出码。内网 Windows 构建会先离线恢复 Yarn/PNPM 依赖、构建上游 Harness 和 workspace，再执行原生 NSIS 打包及安装包校验。

如需内网 Logo/品牌配置，在隔离前先把私有渠道构建上下文一并导入；构建时设置 `CHANNEL=<渠道ID>` 和 `CHANNEL_CONTEXT=/path/to/channels-context`。未传私有渠道资产时，镜像使用通用回退内容。

`prepare-docker-images.sh` 应在独立的联网构建机/专用 Docker daemon 上运行；它会拉取固定 tag 的多架构基础镜像并导出 amd64 内容。不要在承载现有业务容器的 Docker daemon 上准备基础镜像包。

脚本默认不签名。企业代码签名证书只通过 Windows Credential Manager 或安全流水线注入，不放入缓存包或 Git。

纯内网目标机没有任何运行环境时，先交付操作系统、Docker Engine/Compose Plugin 和 CA 安装介质；本目录脚本只负责项目源码、依赖缓存、语言工具链、Docker 基础镜像和客户端构建缓存，不负责安装操作系统或 Docker 本身。Ubuntu 部署机不需要 Go/Node，只有离线构建机需要工具链。
