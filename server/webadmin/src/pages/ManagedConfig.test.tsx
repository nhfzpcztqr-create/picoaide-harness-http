import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import ManagedConfig from './ManagedConfig'
import { request } from '../api'
import { setCurrentAdmin } from '../lib/rbac'
import { ROUTER_FUTURE } from '@/lib/router-future'

const mockRequest = vi.mocked(request)

beforeEach(() => {
  mockRequest.mockReset()
  mockRequest.mockImplementation(async (path: string, init?: RequestInit) => {
    if (path === '/api/server/admin/users?page=1&size=200') return { users: [{ id: 7, username: 'alice' }] }
    if (path === '/api/server/admin/skills') return { skills: [{ name: 'reporting', title: '报表技能', version: '1.2.0' }] }
    if (path === '/api/server/admin/skills/builtin') return { skills: [{ name: 'app-builder', title: '应用构建', version: '1.0.0' }] }
    if (path === '/api/server/admin/users/7/managed-config' && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body))
      return { user_id: 7, revision: 2, settings: body.settings, skills: body.skills.map((skill: any) => ({ ...skill, revision: 2 })), devices: [] }
    }
    if (path === '/api/server/admin/users/7/managed-config') return { user_id: 7, revision: 1, settings: {}, skills: [], devices: [] }
    return {}
  })
  setCurrentAdmin({ role: 'super_admin', permissions: ['managed:read', 'managed:write'] })
})

function renderPage() {
  return render(<MemoryRouter future={ROUTER_FUTURE}><ManagedConfig /></MemoryRouter>)
}

describe('ManagedConfig 用户托管页', () => {
  it('用表单管理配置与 Skill，并提交结构化策略', async () => {
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('当前没有托管 Skill 策略。')).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('或输入 Skill 名称'), 'reporting')
    await user.click(screen.getByRole('button', { name: '添加' }))
    expect(screen.getByText('reporting')).toBeInTheDocument()

    await user.click(screen.getByLabelText('强制遵守托管 Skill 策略'))
    await user.click(screen.getByRole('button', { name: /保存托管策略/ }))
    await waitFor(() => expect(mockRequest).toHaveBeenCalledWith(
      '/api/server/admin/users/7/managed-config',
      expect.objectContaining({
        method: 'PUT',
        body: expect.stringContaining('"force_managed_skills":true'),
      }),
    ))
    expect(await screen.findByText('已保存第 2 版策略，客户端下次同步时生效。')).toBeInTheDocument()
  })

  it('显示设备策略版本与 Skill 安装清单', async () => {
    mockRequest.mockImplementation(async (path: string) => {
      if (path === '/api/server/admin/users?page=1&size=200') return { users: [{ id: 7, username: 'alice' }] }
      if (path === '/api/server/admin/skills' || path === '/api/server/admin/skills/builtin') return { skills: [] }
      if (path === '/api/server/admin/users/7/managed-config') return {
        user_id: 7, revision: 4, settings: {}, skills: [], devices: [{
          device_id: 'device-1', platform: 'windows', client_version: '2.8.2', applied_revision: 4,
          sync_status: 'ok', last_seen_at: '2026-09-27T00:00:00Z', inventory: [{ name: 'reporting', version: '1.2.0' }],
        }],
      }
      return {}
    })
    renderPage()
    expect(await screen.findByText('device-1')).toBeInTheDocument()
    expect(screen.getByText(/策略第 4 版/)).toBeInTheDocument()
    expect(screen.getByText(/reporting@1.2.0/)).toBeInTheDocument()
  })
})
