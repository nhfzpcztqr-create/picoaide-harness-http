import type { Context } from '@deepseek-ai/cordis'
import type { SettingsNamespace } from '@deepseek-ai/dsh-settings'
import { subscribeSession } from './session-service.ts'
import { getBootstrap } from './server-connector/bootstrap.ts'
import { AuthError } from './server-connector/auth.ts'
import { TOKEN_ENV } from './gateway-model.ts'
import type { Session } from './server-connector/config.ts'

/** Stable Cordis plugin name. */
export const name = 'bootstrap'

/** Services consumed: settings writes, the session being synced, and the LLM service for per-model reasoning patching. */
export const inject = ['settings', 'picoSession', 'llm']

const LLM_DEEPSEEK_NS = 'llm-deepseek' as SettingsNamespace
const AGENT_DEFAULT_MODEL_NS = 'agent-default-model' as SettingsNamespace
const WEB_SEARCH_DEEPSEEK_NS = 'web-search-deepseek' as SettingsNamespace

/** The provider route the `llm-deepseek` adapter registers (gateway repoints its base URL). */
const DEEPSEEK_PROVIDER = 'deepseek-official'

/**
 * Extract the server-configured output cap from a model's `default_params`
 * JSON (`{"max_output": N}`). The gateway injects this cap only when the
 * client omits `max_tokens`; mapping it onto the catalog model's `maxTokens`
 * makes the client send exactly the server-configured value (M5) instead of
 * its own 256k default, so the server-side cap actually applies.
 */
export function maxOutputFromDefaultParams(raw: unknown): number | undefined {
  if (typeof raw !== 'string' || raw.length === 0) return undefined
  try {
    const value = JSON.parse(raw) as { max_output?: unknown }
    if (typeof value.max_output === 'number' && Number.isFinite(value.max_output) && value.max_output > 0) {
      return Math.floor(value.max_output)
    }
    return undefined
  } catch {
    return undefined
  }
}

/**
 * Extract the context window from a model's `default_params` JSON
 * (`{"context_length": N}`). The client uses `contextWindow` to drive
 * automatic context compression and token budget estimation; without it
 * every model falls back to the 1M default, which is wrong for smaller
 * models and wastes compression headroom on large ones.
 *
 * Supports several equivalent keys to match community/legacy configs:
 * `context_length`, `context_window`, `max_context_tokens`, `max_input`,
 * `max_input_tokens`, `context`.
 */
export function contextWindowFromDefaultParams(raw: unknown): number | undefined {
  if (typeof raw !== 'string' || raw.length === 0) return undefined
  try {
    const value = JSON.parse(raw) as Record<string, unknown>
    const keys = [
      'context_length',
      'context_window',
      'max_context_tokens',
      'max_input',
      'max_input_tokens',
      'context',
    ]
    for (const k of keys) {
      const v = value[k]
      if (typeof v === 'number' && Number.isFinite(v) && v >= 1024) {
        return Math.floor(v)
      }
      if (typeof v === 'string') {
        const n = Number(v.trim())
        if (Number.isFinite(n) && n >= 1024) return Math.floor(n)
      }
    }
    return undefined
  } catch {
    return undefined
  }
}

/**
 * Validate and project the server-delivered input modalities (0058).
 * Invalid/empty values yield undefined (the llm-deepseek settings schema then
 * defaults to text-only), so a misconfigured server can never inject an
 * unknown modality that would invalidate the whole settings section.
 */
export function resolveInputModalities(raw: unknown): string[] | undefined {
  if (!Array.isArray(raw) || raw.length === 0) return undefined
  const filtered = raw.filter((m): m is string => m === 'text' || m === 'image')
  if (filtered.length === 0) return undefined
  return [...new Set(filtered)]
}

/**
 * Per-model reasoning configuration shape returned by the LLM service's
 * `resolveModelInfo`. Matches `LlmModelReasoningInfo` structurally (we avoid
 * importing the branded type from the submodule since we cannot modify it).
 */
interface ResolvedReasoning {
  efforts: Array<{ id: string; name: string; description?: string }>
  defaultEffort?: string
}

/**
 * Extract the thinking adapter mode string from a model's `default_params`
 * JSON (`{"_thinking_adapter": "..."}`).
 *
 * Returns `undefined` when no adapter is configured (legacy behaviour — the
 * adapter falls back to connection-level thinking / reasoningEffort).
 */
function thinkingAdapterFromDefaultParams(raw: unknown): string | undefined {
  if (typeof raw !== 'string' || raw.length === 0) return undefined
  try {
    const value = JSON.parse(raw) as Record<string, unknown>
    const adapter = value._thinking_adapter
    if (typeof adapter !== 'string' || adapter.length === 0) return undefined
    return adapter
  } catch {
    return undefined
  }
}

