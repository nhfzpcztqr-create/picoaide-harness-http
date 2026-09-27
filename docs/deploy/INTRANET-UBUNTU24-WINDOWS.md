# 内网部署交付手册：Ubuntu 24.04 服务端 + Windows 客户端

本文针对没有公网、没有现成 HTTPS 证书、客户端只需要 Windows 的企业内网。部署目录示例使用 `/opt/picoaide`；域名统一使用 `harness.example.com` 占位，实际值只写在部署机的 `.env`，不要提交回公开仓库。

## 1. 交付边界

- **服务端**：Ubuntu 24.04 LTS，Docker Engine + Compose Plugin，PostgreSQL 与服务端容器同机。
- **客户端**：只发布 Windows x64 NSIS 安装包；客户端通过服务端获取配套版本，不要求访问外网。
- **数据**：PostgreSQL 保存业务数据；`picoaide-data/master.key` 保存 AES-GCM 主密钥，必须与数据库一起备份。
- **审计**：网关保存客户端原始 Prompt 和服务端返回给客户端的完整 Response；正文加密，流式响应按顺序分片。
- **模型**：模型密钥只在服务端；Qwen 兼容接口由 `qwen` channel 转换思考参数，内网供应商的实际 `base_url` 在管理端填写。

## 2. Ubuntu 24.04 首次部署

### 2.1 主机准备

建议最低 4 vCPU、8 GiB RAM、50 GiB 可用磁盘。WASM 应用平台如果机器内存不足 4 GiB，使用 `PICOAI_WASM_MEMORY_PROFILE=small`。

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl openssl unzip jq xz-utils
docker version
docker compose version
```

离线交付介质至少包含：版本化服务端镜像包、Docker 运行镜像包、完整源码/运维脚本包和本文。先安装 Docker Engine + Compose Plugin，再从导入的源码目录运行以下步骤导出部署文件；不要手工拼装生产部署栈：

```bash
sudo mkdir -p /opt/picoaide
sudo chown "$USER":"$(id -gn)" /opt/picoaide
cd /media/picoaide-source
./packaging/offline/verify-bundle.sh /media/picoaide-server-image-VERSION
./packaging/offline/verify-bundle.sh /media/picoaide-docker-images
./packaging/offline/import-docker-images.sh /media/picoaide-docker-images
./packaging/offline/import-server-image.sh /media/picoaide-server-image-VERSION
IMAGE=$(cat /media/picoaide-server-image-VERSION/server-image.tag)
docker run --rm -v /opt/picoaide:/out -e PICOAI_UNPACK_STACK=/out "$IMAGE"
cd /opt/picoaide
```

`PICOAI_UNPACK_STACK` 只替换导出的 `client/`、`VERSION`、Compose、Caddyfile 和 `.env.example` 固定文件；不会清除既有 `.env`、证书、数据库或数据目录。首次部署确认目录为空后，才创建 `.env`：

### 2.2 没有 HTTPS 证书：使用 Caddy 内部 CA

这是纯内网的推荐方案，不需要购买证书或公网 DNS：

```bash
cd /opt/picoaide
test ! -e .env || { echo '.env already exists; refusing to overwrite it' >&2; exit 1; }
cp -n .env.example .env
chmod 600 .env
```

`.env` 至少设置：

```dotenv
DOMAIN=harness.example.com
TLS_MODE=internal
ADMIN_USER=admin
PICOAI_ADMIN_PASSWORD=替换为长度至少10位的随机密码
PG_PASSWORD=替换为随机数据库密码
PICOAI_PUBLIC_BASE_URL=https://harness.example.com
TZ=Asia/Shanghai
```

若主机内存小于约 4 GiB，在同一 `.env` 中额外设置 `PICOAI_WASM_MEMORY_PROFILE=small`；否则启动自检可能因 WASM 平台预估内存超过安全水位而拒绝启动。建议部署机至少 8 GiB RAM，且给 PostgreSQL 与系统预留余量。

启动并检查：

```bash
docker compose up -d
docker compose ps
curl -kfsS https://harness.example.com/healthz
```

导出 Caddy 根证书，交给 Windows 客户端安装：

```bash
mkdir -p certs
docker compose cp caddy:/data/caddy/pki/authorities/local/root.crt certs/harness-root.crt
sha256sum certs/harness-root.crt
```

Windows 管理员安装：

```powershell
certutil.exe -addstore -f Root .\harness-root.crt
```

通过独立可信渠道核对上一步输出的 SHA256，再分发根证书；根 CA 证书一旦被 Windows 信任，可验证该 CA 签发的证书，不能把来源不明的根证书加入系统信任库。客户端以 HTTPS 连接时不再使用 `-k` 忽略验证。

若企业已有内部 CA，改用 `TLS_MODE=manual`，把证书链放入 `certs/server.crt`、私钥放入 `certs/server.key`，再执行 `docker compose restart caddy`。不要把私钥提交到 Git。

### 2.3 明确使用纯 HTTP（不推荐）

如果内网已经有物理或网络隔离，并且你明确接受明文传输风险，可以关闭 HTTPS：

```dotenv
DOMAIN=harness.example.com
TLS_MODE=http
PICOAI_PUBLIC_BASE_URL=http://harness.example.com
```

启动时必须同时使用 HTTP Compose 覆盖文件；它会移除 443 端口映射，并使用不会自动启用 HTTPS 的 Caddy 配置：

```bash
docker compose -f docker-compose.yml -f docker-compose.http.yml up -d
docker compose -f docker-compose.yml -f docker-compose.http.yml ps
curl -fsS http://harness.example.com/healthz
```

纯 HTTP 的 Caddy 监听所有 Host，因此客户端可以使用服务器 IP、内网 DNS 名称或 hosts
别名访问；但 `PICOAI_PUBLIC_BASE_URL` 应填写员工实际使用的那个地址，这样客户端更新清单
和下载地址才会稳定。不要把 `http://` 地址写成 `https://`，否则客户端会再次遇到证书错误。

