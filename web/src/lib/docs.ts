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
export function getDocsLink(configuredLink: unknown): {
  href: string
  external: boolean
} {
  const link = typeof configuredLink === 'string' ? configuredLink.trim() : ''
  // 兼容旧版本保存的官方默认地址，让已有站点也使用内置文档。
  if (
    !link ||
    /^https?:\/\/(?:www\.)?docs\.newapi\.(pro|ai)(?:\/|$)/i.test(link)
  ) {
    return { href: '/docs', external: false }
  }
  return { href: link, external: /^(https?:)?\/\//i.test(link) }
}
