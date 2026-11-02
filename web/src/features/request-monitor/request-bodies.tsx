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
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { MonitoredRequest } from './api'

export function RequestBodies(props: { data: MonitoredRequest }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const labels = {
    request: t('Request body'),
    response: t('Response body'),
    upstream_request: t('Upstream request body'),
    upstream_response: t('Upstream response body'),
  }
  const bodies = props.data.bodies ?? []
  if (props.data.bodies_unavailable) {
    return (
      <p className='text-muted-foreground text-sm'>
        {props.data.bodies_unavailable === 'expired'
          ? t('Body captures expired after 30 minutes.')
          : t(
              'Bodies were evicted to keep the capture cache within its memory limit.'
            )}
      </p>
    )
  }
  if (!bodies.length) return null
  return (
    <section className='min-w-0 space-y-2'>
      <h3 className='font-medium'>{t('Request and response bodies')}</h3>
      {props.data.expires_at && (
        <p className='text-muted-foreground text-xs'>
          {t('Expires at')}:{' '}
          {new Date(props.data.expires_at).toLocaleString(locale)}
        </p>
      )}
      <Tabs
        key={props.data.id}
        defaultValue={`${bodies[0].direction}:${bodies[0].attempt ?? 0}`}
      >
        <TabsList className='h-auto max-w-full flex-wrap justify-start'>
          {bodies.map((body) => (
            <TabsTrigger
              key={`${body.direction}:${body.attempt ?? 0}`}
              value={`${body.direction}:${body.attempt ?? 0}`}
            >
              {labels[body.direction]}
              {body.attempt
                ? ` · ${t('Attempt')} ${formatNumber(body.attempt, locale)}`
                : ''}
            </TabsTrigger>
          ))}
        </TabsList>
        {bodies.map((body) => (
          <TabsContent
            key={`${body.direction}:${body.attempt ?? 0}`}
            value={`${body.direction}:${body.attempt ?? 0}`}
            className='min-w-0 space-y-2'
          >
            <div className='flex flex-wrap items-center gap-3 text-xs'>
              <span className='break-all'>
                {body.content_type || '—'} · {body.encoding} ·{' '}
                {formatNumber(body.total_bytes, locale)} B
              </span>
              {!!body.channel_id && (
                <span>
                  {t('Channel ID')}: {body.channel_id}
                </span>
              )}
              <CopyButton
                value={body.content}
                aria-label={t('Copy body')}
                variant='outline'
                size='sm'
              >
                {t('Copy body')}
              </CopyButton>
            </div>
            {body.truncated && (
              <p className='text-xs text-amber-700 dark:text-amber-400'>
                {t('Body truncated at the capture limit.')}
              </p>
            )}
            {body.encoding === 'base64' && (
              <p className='text-muted-foreground text-xs'>
                {t('Binary body is shown as Base64.')}
              </p>
            )}
            {body.content_type === 'application/x-ndjson' && (
              <p className='text-muted-foreground text-xs'>
                {t(
                  'WebSocket messages are separated by newlines in delivery order.'
                )}
              </p>
            )}
            <Textarea
              readOnly
              aria-label={labels[body.direction]}
              value={body.content}
              className='field-sizing-fixed h-64 max-h-96 min-h-32 resize-y font-mono text-xs'
            />
          </TabsContent>
        ))}
      </Tabs>
    </section>
  )
}