HTTP 会明文传输登录凭据、Token、Prompt、Response、审计内容和管理操作。只建议用于测试或有明确隔离措施的内网；如果只是没有公网证书，优先使用上一节的 `TLS_MODE=internal`。

## 3. 升级 DeepSeek Harness、服务端和客户端

### 3.1 是否需要重新打包 Windows 客户端

| 改动 | 是否重新打包 Windows 客户端 |
|---|---|
| 只改服务端路由、计费、审计清理、模型供应商配置 | **不需要**；只构建服务端镜像 |
| 改 `deepseek-harness/` 上游代码或根目录 patch | **需要**；客户端 bundle 可能变化 |
| 改桌面客户端、插件、权限、logo、品牌配置、Electron 配置 | **需要** |
| 只新增服务端 Skill/商城归档 | **不需要**；上传/授权后客户端刷新即可 |
| 改客户端—服务端协议或 WASM 接口 | **需要**，服务端与客户端必须成套发布 |

原则是：**客户端安装包随服务端镜像发布，但不是每个服务端提交都必须生成客户端**。

### 3.2 升级流程

1. 在联网构建机更新源码、`deepseek-harness` 子模块、Yarn/PNPM 锁文件和 patch。
2. 执行服务端定向测试、Linux amd64 编译、Windows 原生打包验证。
3. 生成带版本号的镜像包、Windows 安装包、`CLIENT-RELEASE.json` 和 SHA256 清单。
4. 将交付包通过受控介质带入内网，校验 SHA256，先备份，再导入带唯一版本标签的新镜像。
5. 只更新 `.env` 中的 `SERVER_IMAGE`，不要用新包覆盖现有 `.env`、证书或数据目录；保留旧镜像直到观察期结束。升级通常不需要再次导入 Caddy/PostgreSQL 运行镜像。

