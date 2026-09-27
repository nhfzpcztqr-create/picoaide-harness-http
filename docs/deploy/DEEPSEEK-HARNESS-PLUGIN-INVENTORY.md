# DeepSeek Harness 插件库存口径

这份清单区分三件事：依赖包是否安装、Cordis profile 是否声明、启动时该行是否激活。不能把 `package.json` 的全部依赖直接当成“已启用插件”。

## 当前来源

| 层 | 真源 | 当前规模/说明 |
|---|---|---|
| 上游基础 profile | `deepseek-harness/packages/bundle/base/cordis.patch.yml` | 89 个唯一插件 ID；其中有条件启用项，且桌面覆盖层会显式禁用/替代部分行，不能简单视为 89 个都在运行 |
| Windows 桌面层 | `packages/host/desktop/cordis.patch.yml` | 6 个桌面宿主行：桌面 Shell、诊断、更新、循环通知、ASAR 文件系统、ASAR 指引 |
| 企业与产品覆盖层 | `packages/host/enterprise/cordis.patch.yml`、`packages/client/*/cordis.patch.yml`、`packages/host/*/cordis.patch.yml` | 企业登录/服务端连接、账户卡片、WASM 应用、底部菜单、连接器、浏览器、定时任务、内置记忆等 |
| 运行时 Skill | `server/skills/` | 服务端随镜像发布，客户端从能力中心按需安装；不属于 Windows 安装包内置插件 |

## 产品覆盖层

当前桌面 profile 由 `packages/host/desktop/src/profile.ts` 按顺序叠加以下能力（下列名称是 profile 插件 ID，不等同于 UI 菜单数）：

- `@picoaide/dsh-enterprise` 及其 session、gateway-model、bootstrap、error-reporting、skill-telemetry、auth-gate、channel-sync 行。
- `@picoaide/dsh-account-card`、`@picoaide/dsh-wasm-apps`、`@picoaide/dsh-foot-menu`。
- `@picoaide/dsh-wasm-apps-host`、`@picoaide/dsh-connectors`、`@picoaide/dsh-browser`、`@picoaide/dsh-cron`。
- `dsh-memory-evolve`。
- `dsh-plugin-desktop` 的 6 个桌面宿主行。

产品覆盖层的具体 ID：企业层 `picoaide-enterprise`、`picoaide-session`、`picoaide-gateway-model`、`picoaide-bootstrap`、`picoaide-error-reporting`、`picoaide-skill-telemetry`、`picoaide-auth-gate`、`picoaide-channel-sync`、`pico-skill-filesystem`、`pico-tool-skill`；其他包分别提供 `picoaide-account-card`、`picoaide-wasm-apps`、`picoaide-foot-menu`、`pico-wasm-apps-host`、`pico-connectors`、`pico-browser`、`pico-cron` 和 `dsh-memory-evolve`。桌面宿主 ID 为 `desktop-shell`、`desktop-diagnostics`、`desktop-updates`、`desktop-loop-notify`、`desktop-asar-fs`、`desktop-asar-guidance`。

## 明确禁用的上游行

桌面 patch 明确禁用以下行，不能把它们描述成“桌面已启用”：

- `fs-sandbox`：由桌面 ASAR 文件系统实现替代。
- `ui-settings-plugins`：桌面使用自己的插件管理入口。
- `hmr`：桌面不提供上游 CLI 所需的 `appReady`。
- `session-log-deepseek`：避免把会话正文、工具参数和工作区路径附加转发给模型供应商。
- `ui-sidebar-browser`：桌面使用自有浏览器入口。
- `ui-plugin-manager`：桌面使用自有 `DesktopPluginsService`。
- `office-to-pdf`：桌面当前不打包其外部 Office 引擎。

上游基础 profile 的 89 个 ID（其中少数依运行平台/profile context 条件启用，桌面覆盖层可覆盖其状态）：

```text
tool-plugin-manager, plugin-manager, timer, hmr, llm, deepseek-llm-api-extensions,
session, session-log-deepseek, typert, typert-loader, typert-gateway, session-title,
session-title-llm, user-questions, agent, plugin-package-inventory-deepseek,
agent-default-model, jobs, llm-retry, settings, credentials, llm-pi-ai,
session-persistence-jsonl, attachment-local, session-query-sqlite, session-projection,
storage, storage-json, storage-domain, session-projection-cache, session-telemetry-otel,
subprocess, sandbox, sandbox-policy, bash-sandbox, pwsh-sandbox, approval, permission,
shell-env, tool-bash, tool-pwsh, tool-jobs, fs-observation-policy, tool-fs,
tool-fs-search, agent-instructions, skill, skill-filesystem, skill-badge, tool-skill,
commands, command-feedback, goal, goal-round-driver, command-goal, plan-mode, token-meter,
compaction-basic, command-compact, subagent, subagent-spawn-in-process,
subagent-fork-in-process, tool-subagent-control, tool-subagent-list-agents,
tool-subagent, tool-subagent-fork, ptc-runtime, workflow-ptc, tool-workflow,
timeout-policy, spill-local, spill-policy, session-checkpoint-policy, tool-result-pruner,
image-offload, tool-todo, tool-goal, tool-ralph, repeat-tool-reminder, web,
web-search-deepseek, web-fetch-http, tool-web, mcp-resources, tools, system-prompt,
agent-loop, fs-sandbox, llm-deepseek
```

## 运维口径

- 修改服务端 `server/skills/`：通常只重建服务端镜像，不重打 Windows 客户端。
- 修改 `cordis.patch.yml`、桌面宿主代码、上游 Harness、Electron 配置、权限或 Logo：必须重建 Windows 安装包。
- 新增 MCP/连接器配置：优先在服务端管理端配置，密钥不进入客户端安装包。
- 桌面运行时管理仅面向已经随包声明的 bundle 启停；安装包不包含 pnpm，`install_bundle` / `remove_bundle` 不能作为生产安装流程，会明确报错。第三方代码插件必须通过构建流程纳入依赖和 profile 后重发安装包。
- 升级上游 Harness 后，先检查上述 patch 行是否仍存在，再运行 profile boot、Windows package 和运行时闭包验证；patch 无法应用时禁止发布。

## 现场导出精确清单

在已安装根依赖的源码树执行以下命令，可得到当前锁文件对应的声明行；不要手工维护第二份版本号：

```bash
printf '%s\n' '--- upstream base ---'
rg '^      name:' deepseek-harness/packages/bundle/base/cordis.patch.yml
printf '%s\n' '--- desktop layer ---'
rg '^      name:' packages/host/desktop/cordis.patch.yml
printf '%s\n' '--- product overlays ---'
rg '^[[:space:]]+name:' packages/host/enterprise/cordis.patch.yml \
  packages/client/*/cordis.patch.yml packages/host/{browser,connectors,cron,wasm-apps-host}/cordis.patch.yml \
  packages/vendor/memory-evolve/cordis.patch.yml
```