/**
 * Build a per-model reasoning configuration from an adapter mode string.
 *
 * Adapter modes and their UI presentation:
 *   - `deepseek`:   4 levels — 关闭 / 低 / 高 / 最高
 *   - `qwen`:       4 levels — 关闭 / 低 / 中 / 超高 (Qwen-style labels)
 *   - `strip_open`:2 levels — 关闭 / 模型默认 (non-off all → model default)
 *   - `strip_all`:  `null` → 模型始终走默认配置 (selector hidden)
 *
 * Returns `null` for `strip_open` (meaning "this model has no reasoning
 * capability, hide the selector"), and `undefined` for unknown / unset
 * adapters (meaning "fall back to connection-level default").
 */
function reasoningFromAdapter(adapter: string): ResolvedReasoning | null | undefined {
  switch (adapter) {
    case '__default__':
      return {
        efforts: [
          { id: 'off', name: 'off', description: '简单任务，不使用推理' },
          { id: 'low', name: 'low', description: '常规任务，优先速度' },
          { id: 'high', name: 'high', description: '平衡质量与速度' },
          { id: 'max', name: 'max', description: '复杂任务，优先质量' },
        ],
        defaultEffort: 'high',
      }
    case 'qwen':
      return {
        efforts: [
          { id: 'off', name: 'off', description: '直接生成，不进行推理' },
          { id: 'low', name: 'low', description: '效率优先，快速响应' },
          { id: 'high', name: 'medium', description: '准确性与速度平衡' },
          { id: 'max', name: 'xhigh', description: '深度推理，追求最优解' },
        ],
        defaultEffort: 'high',
      }
    case 'strip_open':
      return {
        efforts: [
          { id: 'off', name: '关闭', description: '不使用推理，直接生成' },
          { id: 'max', name: '模型默认', description: '使用模型默认推理深度' },
        ],
        defaultEffort: 'max',
      }
    case 'strip_all':
      // null = no reasoning capability → UI hides the effort selector.
      return null
    default:
      return undefined
  }
}

// ---------------------------------------------------------------------------
// Per-model thinking-adapter map & resolveModelInfo decoration.
//
// The deepseek-harness submodule cannot be modified, so we inject per-model
// reasoning configuration by monkey-patching LlmRuntime.prototype.resolveModelInfo.
// Prototype patching is used instead of ctx.llm decoration because Cordis
// service references may differ across scopes, but the prototype is shared by
// all instances. When the model catalog is built, each model's resolved info
// carries our injected reasoning, and the UI model picker renders accordingly.
// ---------------------------------------------------------------------------

/**
 * Map from model id → thinking adapter mode. Populated during bootstrap sync
 * and read by the resolveModelInfo decorator.
 */
let modelThinkingAdapterMap: Map<string, string> = new Map()

/** Guards against double-patching if the plugin is re-applied. */
let perModelReasoningPatched = false

/**
 * Install the per-model reasoning patch on the LLM runtime prototype.
 *
 * We derive the prototype from the live `ctx.llm` instance via
 * `Object.getPrototypeOf()`, which guarantees we patch the exact class copy
 * the runtime actually uses — no risk of hitting a duplicate from transitive
 * node_modules. Prototype patching affects every instance, including those
 * created later, so both the session controller and any other consumers all
 * see the per-model reasoning injection.
 *
 * Safe to call multiple times — only patches once.
 * The `llm` service is declared in inject[], so it is guaranteed available.
 */
function installPerModelReasoning(ctx: Context): void {
  if (perModelReasoningPatched) return
  perModelReasoningPatched = true

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const llm = (ctx as unknown as Record<string, unknown>).llm as any
  const proto = Object.getPrototypeOf(llm)

  const originalResolve: (
    provider: string,
    model: string,
    signal?: AbortSignal,
  ) => Promise<Record<string, unknown>> = proto.resolveModelInfo

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  proto.resolveModelInfo = async function (
    this: any,
    provider: string,
    model: string,
    signal?: AbortSignal,
  ): Promise<Record<string, unknown>> {
    const info = await originalResolve.call(this, provider, model, signal)

    const adapter = modelThinkingAdapterMap.get(model)
    if (adapter === undefined) return info

    const reasoning = reasoningFromAdapter(adapter)
    if (reasoning === undefined) return info

    if (reasoning === null) {
      // strip-open: remove reasoning entirely → the model picker hides the
      // effort selector for this model.
      const { reasoning: _, ...rest } = info
      return rest
    }

    return { ...info, reasoning }
  }

  ctx.logger?.info?.('[pico-bootstrap] per-model reasoning patch installed on llm prototype')
}