```bash
cd /opt/picoaide
STAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p deploy-backup
cp -a .env docker-compose.yml Caddyfile.* deploy-backup/
OLD_IMAGE=$(docker inspect "$(docker compose ps -q server)" --format '{{.Config.Image}}')
printf '%s\n' "$OLD_IMAGE" > "deploy-backup/server-image-$STAMP.txt"
docker compose exec -T postgres pg_dump -U picoaide -d picoaide > "deploy-backup/picoaide-$STAMP.sql"
test -s "deploy-backup/picoaide-$STAMP.sql"
docker compose stop server
tar -czf "deploy-backup/picoaide-data-$STAMP.tgz" picoaide-data
sha256sum "deploy-backup/picoaide-$STAMP.sql" "deploy-backup/picoaide-data-$STAMP.tgz" > "deploy-backup/SHA256SUMS-$STAMP"
cd /media/picoaide-source
./packaging/offline/import-server-image.sh /media/picoaide-server-image-VERSION
cd /opt/picoaide
NEW_IMAGE=$(cat /media/picoaide-server-image-VERSION/server-image.tag)
docker image inspect "$NEW_IMAGE" >/dev/null
if grep -q '^SERVER_IMAGE=' .env; then
  sed -i "s|^SERVER_IMAGE=.*|SERVER_IMAGE=$NEW_IMAGE|" .env
else
  printf '\nSERVER_IMAGE=%s\n' "$NEW_IMAGE" >> .env
fi
docker compose up -d --no-deps server
docker compose ps
curl -kfsS https://harness.example.com/healthz
```

不要将离线交付包中的示例 `.env` 拷贝到已运行部署目录；只按变更说明更新 `SERVER_IMAGE` 等明确需要修改的值。部署文件升级时逐项比较并手工合并，保留现有域名、密码、TLS_MODE 和挂载数据。

### 3.3 回滚

镜像回滚只适用于新版本尚未执行数据库迁移，或迁移明确向后兼容的情况。记录的 `OLD_IMAGE` 是完整镜像引用（含仓库前缀/tag），`.env` 中 `SERVER_IMAGE` 若不存在需追加，不能依赖只替换已有行。若新版本已执行不兼容迁移，优先发布前向修复版本；确需回到旧版时，必须在维护窗口恢复与该旧版匹配的 PostgreSQL dump 和 `picoaide-data` 备份。恢复数据库会覆盖当前数据，应由数据库管理员按预演过的恢复流程执行，不能只换镜像。

```bash
# 仅限数据库迁移兼容的镜像回滚：将下方 tag 替换成备份记录里的旧 tag
OLD_IMAGE=$(cat deploy-backup/server-image-TIMESTAMP.txt)
if grep -q '^SERVER_IMAGE=' .env; then
  sed -i "s|^SERVER_IMAGE=.*|SERVER_IMAGE=$OLD_IMAGE|" .env
else
  printf '\nSERVER_IMAGE=%s\n' "$OLD_IMAGE" >> .env
fi
docker compose up -d --no-deps server
docker compose ps
curl -kfsS https://harness.example.com/healthz
```

## 4. 插件、Skill 和连接器

### 4.1 当前内置客户端插件边界

桌面安装包内置的是编译进 profile 的插件，不是运行时从服务器下载的任意代码。当前产品层主要包含：

- **上游核心**：Cordis Loader、LLM、会话持久化/查询/统计、工作区、工具、权限审批、计划模式、子代理、终端、沙箱、设置、Web/HTTP Fetch、MCP。
- **Windows 主机能力**：本地子进程、PowerShell、本地命令、Windows ACL 沙箱、目录选择器、主机插件清单、Electron 宿主。
- **PicoAide 桌面层**：企业登录与服务端连接、连接器、浏览器、定时任务、能力中心、账户卡片、底部功能栏、WASM 应用及宿主桥接、错误上报和更新检查。
- **界面与品牌**：客户端 store、主题、品牌、文档预览和桌面菜单。

