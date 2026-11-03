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
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { useTopNavLinks } from '@/hooks/use-top-nav-links'

function navigationFor(docsLink?: string, enabled = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {
    docs_link: docsLink,
    HeaderNavModules: JSON.stringify({ docs: enabled }),
  })
  return renderHook(() => useTopNavLinks(), {
    wrapper: (props: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    ),
  })
}

describe('文档入口', () => {
  it.each([
    undefined,
    '',
    'https://docs.newapi.pro',
    'https://docs.newapi.pro/',
    'https://docs.newapi.ai/zh/docs/installation',
    'https://www.docs.newapi.pro/guide',
    '/docs',
  ])('配置为 %s 时在当前站点打开文档', (docsLink) => {
    const { result } = navigationFor(docsLink)
    expect(result.current.find((link) => link.title === 'Docs')).toMatchObject({
      href: '/docs',
      external: false,
    })
  })

  it.each([
    ['https://help.example.com/guide', true],
    ['/help', false],
  ] as const)('保留自定义文档地址 %s 的跳转方式', (docsLink, external) => {
    const { result } = navigationFor(docsLink)
    expect(result.current.find((link) => link.title === 'Docs')).toMatchObject({
      href: docsLink,
      external,
    })
  })

  it('关闭文档导航后不显示入口', () => {
    const { result } = navigationFor('/docs', false)
    expect(result.current.some((link) => link.title === 'Docs')).toBe(false)
  })
})
