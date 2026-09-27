import { useEffect, useMemo, useState } from 'react'
import { request, ADMIN_API } from '../api'
import { PageHeader } from '../components/page-header'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select'
import { Switch } from '../components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'
import { PERM_MANAGED_WRITE, hasPermission } from '../lib/rbac'
import { Check, Monitor, Plus, RefreshCw, Save, Trash2 } from 'lucide-react'

interface UserItem { id: number; username: string; display_name?: string }
interface SkillPolicy { name: string; mode: 'required' | 'optional' | 'blocked'; version: string; revision: number }
interface ManagedDevice {
  device_id: string
  platform: string
  client_version: string
  inventory: Array<{ name?: string; version?: string } | string>
  applied_revision: number
  sync_status: string
  sync_error?: string
  last_seen_at: string
}
interface ManagedConfig { settings: Record<string, unknown>; skills: SkillPolicy[]; revision: number; devices: ManagedDevice[] }
interface AvailableSkill { name: string; version?: string; title?: string }

type SettingsForm = {
  default_model: string
  reasoning_effort: 'off' | 'low' | 'high' | 'max'
  allow_local_skills: boolean
  force_managed_skills: boolean
  allow_plugin_changes: boolean
}

const DEFAULT_SETTINGS: SettingsForm = {
  default_model: '',
  reasoning_effort: 'off',
  allow_local_skills: true,
  force_managed_skills: false,
  allow_plugin_changes: true,
}

function settingsToForm(settings: Record<string, unknown>): SettingsForm {
  return {
    default_model: typeof settings.default_model === 'string' ? settings.default_model : '',
    reasoning_effort: settings.reasoning_effort === 'low' || settings.reasoning_effort === 'high' || settings.reasoning_effort === 'max'
      ? settings.reasoning_effort
      : 'off',
    allow_local_skills: settings.allow_local_skills !== false,
    force_managed_skills: settings.force_managed_skills === true,
    allow_plugin_changes: settings.allow_plugin_changes !== false,
  }
}

function formToSettings(form: SettingsForm): Record<string, unknown> {
  const settings: Record<string, unknown> = {
    reasoning_effort: form.reasoning_effort,
    allow_local_skills: form.allow_local_skills,
    force_managed_skills: form.force_managed_skills,
    allow_plugin_changes: form.allow_plugin_changes,
  }
  if (form.default_model.trim() !== '') settings.default_model = form.default_model.trim()
  return settings
}

function skillLabel(skill: AvailableSkill): string {
  return skill.title && skill.title !== skill.name ? `${skill.title} (${skill.name})` : skill.name
}