/**
 * Project a gateway session onto the DSH model settings: the gateway model
 * catalog drives the `llm-deepseek` models list, and the gateway default model
 * becomes the Agent default. Clearing the session resets both to composition
 * defaults.
 */
export function apply(ctx: Context): void {
  // Install the per-model reasoning patch on the LLM runtime prototype.
  // The `llm` service is in inject[], so it is guaranteed available here.
  installPerModelReasoning(ctx)

  const sync = async (session: Session | null): Promise<void> => {

    if (session === null) {
      await ctx.settings.replace(AGENT_DEFAULT_MODEL_NS, {})
      await ctx.settings.replace(LLM_DEEPSEEK_NS, {})
      await ctx.settings.replace(WEB_SEARCH_DEEPSEEK_NS, {})
      modelThinkingAdapterMap = new Map()
      return
    }
    try {
      const { config: cfg } = await getBootstrap(session)
      // 服务端下发的思考强度(2026-08):llm-deepseek 适配器的
      // connection.defaults.reasoningEffort 来自 settings(off|low|high|max),
      // 这是实际生效点;同时写 agent-default-model 保持 UI 展示一致。
      const reasoningEffort = cfg.web?.default_thinking_level

      // 先更新 thinking adapter 映射(装饰 resolveModelInfo 用)。
      // 必须在 settings.update 之前:settings.update 会触发
      // settings/document-updated 事件,客户端据此刷新 catalog;
      // 刷新时 resolveModelInfo 调用需要读到最新映射。
      const nextMap = new Map<string, string>()
      for (const m of cfg.models) {
        const adapter = thinkingAdapterFromDefaultParams(m.default_params)
        if (adapter !== undefined) nextMap.set(m.id, adapter)
      }
      modelThinkingAdapterMap = nextMap
      ctx.logger?.info?.(
        `[pico-bootstrap] thinking adapter map updated: ${nextMap.size} models configured ` +
        `(${Array.from(nextMap.entries()).map(([k, v]) => `${k}=${v}`).join(', ') || 'none'})`,
      )

      await ctx.settings.update(LLM_DEEPSEEK_NS, {
        models: cfg.models.map((m) => {
          const maxTokens = maxOutputFromDefaultParams(m.default_params)
          const contextWindow = contextWindowFromDefaultParams(m.default_params)
          const inputModalities = resolveInputModalities(m.input_modalities)
          return {
            id: m.id,
            name: m.display_name,
            ...maxTokens === undefined ? {} : { maxTokens },
            ...contextWindow === undefined ? {} : { contextWindow },
            // 0058:图片支持配置随模型清单下发;缺失 = 仅 text(适配器据此
            // 拒绝图片输入,与服务端「配置未下发」时旧行为一致)。
            ...inputModalities === undefined ? {} : { inputModalities },
          }
        }),
        ...reasoningEffort ? { reasoningEffort } : {},
      })
      await ctx.settings.replace(AGENT_DEFAULT_MODEL_NS, {
        provider: DEEPSEEK_PROVIDER,
        model: cfg.default_model,
        ...reasoningEffort ? { reasoningEffort } : {},
      })
      // web_search 服务端代理(0043):搜索也走网关 /v1/messages,官方 key
      // 不下发客户端。apiKeyEnv 指向网关 token(与 chat 同一凭据),baseURL
      // 指向网关路由前缀(provider 追加 /messages 即 /v1/messages),model 与
      // chat 同用服务端 default_model(该模型名由服务端 anthropic 协议
      // provider 承载,与 openai 协议 provider 可同名共存)。
      await ctx.settings.update(WEB_SEARCH_DEEPSEEK_NS, {
        apiKeyEnv: TOKEN_ENV,
        baseURL: `${session.serverURL.replace(/\/+$/, '')}/v1`,
        model: cfg.default_model,
      })
    } catch (cause) {
      // M2: a revoked/expired/disabled session must not linger. Clear it so
      // the auth-gate tripwire reloads the window into the login page.
      if (cause instanceof AuthError && cause.kind === 'auth_expired') {
        ctx.picoSession.clear()
        return
      }
      ctx.logger.error('pico bootstrap sync failed')
      ctx.logger.error(cause)
    }
  }

  // subscribeSession 而不是裸 ctx.on：`restore()` 在 SessionService 构造期就启动，
  // 完成时机与插件装载顺序无关 —— 恢复型启动下首个会话事件可能早于本插件 apply，
  // 裸订阅会整个漏掉它（2026-09-05 现场：旧会话下视觉模型缺 inputModalities、
  // 上传图片被拒，重新登录即恢复，根因就是这次同步没跑）。
  subscribeSession(ctx, (session) => { void sync(session).catch((cause) => ctx.logger.error(cause)) })
}