精确库存、激活/禁用口径见 [`DEEPSEEK-HARNESS-PLUGIN-INVENTORY.md`](DEEPSEEK-HARNESS-PLUGIN-INVENTORY.md)；具体版本以 `packages/host/desktop/package.json` 和锁文件为准。

### 4.2 安装新能力

- **Skill/Agent**：管理员在 webadmin 的能力中心上传归档、审核、授权；员工客户端从 marketplace 或共享能力接口安装。通常不需要重新打包客户端。
- **MCP/连接器**：在服务端管理端配置，密钥留在服务端，按用户/部门授权。
- **桌面插件**：修改 workspace 或 `deepseek-harness` profile/patch 后重新安装依赖、构建、运行测试并重新生成 Windows 安装包。不能在生产客户端目录直接替换 JS 文件。
- **桌面插件管理边界**：客户端可列举已随包声明的 bundle，并对允许管理的 bundle 做启用/禁用；安装包不携带 pnpm/包管理器。因此运行时 `plugin_manager install_bundle` / `remove_bundle` 不能作为内网插件安装流程，调用会明确报错。新增或升级桌面插件必须在构建环境纳入依赖、锁文件和 profile，再重打包、验签并发布 Windows 安装包。
- **上游插件升级**：先更新上游子模块，再检查根目录 patch 是否仍能应用；patch 失败时禁止继续打包。

## 5. Prompt/Response 全文审计

- 覆盖 `/v1` 和根路径的 Chat Completions、Completions、Responses、Anthropic Messages、Embeddings。
- 保存客户端原始请求体；响应按实际写入顺序保存加密分片，兼容 SSE 流式输出。
- 单次 Prompt 上限 64 MiB，单次 Response 审计上限 128 MiB；超过上限会标记为 `write_failed`，不把部分正文伪装成完整审计。
- 每次网关请求响应都会返回 `X-Request-ID`，可与管理端审计记录的请求 ID 对照。
- 审计记录创建失败时 fail-closed，请求不转发到上游。
- 管理员从「审计日志 → LLM Prompt / Response 审计」查看元数据、全文和 JSON 导出。
- `audit.retention_days` 管理传统操作日志；`llm.transcript_retention_days` 管理 Prompt/Response，默认 180 天，调度器周期清理，外键级联删除分片。
- 使用 `/data/master.key` 与现有 AES-GCM 机制；备份数据库时必须同步备份该文件。

## 6. Qwen 思考模式

服务端接受客户端常见的 DeepSeek 风格：

```json
{"thinking":{"type":"enabled"}}
```

并转换为 Qwen OpenAI 兼容裸 HTTP 请求的顶层字段形式：

```json
{"enable_thinking":true}
```

关闭时发送 `enable_thinking:false`；配置的 `thinking_budget` 也作为顶层字段发送。客户端传入的 SDK 专用 `extra_body` 会被展开到请求 JSON 顶层，不把 SDK 包装对象原样转发。顶层 `thinking`、`reasoning_effort` 不会泄漏给 Qwen。

内网供应商可能有二次方言差异，验收时提供开启/关闭请求、非流式响应、流式 SSE 响应四份样例。若供应商不使用上述协议，只改 `server/internal/llmgateway/channels/qwen.go` 的转换器和测试，不要在桌面端散落供应商判断。

该协议形态适用于遵循百炼 OpenAI 兼容 API 的供应商。供应商地址在 webadmin 的 Provider `base_url` 填写；模型默认参数示例：

```json
{"thinking":{"enabled":true,"budget":2048},"max_output":8192}
```

## 7. Logo 和品牌

不要在源码里搜索替换文字或手工复制图标。品牌真源是渠道私有配置注入点：准备 SVG 主标、深色版、应用图标和 favicon，放入私有渠道构建上下文，运行桌面包的 `brand-prepare`，再跑 `verify:channel`、`verify:profile` 和 Windows 安装包验证。品牌改动必须重新打包客户端。

公开仓库只保留官方占位品牌，不要把企业名称、内网域名、IP 或证书写入仓库。

## 8. 内网验收清单

