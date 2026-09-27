import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { randomUUID } from 'node:crypto'
import type { Context } from '@deepseek-ai/cordis'
import type { SettingsNamespace } from '@deepseek-ai/dsh-settings'
import { dshHomeSafe } from 'dsh-plugin-desktop/desktop-home'
import { ApiError, fetchJSON, gatewayFetch, normalizeServerURL } from './server-connector/auth.ts'
import type { Session } from './server-connector/config.ts'
import {
  getInstalledSkillVersion,
  installSkillArchive,
  listInstalledSkills,
  resolveSkillsDir,
  uninstallSkill,
} from './skill-install.ts'
import { MAX_ARCHIVE_BYTES } from './archive-util.ts'
import { subscribeSession } from './session-service.ts'

export interface ManagedSkillPolicy {
  name: string
  mode: 'required' | 'optional' | 'blocked'
  version: string
  revision: number
}

export interface ManagedPolicy {
  user_id: number
  revision: number
  settings: Record<string, unknown>
  skills: ManagedSkillPolicy[]
}

const MANAGED_DEVICE_FILE = 'managed-device-id'
const MANAGED_SYNC_INTERVAL_MS = 5 * 60 * 1000
const AGENT_DEFAULT_MODEL_NS = 'agent-default-model' as SettingsNamespace
const LLM_DEEPSEEK_NS = 'llm-deepseek' as SettingsNamespace

let deviceIDPromise: Promise<string> | undefined

async function deviceID(): Promise<string> {
  if (deviceIDPromise === undefined) {
    deviceIDPromise = (async () => {
      const home = dshHomeSafe()
      const file = join(home, MANAGED_DEVICE_FILE)
      try {
        const existing = (await readFile(file, 'utf8')).trim()
        if (existing.length > 0 && existing.length <= 128) return existing
      } catch { /* create below */ }
      const value = randomUUID()
      await mkdir(home, { recursive: true, mode: 0o700 })
      await writeFile(file, value, { encoding: 'utf8', mode: 0o600 })
      return value
    })()
  }
  return deviceIDPromise
}

export async function getManagedPolicy(session: Session): Promise<ManagedPolicy> {
  let data: Partial<ManagedPolicy>
  try {
    data = await fetchJSON(session.serverURL, '/api/client/v2/management/policy', { token: session.token }) as Partial<ManagedPolicy>
  } catch (cause) {
    // Older internal servers do not expose managed policy yet. Keep the
    // existing bootstrap path fully backward compatible until they upgrade.
    if (cause instanceof ApiError && cause.status === 404) {
      return { user_id: 0, revision: 0, settings: {}, skills: [] }
    }
    throw cause
  }
  return {
    user_id: typeof data.user_id === 'number' ? data.user_id : 0,
    revision: typeof data.revision === 'number' ? data.revision : 0,
    settings: data.settings !== null && typeof data.settings === 'object' ? data.settings as Record<string, unknown> : {},
    skills: Array.isArray(data.skills) ? data.skills as ManagedSkillPolicy[] : [],
  }
}

export async function reportManagedState(
  session: Session,
  policy: ManagedPolicy,
  status: 'ok' | 'error' = 'ok',
  error = '',
): Promise<void> {
  const root = resolveSkillsDir()
  const entries = await readdir(root, { withFileTypes: true }).catch(() => [])
  const inventory = entries
    .filter((entry) => entry.isDirectory() && /^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(entry.name))
    .map((entry) => ({ name: entry.name }))
  try {
    await fetchJSON(session.serverURL, '/api/client/v2/management/state', {
      token: session.token,
      method: 'POST',
      body: {
        device_id: await deviceID(),
        platform: process.platform,
        client_version: process.env.PICOAIDE_CLIENT_VERSION ?? 'unknown',
        inventory,
        applied_revision: policy.revision,
        sync_status: status,
        sync_error: error.slice(0, 500),
      },
    })
  } catch (cause) {
    if (!(cause instanceof ApiError && cause.status === 404)) throw cause
  }
}

interface SkillArchiveSource {
  path: string
  channel: 'builtin' | 'market' | 'org'
  version?: string
}

async function readArchiveResponse(response: Response): Promise<Buffer> {
  const length = response.headers.get('content-length')
  if (length !== null && Number(length) > MAX_ARCHIVE_BYTES) {
    throw new Error(`managed skill archive too large (${length} bytes)`)
  }
  if (response.body === null) {
    const body = Buffer.from(await response.arrayBuffer())
    if (body.byteLength > MAX_ARCHIVE_BYTES) throw new Error('managed skill archive too large')
    return body
  }
  const chunks: Buffer[] = []
  let total = 0
  const reader = response.body.getReader()
  try {
    while (true) {
      const next = await reader.read()
      if (next.done) break
      const chunk = Buffer.from(next.value)
      total += chunk.byteLength
      if (total > MAX_ARCHIVE_BYTES) throw new Error('managed skill archive too large')
      chunks.push(chunk)
    }
  } finally {
    reader.releaseLock()
  }
  return Buffer.concat(chunks)
}

