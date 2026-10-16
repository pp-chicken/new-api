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
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor,
} from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, test, vi } from 'vitest'

import type { TaskLog } from '../../types'
import { TaskDetailsDialog } from '../dialogs/task-details-dialog'

const originalClipboard = Object.getOwnPropertyDescriptor(
  navigator,
  'clipboard'
)
afterEach(() => {
  cleanup()
  if (originalClipboard) {
    Object.defineProperty(navigator, 'clipboard', originalClipboard)
  } else {
    Reflect.deleteProperty(navigator, 'clipboard')
  }
})
const i18n = createInstance()
await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })
const log = {
  id: 1,
  user_id: 1,
  channel_id: 1,
  platform: 'openai-image',
  task_id: 'task_1',
  action: 'GENERATE',
  group: 'default',
  quota: 0,
  submit_time: 1,
  status: 'FAILURE',
  root_info: {
    diagnostics: {
      method: 'POST',
      url: 'https://upstream.example/v1/images/generations',
      status_code: 429,
      request_body: '{"prompt":"一只猫"}',
      response_body: '{"error":{"code":"rate_limit_exceeded"}}',
      response_kind: 'body',
      error_code: 'upstream_error',
    },
  },
} satisfies TaskLog

test('超级管理员能在详情查看请求响应和错误码', async () => {
  render(
    <I18nextProvider i18n={i18n}>
      <TaskDetailsDialog
        log={log}
        isAdmin
        isRoot
        open
        onOpenChange={() => {}}
      />
    </I18nextProvider>
  )
  expect(await screen.findByText('upstream_error')).toBeInTheDocument()
  expect(screen.getByText('429')).toBeInTheDocument()
  expect(screen.getByText('Request Body')).toBeInTheDocument()
  expect(screen.getByText('Response Body')).toBeInTheDocument()
  expect(screen.getByText(/一只猫/)).toBeInTheDocument()
  expect(screen.getByText(/rate_limit_exceeded/)).toBeInTheDocument()
})

test('普通管理员无法查看诊断正文，即使输入数据包含诊断', () => {
  render(
    <I18nextProvider i18n={i18n}>
      <TaskDetailsDialog
        log={log}
        isAdmin
        isRoot={false}
        open
        onOpenChange={() => {}}
      />
    </I18nextProvider>
  )
  expect(screen.queryByText('Request Body')).not.toBeInTheDocument()
  expect(screen.queryByText(/rate_limit_exceeded/)).not.toBeInTheDocument()
})

test('流式诊断明确标记最后事件并支持复制响应', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  })
  const streamLog: TaskLog = {
    ...log,
    root_info: {
      diagnostics: {
        ...log.root_info.diagnostics,
        response_kind: 'last_sse_event',
      },
    },
  }
  render(
    <I18nextProvider i18n={i18n}>
      <TaskDetailsDialog
        log={streamLog}
        isAdmin
        isRoot
        open
        onOpenChange={() => {}}
      />
    </I18nextProvider>
  )
  expect(screen.getByText('Last SSE Event')).toBeInTheDocument()
  fireEvent.click(screen.getAllByRole('button', { name: 'Copy code' })[1])
  await waitFor(() =>
    expect(writeText).toHaveBeenCalledWith(
      log.root_info.diagnostics.response_body
    )
  )
})

test('旧任务缺少诊断时展示明确空状态', () => {
  render(
    <I18nextProvider i18n={i18n}>
      <TaskDetailsDialog
        log={{ ...log, root_info: {} }}
        isAdmin
        isRoot
        open
        onOpenChange={() => {}}
      />
    </I18nextProvider>
  )
  expect(
    screen.getByText('No request diagnostics were recorded for this task.')
  ).toBeInTheDocument()
  expect(screen.queryByText('Request Body')).not.toBeInTheDocument()
})
