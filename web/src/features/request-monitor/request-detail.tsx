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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { PolicyDecisionRecord } from '@/features/system-settings/request-policies/decision-record'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import {
  getServerErrorMessage,
  getServerErrorStatus,
} from '@/lib/server-error-message'

import { getRequest, type MonitoredUpstream } from './api'
import { requestStateLabel } from './labels'
import { WebSocketDetails } from './websocket-details'

export function RequestDetail(props: {
  id: string | null
  live: boolean
  onClose: () => void
  onSelect?: (id: string) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const detail = useQuery({
    queryKey: ['request-monitor-detail', props.id],
    queryFn: () => getRequest(props.id ?? ''),
    enabled: props.id !== null,
    refetchInterval: (query) =>
      props.live && query.state.data?.state === 'running' ? 3000 : false,
    refetchOnWindowFocus: props.live,
    retry: false,
    gcTime: 0,
  })
  const data = detail.data
  const columns: StaticDataTableColumn<MonitoredUpstream>[] = [
    { id: 'attempt', header: t('Attempt'), cell: (_, index) => index + 1 },
    { id: 'channel', header: t('Channel ID'), cell: (row) => row.channel_id },
    {
      id: 'host',
      header: t('Upstream host'),
      cell: (row) => (
        <span className='break-all'>
          {row.method} {row.host}
        </span>
      ),
    },
    {
      id: 'status',
      header: t('Upstream HTTP status'),
      cell: (row) => row.status || '—',
    },
    {
      id: 'protocol',
      header: t('Protocol'),
      cell: (row) => row.protocol || '—',
    },
    {
      id: 'headers',
      header: t('Response headers latency'),
      cell: (row) =>
        row.status ? `${formatNumber(row.headers_ms, locale)} ms` : '—',
    },
    {
      id: 'first',
      header: t('First response byte'),
      cell: (row) =>
        row.protocol !== 'WebSocket' && row.status
          ? `${formatNumber(row.first_byte_ms, locale)} ms`
          : '—',
    },
    {
      id: 'duration',
      header: t('Duration'),
      cell: (row) => `${formatNumber(row.duration_ms, locale)} ms`,
    },
    {
      id: 'bytes',
      header: t('Received bytes'),
      cell: (row) =>
        row.protocol === 'WebSocket' ? '—' : formatNumber(row.bytes, locale),
    },
    {
      id: 'reuse',
      header: t('Connection reused'),
      cell: (row) => {
        if (row.protocol === 'WebSocket') return '—'
        return row.reused ? t('Yes') : t('No')
      },
    },
    {
      id: 'end',
      header: t('Connection end'),
      cell: (row) => row.end || t('In progress'),
    },
    {
      id: 'request',
      header: t('Upstream request ID'),
      cell: (row) =>
        row.request_id ? (
          <CopyButton value={row.request_id} size='sm'>
            {row.request_id}
          </CopyButton>
        ) : (
          '—'
        ),
    },
  ]
  return (
    <Dialog
      open={props.id !== null}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Request details')}
      description={props.id}
      contentClassName='sm:max-w-6xl'
      bodyClassName='space-y-5'
    >
      {detail.isPending && <LoadingState />}
      {detail.isError && (
        <ErrorState
          description={
            getServerErrorStatus(detail.error) === 404
              ? t('Request record expired or belongs to another instance')
              : getServerErrorMessage(detail.error)
          }
          onRetry={() => void detail.refetch()}
        />
      )}
      {data && !detail.isError && (
        <>
          <div className='flex flex-wrap items-center gap-3'>
            <span>{requestStateLabel(t, data.state, data.kind)}</span>
            <CopyButton
              value={JSON.stringify(data, null, 2)}
              aria-label={t('Copy diagnostic JSON')}
              variant='outline'
              size='sm'
            >
              {t('Copy diagnostic JSON')}
            </CopyButton>
          </div>
          {data.parent_id && (
            <p>
              {t('Parent connection')}:{' '}
              <Button
                variant='link'
                onClick={() => props.onSelect?.(data.parent_id ?? '')}
              >
                {data.parent_id}
              </Button>
            </p>
          )}
          {data.kind === 'ws_request' && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Generation result is independent of connection lifetime. Message counters and handshake details are on the parent connection.'
              )}
            </p>
          )}
          {data.websocket && (
            <WebSocketDetails
              data={data}
              live={props.live}
              onSelect={props.onSelect}
            />
          )}
          <dl className='grid grid-cols-1 gap-3 text-sm sm:grid-cols-2'>
            <div>
              <dt className='text-muted-foreground'>{t('Start time')}</dt>
              <dd>{new Date(data.started_at).toLocaleString(locale)}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('End time')}</dt>
              <dd>
                {data.ended_at
                  ? new Date(data.ended_at).toLocaleString(locale)
                  : '—'}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('Request path')}</dt>
              <dd className='break-all'>
                {data.method} {data.path}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('Model')}</dt>
              <dd>{data.model || '—'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('User ID')}</dt>
              <dd>{data.user_id || '—'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('Downstream HTTP status')}
              </dt>
              <dd>{data.kind === 'ws_request' ? '—' : data.status || '—'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('Duration')}</dt>
              <dd>{formatNumber(data.duration_ms, locale)} ms</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('Downstream bytes')}</dt>
              <dd>
                {data.kind === 'ws_request'
                  ? '—'
                  : formatNumber(
                      data.websocket?.client.bytes_out ?? data.bytes,
                      locale
                    )}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('Client connection error')}
              </dt>
              <dd>{data.client_error || '—'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('Stream end / outcome')}
              </dt>
              <dd>
                {data.stream_end || '—'} / {data.stream_outcome || '—'}
              </dd>
            </div>
          </dl>
          {data.truncated && (
            <p className='text-amber-700 dark:text-amber-400'>
              {t('Some diagnostic events were truncated to limit memory use.')}
            </p>
          )}
          <section className='space-y-2'>
            <h3 className='font-medium'>{t('Upstream requests')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'HTTP 200 does not guarantee stream completion. EOF is a transport end; use stream outcome and request decision flow to diagnose interruptions.'
              )}
            </p>
            <StaticDataTable
              columns={columns}
              data={data.upstreams ?? []}
              emptyContent={
                data.kind === 'ws_request'
                  ? t(
                      'No new handshake for this generation. Check the parent connection for reused upstream connections.'
                    )
                  : t(
                      'No upstream HTTP records for this request. It may have been rejected locally or used another transport.'
                    )
              }
            />
          </section>
          <section className='space-y-3'>
            <h3 className='font-medium'>{t('Request decision flow')}</h3>
            {data.events?.length ? (
              <PolicyDecisionRecord events={data.events} />
            ) : (
              <p className='text-muted-foreground text-sm'>
                {t('No routing decisions recorded.')}
              </p>
            )}
          </section>
        </>
      )}
    </Dialog>
  )
}