async function downloadManagedSkill(session: Session, policy: ManagedSkillPolicy): Promise<{
  archive: Buffer
  checksum?: string | undefined
  version?: string | undefined
  channel: 'builtin' | 'market' | 'org'
}> {
  const name = encodeURIComponent(policy.name)
  const sources: SkillArchiveSource[] = [
    { path: `/api/client/v2/skills/builtin/${name}/archive`, channel: 'builtin' },
    { path: `/api/client/v2/marketplace/skills/${name}/archive`, channel: 'market' },
  ]
  if (policy.version !== '') {
    sources.push({
      path: `/api/client/v2/shared-skills/${name}/${encodeURIComponent(policy.version)}/archive`,
      channel: 'org',
      version: policy.version,
    })
  }
  let lastStatus = 404
  for (const source of sources) {
    const response = await gatewayFetch(`${normalizeServerURL(session.serverURL)}${source.path}`, {
      headers: { Authorization: `Bearer ${session.token}` },
    })
    if (response.status === 404) {
      lastStatus = response.status
      continue
    }
    if (!response.ok) {
      lastStatus = response.status
      continue
    }
    const archive = await readArchiveResponse(response)
    const headerVersion = response.headers.get('x-skill-version') ?? source.version
    return {
      archive,
      checksum: response.headers.get('x-skill-checksum') ?? undefined,
      version: headerVersion ?? undefined,
      channel: source.channel,
    }
  }
  throw new ApiError('MANAGED_SKILL_NOT_FOUND', `managed skill ${policy.name} is not available (${lastStatus})`, lastStatus)
}

/**
 * Apply the server-owned Skill policy on the local machine.
 *
 * Required entries are installed through the same verified archive path as
 * the Capability Hub. Blocked entries are removed only because the policy is
 * an explicit administrator decision; `force_managed_skills` additionally
 * removes skills outside the declared policy, making the admin list a true
 * allow-list. Optional entries are intentionally not auto-installed.
 */
export async function syncManagedSkills(session: Session, policy: ManagedPolicy): Promise<void> {
  const skillsDir = resolveSkillsDir()
  const installed = await listInstalledSkills(skillsDir)
  const installedSet = new Set(installed)
  const settings = policy.settings
  const forceManaged = settings.force_managed_skills === true
  const allowLocal = settings.allow_local_skills !== false
  const declared = new Map(policy.skills.map((skill) => [skill.name, skill]))

  for (const skill of policy.skills) {
    if (skill.mode === 'blocked') {
      if (installedSet.has(skill.name)) await uninstallSkill(skillsDir, skill.name, { overwrite: true })
      continue
    }
    if (skill.mode !== 'required') continue
    const currentVersion = await getInstalledSkillVersion(skillsDir, skill.name)
    if (installedSet.has(skill.name) && (skill.version === '' || currentVersion === skill.version)) continue
    const downloaded = await downloadManagedSkill(session, skill)
    if (downloaded.version !== undefined && skill.version !== '' && downloaded.version !== skill.version) {
      throw new Error(`managed skill ${skill.name} version mismatch: requested ${skill.version}, received ${downloaded.version}`)
    }
    await installSkillArchive({
      name: skill.name,
      archive: downloaded.archive,
      checksum: downloaded.checksum,
      skillsDir,
      version: downloaded.version ?? skill.version,
      channel: downloaded.channel,
      server: session.serverURL,
      overwrite: forceManaged,
    })
  }

  if (forceManaged || !allowLocal) {
    for (const name of installed) {
      const rule = declared.get(name)
      if (rule === undefined || rule.mode === 'blocked') {
        await uninstallSkill(skillsDir, name, { overwrite: true })
      }
    }
  }
}

export async function applyManagedSettings(ctx: Context, policy: ManagedPolicy, models: { id: string }[]): Promise<void> {
  const settings = policy.settings
  const model = typeof settings.default_model === 'string' && models.some((item) => item.id === settings.default_model)
    ? settings.default_model
    : undefined
  const effort = settings.reasoning_effort
  const reasoningEffort = effort === 'off' || effort === 'low' || effort === 'high' || effort === 'max' ? effort : undefined
  if (model !== undefined) {
    await ctx.settings.update(LLM_DEEPSEEK_NS, { models, ...(reasoningEffort === undefined ? {} : { reasoningEffort }) })
    await ctx.settings.update(AGENT_DEFAULT_MODEL_NS, { model, ...(reasoningEffort === undefined ? {} : { reasoningEffort }) })
  } else if (reasoningEffort !== undefined) {
    await ctx.settings.update(LLM_DEEPSEEK_NS, { reasoningEffort })
    await ctx.settings.update(AGENT_DEFAULT_MODEL_NS, { reasoningEffort })
  }
}

export function startManagedStateSync(
  ctx: Context,
  load: (session: Session) => Promise<void>,
): () => void {
  let timer: ReturnType<typeof setInterval> | undefined
  let epoch = 0
  const off = subscribeSession(ctx, (session: Session | null) => {
    epoch += 1
    const current = epoch
    if (timer !== undefined) clearInterval(timer)
    timer = undefined
    if (session === null) return
    const tick = (): void => {
      void load(session).catch((cause) => ctx.logger.error(cause))
    }
    tick()
    timer = setInterval(() => {
      if (current === epoch) tick()
    }, MANAGED_SYNC_INTERVAL_MS)
  })
  return () => {
    off()
    if (timer !== undefined) clearInterval(timer)
  }
}