export default function ManagedConfig() {
  const [users, setUsers] = useState<UserItem[]>([])
  const [userID, setUserID] = useState('')
  const [config, setConfig] = useState<ManagedConfig | null>(null)
  const [form, setForm] = useState<SettingsForm>(DEFAULT_SETTINGS)
  const [skills, setSkills] = useState<SkillPolicy[]>([])
  const [availableSkills, setAvailableSkills] = useState<AvailableSkill[]>([])
  const [newSkill, setNewSkill] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const canWrite = hasPermission(PERM_MANAGED_WRITE)

  async function loadAvailableSkills() {
    const [market, builtin] = await Promise.all([
      request<{ skills?: AvailableSkill[] }>(`${ADMIN_API}/skills`).catch(() => ({ skills: [] })),
      request<{ skills?: AvailableSkill[] }>(`${ADMIN_API}/skills/builtin`).catch(() => ({ skills: [] })),
    ])
    const merged = new Map<string, AvailableSkill>()
    for (const skill of [...(market.skills ?? []), ...(builtin.skills ?? [])]) {
      if (skill.name) merged.set(skill.name, skill)
    }
    setAvailableSkills([...merged.values()].sort((a, b) => a.name.localeCompare(b.name)))
  }

  useEffect(() => {
    void Promise.all([
      request<{ users?: UserItem[] }>(`${ADMIN_API}/users?page=1&size=200`),
      loadAvailableSkills(),
    ]).then(([data]) => {
      const nextUsers = data.users ?? []
      setUsers(nextUsers)
      if (nextUsers[0]) setUserID(String(nextUsers[0].id))
    }).catch((cause: any) => setError(cause.message ?? '读取用户失败'))
  }, [])

  async function loadConfig(id = userID) {
    if (!id) return
    setConfig(null)
    setError('')
    try {
      const data = await request<ManagedConfig>(`${ADMIN_API}/users/${id}/managed-config`)
      setConfig(data)
      setForm(settingsToForm(data.settings ?? {}))
      setSkills(data.skills ?? [])
    } catch (cause: any) {
      setError(cause.message ?? '读取托管配置失败')
    }
  }

  useEffect(() => { void loadConfig() }, [userID])

  const knownSkillNames = useMemo(() => new Set(availableSkills.map((skill) => skill.name)), [availableSkills])

  function addSkill(name = newSkill) {
    const trimmed = name.trim()
    if (!trimmed || skills.some((skill) => skill.name === trimmed)) return
    setSkills((current) => [...current, { name: trimmed, mode: 'optional', version: '', revision: 0 }])
    setNewSkill('')
  }

  function updateSkill(index: number, patch: Partial<SkillPolicy>) {
    setSkills((current) => current.map((skill, i) => i === index ? { ...skill, ...patch } : skill))
  }

  async function save() {
    if (!canWrite || !userID || busy) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const data = await request<ManagedConfig>(`${ADMIN_API}/users/${userID}/managed-config`, {
        method: 'PUT',
        body: JSON.stringify({ settings: formToSettings(form), skills: skills.map(({ name, mode, version }) => ({ name, mode, version })) }),
      })
      setConfig(data)
      setForm(settingsToForm(data.settings ?? {}))
      setSkills(data.skills ?? [])
      setNotice(`已保存第 ${data.revision} 版策略，客户端下次同步时生效。`)
    } catch (cause: any) {
      setError(cause.message ?? '保存托管配置失败')
    } finally {
      setBusy(false)
    }
  }

  return <div className="space-y-5">
    <PageHeader title="用户托管" desc="管理员可以按用户下发客户端配置、控制 Skill，并查看设备同步状态。" />
    <Card>
      <CardHeader className="pb-3"><CardTitle>选择用户</CardTitle><CardDescription>托管策略只影响客户端行为，不会读取或覆盖用户的私密凭据。</CardDescription></CardHeader>
      <CardContent>
        <Select value={userID} onValueChange={setUserID}>
          <SelectTrigger className="max-w-md"><SelectValue placeholder="选择用户" /></SelectTrigger>
          <SelectContent>{users.map((user) => <SelectItem key={user.id} value={String(user.id)}>{user.username}{user.display_name ? `（${user.display_name}）` : ''}</SelectItem>)}</SelectContent>
        </Select>
      </CardContent>
    </Card>
    {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">{error}</div>}
    {notice && <div className="flex items-center gap-2 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700"><Check className="h-4 w-4" />{notice}</div>}
    {config && <>
      <Card>
        <CardHeader className="pb-3"><CardTitle>客户端配置</CardTitle><CardDescription>这些设置会作为该用户的组织策略下发。留空默认模型表示不强制指定模型。</CardDescription></CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2"><Label htmlFor="managed-default-model">默认模型</Label><Input id="managed-default-model" value={form.default_model} onChange={(event) => setForm({ ...form, default_model: event.target.value })} disabled={!canWrite} placeholder="不指定" /></div>
          <div className="space-y-2"><Label htmlFor="managed-reasoning">思考模式</Label><Select value={form.reasoning_effort} onValueChange={(value: SettingsForm['reasoning_effort']) => setForm({ ...form, reasoning_effort: value })} disabled={!canWrite}><SelectTrigger id="managed-reasoning"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="off">关闭</SelectItem><SelectItem value="low">低</SelectItem><SelectItem value="high">高</SelectItem><SelectItem value="max">最高</SelectItem></SelectContent></Select></div>
          <PolicySwitch label="允许用户本地 Skill" checked={form.allow_local_skills} disabled={!canWrite} onCheckedChange={(value) => setForm({ ...form, allow_local_skills: value })} />
          <PolicySwitch label="强制遵守托管 Skill 策略" checked={form.force_managed_skills} disabled={!canWrite} onCheckedChange={(value) => setForm({ ...form, force_managed_skills: value })} />
          <PolicySwitch label="允许用户修改插件" checked={form.allow_plugin_changes} disabled={!canWrite} onCheckedChange={(value) => setForm({ ...form, allow_plugin_changes: value })} />
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-3"><CardTitle>Skill 策略</CardTitle><CardDescription>强制安装、允许使用或禁止使用。版本留空表示跟随服务端当前版本；也可以输入内置列表之外的 Skill 名称。</CardDescription></CardHeader>
        <CardContent className="space-y-3">
          {canWrite && <div className="flex flex-wrap gap-2"><Select value="" onValueChange={addSkill}><SelectTrigger className="min-w-[16rem] flex-1"><SelectValue placeholder="选择服务端 Skill" /></SelectTrigger><SelectContent>{availableSkills.filter((skill) => !skills.some((item) => item.name === skill.name)).map((skill) => <SelectItem key={skill.name} value={skill.name}>{skillLabel(skill)}{skill.version ? ` · ${skill.version}` : ''}</SelectItem>)}</SelectContent></Select><Input className="min-w-[12rem] flex-1" value={newSkill} onChange={(event) => setNewSkill(event.target.value)} placeholder="或输入 Skill 名称" onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); addSkill() } }} /><Button type="button" variant="outline" onClick={() => addSkill()} disabled={!newSkill.trim()}><Plus className="mr-2 h-4 w-4" />添加</Button></div>}
          {skills.length === 0 ? <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">当前没有托管 Skill 策略。</div> : <div className="overflow-x-auto rounded-md border"><Table><TableHeader><TableRow><TableHead>Skill</TableHead><TableHead className="w-44">模式</TableHead><TableHead className="w-44">版本</TableHead>{canWrite && <TableHead className="w-14" />}</TableRow></TableHeader><TableBody>{skills.map((skill, index) => <TableRow key={`${skill.name}-${index}`}><TableCell><div className="font-medium">{knownSkillNames.has(skill.name) ? (availableSkills.find((item) => item.name === skill.name)?.title || skill.name) : skill.name}</div><div className="font-mono text-xs text-muted-foreground">{skill.name}</div></TableCell><TableCell><Select value={skill.mode} onValueChange={(value: SkillPolicy['mode']) => updateSkill(index, { mode: value })} disabled={!canWrite}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="required">强制安装</SelectItem><SelectItem value="optional">允许使用</SelectItem><SelectItem value="blocked">禁止使用</SelectItem></SelectContent></Select></TableCell><TableCell><Input value={skill.version} onChange={(event) => updateSkill(index, { version: event.target.value })} disabled={!canWrite} placeholder="跟随服务端" /></TableCell>{canWrite && <TableCell><Button variant="ghost" size="icon" aria-label={`删除 ${skill.name}`} onClick={() => setSkills((current) => current.filter((_, i) => i !== index))}><Trash2 className="h-4 w-4 text-muted-foreground" /></Button></TableCell>}</TableRow>)}</TableBody></Table></div>}
          {canWrite && <Button onClick={() => void save()} disabled={busy}><Save className="mr-2 h-4 w-4" />{busy ? '保存中…' : '保存托管策略'}</Button>}
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-3 space-y-0 pb-3"><div><CardTitle>设备与安装状态</CardTitle><CardDescription>客户端定期通过普通 HTTP 上报；这里可以确认策略是否已应用以及 Skill 是否已安装。</CardDescription></div><Button variant="outline" size="sm" onClick={() => void loadConfig()}><RefreshCw className="mr-2 h-4 w-4" />刷新</Button></CardHeader>
        <CardContent>{config.devices.length === 0 ? <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无设备上报。用户启动客户端并登录后会自动出现。</div> : <div className="space-y-3">{config.devices.map((device) => <div key={device.device_id} className="rounded-md border p-3"><div className="flex flex-wrap items-center gap-2 text-sm"><Monitor className="h-4 w-4" /><span className="font-mono text-xs">{device.device_id}</span><Badge variant={device.sync_status === 'ok' ? 'secondary' : 'destructive'}>{device.sync_status === 'ok' ? '已同步' : device.sync_status}</Badge><span className="text-muted-foreground">{device.platform} · {device.client_version || '未知版本'} · 策略第 {device.applied_revision} 版</span><span className="ml-auto text-xs text-muted-foreground">{new Date(device.last_seen_at).toLocaleString()}</span></div>{device.sync_error && <div className="mt-2 text-xs text-red-600">{device.sync_error}</div>}<div className="mt-2 text-xs text-muted-foreground">已发现 Skill：{device.inventory.length === 0 ? '无' : device.inventory.map((item) => typeof item === 'string' ? item : `${item.name ?? '未命名'}${item.version ? `@${item.version}` : ''}`).join('、')}</div></div>)}</div>}</CardContent>
      </Card>
    </>}
  </div>
}

function PolicySwitch({ label, checked, disabled, onCheckedChange }: { label: string; checked: boolean; disabled: boolean; onCheckedChange: (value: boolean) => void }) {
  return <div className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"><Label className="cursor-pointer" htmlFor={`policy-${label}`}>{label}</Label><Switch id={`policy-${label}`} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} /></div>
}