```text
[ ] Windows 客户端能安装并连接内网地址
[ ] Windows 已信任内部 CA，客户端不再出现证书错误
[ ] 管理员可配置 Provider、模型和默认 thinking 参数
[ ] Qwen 思考开/关、非流式/流式均通过真实内网供应商验收
[ ] 普通用户无权查看 Prompt/Response 详情
[ ] 审计列表、全文详情、JSON 导出可用
[ ] 重启后 master.key 不变、历史密文可解密
[ ] pg_dump 与 picoaide-data 恢复演练通过
[ ] 升级后旧 Windows 客户端仍能完成普通对话
[ ] 客户端协议变化时已成套升级服务端和 Windows 安装包
```

## 上游多 API Key 动态轮换

当同一个内网模型供应商存在多个会限额的 API Key 时，不要把 Key 用逗号拼接到旧的 `api_key` 字段中。升级到包含迁移 `0083_gateway_provider_api_keys.sql` 的服务端后，在管理端为同一个 Provider 添加多把 Key；服务端保存 AES-GCM 加密密文，前端只显示掩码。

- 同一 Provider 的启用 Key 按优先级、轮询顺序选择；旧 Provider 的 `api_key_enc` 保留为兼容回退。
- `429` 按 `Retry-After` 或指数退避进入冷却，最长 30 分钟；`401/403` 默认冷却 10 分钟；网络错误/5xx 使用短冷却。
- 参数错误等普通 `4xx` 不会盲目换 Key；流式响应一旦开始发送，绝不在中途换 Key，避免重复计费和响应串线。
- 管理员可以查看标签、启用状态、优先级、失败次数、冷却时间，并重置单把 Key 的冷却状态；明文 Key 不进入审计日志或响应。
- Key 池状态在数据库中持久化；备份时必须同时备份 PostgreSQL 和 `picoaide-data/master.key`，否则密文无法恢复。

示例管理 API（需要管理员会话、CSRF 和 `gateway:read/write` 权限）：

```text
GET    /api/server/admin/providers/:id/keys
POST   /api/server/admin/providers/:id/keys
PUT    /api/server/admin/providers/:id/keys/:key_id
DELETE /api/server/admin/providers/:id/keys/:key_id
POST   /api/server/admin/providers/:id/keys/:key_id/reset
```

请求只在新增或更换时提交明文 `api_key`；查询结果永远是 `***`。建议使用不带真实域名和真实密钥的备份演练环境验证轮换、冷却、恢复和回滚。

## Mac + Windows 双客户端本机联调

本开发机是 macOS Apple Silicon，适合直接运行 Mac 客户端并进行内网服务联调；Windows 客户端仍必须在原生 Windows x64 环境打包。仓库不建议在 Apple Silicon Mac 上依赖交叉编译生成 Windows 安装包，因为 Electron 原生模块、PowerShell 沙箱和 NSIS 验证必须在 Windows 原生环境完成。

### Mac 本机测试

```bash
corepack yarn desktop:mac --no-prebuild --no-gates
open packages/host/desktop/dist/mac-smoke/*.dmg
```

如果还没有构建依赖产物，去掉 `--no-prebuild --no-gates`，首次构建会先准备 workspace 依赖并执行 Mac 打包门禁。Mac 运行时把服务端地址配置成内网服务地址；生产建议使用 `https://` 或企业内部 CA，纯 HTTP 测试则使用 `http://`。

### Windows 安装包

在 Windows x64 原生机或 Windows CI 中执行：

```powershell
corepack yarn desktop:win --no-prebuild --no-gates
```

离线内网构建使用 `packaging/offline/build-windows.ps1`；生成的 `*Setup*.exe` 再通过受控介质交给 Ubuntu 服务端镜像构建流程。Windows 与 Mac 的代码、协议和 UI 修改来自同一源码，但安装包必须分别构建和验证。

### 双端修改规则

