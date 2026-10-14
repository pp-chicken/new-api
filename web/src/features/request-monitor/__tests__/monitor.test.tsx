/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { RequestMonitor } from '..'
import type { MonitoredRequest } from '../api'

const request: MonitoredRequest = {
  id: 'request-123',
  started_at: '2026-10-12T08:00:00Z',
  duration_ms: 1234,
  method: 'POST',
  path: '/v1/chat/completions',
  model: 'test-model',
  user_id: 1,
  channel_id: 2,
  status: 200,
  bytes: 42,
  state: 'failed',
  client_error: '',
  stream_end: 'scanner_error',
  stream_outcome: '',
  truncated: false,
  upstreams: [
    {
      channel_id: 2,
      host: 'upstream.example',
      method: 'POST',
      started_at: '2026-10-12T08:00:00Z',
      headers_ms: 120,
      first_byte_ms: 119,
      duration_ms: 1200,
      status: 200,
      protocol: 'HTTP/2.0',
      request_id: 'upstream-123',
      reused: true,
      bytes: 42,
      end: 'unexpected_eof',
    },
  ],
  events: [],
}

const clients: QueryClient[] = []
function renderMonitor() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <RequestMonitor />
    </QueryClientProvider>
  )
}
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

it('筛选请求并打开 HTTP 200 但流中断的详情', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/performance/requests'
          ? { items: [request], capacity: 500, instance: 'node-1' }
          : request,
    },
  }))
  const user = userEvent.setup()
  renderMonitor()
  await user.type(
    screen.getByRole('textbox', { name: 'Search request ID, model or path' }),
    'test-model'
  )
  await user.click(screen.getByRole('button', { name: 'Search' }))
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Request status' }),
    'failed'
  )
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/performance/requests', {
      params: { q: 'test-model', state: 'failed' },
    })
  )
  await user.click(await screen.findByRole('button', { name: 'request-123' }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('unexpected_eof')).toBeVisible()
  expect(within(dialog).getByText('Failed')).toBeVisible()
  expect(within(dialog).getByText('scanner_error / —')).toBeVisible()
  expect(
    within(dialog).getByRole('button', { name: /Copy diagnostic JSON/ })
  ).toBeEnabled()
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

it('空列表显示采集范围和空状态，并允许暂停及恢复刷新', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [], capacity: 500, instance: 'node-1' },
    },
  })
  const user = userEvent.setup()
  renderMonitor()
  expect(
    await screen.findByText(
      'No matching requests. Send an API request or change the filters.'
    )
  ).toBeVisible()
  expect(screen.getByText(/Records are cleared on restart/)).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Pause refresh' }))
  expect(screen.getByRole('button', { name: 'Resume refresh' })).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Resume refresh' }))
  expect(screen.getByRole('button', { name: 'Pause refresh' })).toBeVisible()
})

it('业务错误显示错误状态而不是空列表，重试后恢复', async () => {
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({ data: { success: false, message: 'Unavailable' } })
    .mockResolvedValue({
      data: {
        success: true,
        data: { items: [], capacity: 500, instance: 'node-1' },
      },
    })
  const user = userEvent.setup()
  renderMonitor()
  await user.click(await screen.findByRole('button', { name: 'Retry' }))
  expect(
    await screen.findByText(
      'No matching requests. Send an API request or change the filters.'
    )
  ).toBeVisible()
})

it('详情已过期时显示说明且不展示之前的请求内容', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url !== '/api/performance/requests') {
      throw Object.assign(new Error('Not Found'), { response: { status: 404 } })
    }
    return {
      data: {
        success: true,
        data: { items: [request], capacity: 500, instance: 'node-1' },
      },
    }
  })
  const user = userEvent.setup()
  renderMonitor()
  await user.click(await screen.findByRole('button', { name: 'request-123' }))
  const dialog = await screen.findByRole('dialog')
  expect(
    await within(dialog).findByText(
      'Request record expired or belongs to another instance'
    )
  ).toBeVisible()
  expect(within(dialog).queryByText('upstream.example')).not.toBeInTheDocument()
})

it('连接保持时展示独立的已完成生成，并可跳转详情和返回父连接', async () => {
  const child: MonitoredRequest = {
    ...request,
    id: 'connection-1-ws-0',
    parent_id: 'connection-1',
    kind: 'ws_request',
    state: 'completed',
    stream_end: 'done',
    stream_outcome: 'completed',
  }
  const socket = {
    messages_in: 2,
    messages_out: 3,
    bytes_in: 100,
    bytes_out: 200,
    last_activity: '2026-10-13T08:00:00Z',
    close_code: 0,
    error: '',
  }
  const parent: MonitoredRequest = {
    ...request,
    id: 'connection-1',
    kind: 'websocket',
    state: 'running',
    status: 101,
    stream_end: '',
    stream_outcome: '',
    websocket: {
      client: socket,
      upstream: socket,
      close_source: '',
      gateway_close_code: 0,
    },
  }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/performance/requests') {
      return {
        data: {
          success: true,
          data: { items: [parent, child], capacity: 500, instance: 'node-1' },
        },
      }
    }
    return {
      data: { success: true, data: url.endsWith('ws-0') ? child : parent },
    }
  })
  const user = userEvent.setup()
  renderMonitor()
  await user.click(await screen.findByRole('button', { name: 'connection-1' }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('Connection open')).toBeVisible()
  expect(within(dialog).getByText('Received messages')).toBeVisible()
  await user.click(
    await within(dialog).findByRole('button', { name: child.id })
  )
  expect(await within(dialog).findByText('done / completed')).toBeVisible()
  expect(within(dialog).queryByText('Connection open')).not.toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: parent.id }))
  expect(await within(dialog).findByText('Connection open')).toBeVisible()
})