- React/UI、服务端 API、认证、Prompt/Response 审计、Qwen 参数转换等共享代码修改后，先在 Mac 本机验证，再在 Windows 原生机重新生成 Windows 安装包。
- Electron 主进程、窗口、快捷键、文件路径、终端、Windows 沙箱、原生模块相关修改，必须分别跑 Mac 和 Windows 专项测试，不能仅凭 Mac 通过判定 Windows 可用。
- 只改服务端并重新部署 Ubuntu 镜像时，已有 Mac/Windows 客户端通常不需要重打包；只有客户端协议、内置插件、权限、Logo、Electron 配置或安装包内容变化时才需要重新打包。
- 每轮客户端构建都生成版本号、SHA256 和目标平台记录；Mac DMG 与 Windows EXE 不共用校验文件。

### 本机测试反馈流程

1. Mac 客户端连接内网 Ubuntu 服务端。
2. 记录客户端版本、服务端版本、系统版本、请求模型、是否流式、是否开启思考模式。
3. 保存客户端错误日志和服务端对应时间段日志；Prompt/Response 全文审计只在管理员审计权限下导出。
4. 把问题按“共享逻辑 / Mac 专属 / Windows 专属 / 服务端”分类反馈。
5. 修复共享代码后同时跑 Mac 与 Windows 构建；修复平台专属代码时只改对应平台分支，但仍做另一平台回归。

## 纯内网首次导入前的硬性边界

这套交付物**不包含 Ubuntu 操作系统、Docker Engine、Docker Compose Plugin、Windows 操作系统或企业 CA 的安装程序**。如果目标机“完全没有任何环境”，必须由企业受控介质另外提供并验证：

- Ubuntu 24.04 LTS x86_64 安装介质和安全更新包；
- Docker Engine 与 Docker Compose Plugin 的离线 `.deb` 包及依赖；
- Windows x64 的受控安装环境或 Windows 原生构建机；
- 企业内部 CA 根证书分发渠道；
- 目标内网 DNS 或 hosts 记录，使 `DOMAIN` 在客户端解析到 Ubuntu 服务器地址。

服务端首次启动前，在 Ubuntu 上执行：

```bash
./packaging/offline/preflight-intranet.sh /opt/picoaide
```

不要把 `curl -k` 当成客户端验收方式；它只适用于服务端初始探活。正式客户端必须信任内部 CA。

### Mac 信任内部 CA

从 Caddy 容器导出 `harness-root.crt` 后，通过受控介质复制到 Mac，核对 SHA256，再执行：

```bash
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain \
  ./harness-root.crt
```

如果企业不允许修改系统钥匙串，则将根证书导入当前用户登录钥匙串，并在“钥匙串访问”中设置为始终信任。Windows 和 Mac 必须分别导入同一个经过 SHA256 核验的根证书；不要把 Caddy 私钥分发到客户端。

### 纯内网更新策略

客户端更新清单默认可能指向公网更新地址。纯内网环境应在 `.env` 中显式关闭：

```dotenv
PICOAI_UPDATE_ENDPOINT=off
```

如果要启用内网升级，必须把新版客户端安装包重新放入服务端镜像的 `/opt/picoaide/client`，生成新的 `CLIENT-RELEASE.json`，并保证 `PICOAI_PUBLIC_BASE_URL` 是客户端可访问的绝对 `http://` 或 `https://` 地址。HTTP 模式下客户端会接受 HTTP 更新地址，但下载完整性仍依赖 `SHA256`，传输机密性不受保护。

### 内网必须建立的运维资产

正式导入前应另外建立：

- 版本包、镜像包、源码包、客户端 EXE/DMG 的 SHA256 清单；
- PostgreSQL dump 与 `picoaide-data/master.key` 的加密备份；
- 内部 CA 根证书及其指纹记录；
- 当前服务端镜像 tag、客户端版本、数据库迁移版本 `0083`；
- 一套不含真实业务 Prompt、Response 和 API Key 的恢复演练数据；
- Windows、Mac、Ubuntu 三端的时间同步和日志保留策略。
