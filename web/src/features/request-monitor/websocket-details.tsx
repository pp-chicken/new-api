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

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getRequests, type MonitoredRequest, type MonitorSocket } from './api'
import { requestStateLabel } from './labels'

export function WebSocketDetails(props: {
  data: MonitoredRequest
  live: boolean
  onSelect?: (id: string) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const calls = useQuery({
    queryKey: ['request-monitor-children', props.data.id],
    queryFn: () => getRequests(props.data.id, ''),
    refetchInterval:
      props.live && props.data.state === 'running' ? 3000 : false,
    refetchOnWindowFocus: props.live,
    gcTime: 0,
  })
  const ws = props.data.websocket
  if (!ws) return null
  const columns: StaticDataTableColumn<MonitorSocket & { side: string }>[] = [
    { id: 'side', header: t('Connection side'), cell: (row) => row.side },
    {
      id: 'in',
      header: t('Received messages'),
      cell: (row) => formatNumber(row.messages_in, locale),
    },
    {
      id: 'out',
      header: t('Sent messages'),
      cell: (row) => formatNumber(row.messages_out, locale),
    },
    {
      id: 'bytesIn',
      header: t('Received payload bytes'),
      cell: (row) => formatNumber(row.bytes_in, locale),
    },
    {
      id: 'bytesOut',
      header: t('Sent payload bytes'),
      cell: (row) => formatNumber(row.bytes_out, locale),
    },
    {
      id: 'last',
      header: t('Last activity'),
      cell: (row) =>
        row.last_activity.startsWith('0001')
          ? '—'
          : new Date(row.last_activity).toLocaleString(locale),
    },
    {
      id: 'code',
      header: t('Peer close code'),
      cell: (row) => row.close_code || '—',
    },
    { id: 'error', header: t('Error'), cell: (row) => row.error || '—' },
    {
      id: 'errorDetail',
      header: t('Error details'),
      cell: (row) => (
        <div className='max-w-sm break-all whitespace-pre-wrap'>
          <span>{row.error_detail || '—'}</span>
        </div>
      ),
    },
  ]
  const callColumns: StaticDataTableColumn<MonitoredRequest>[] = [
    {
      id: 'id',
      header: t('Request ID'),
      cell: (row) => (
        <Button variant='link' onClick={() => props.onSelect?.(row.id)}>
          {row.id}
        </Button>
      ),
    },
    { id: 'model', header: t('Model'), cell: (row) => row.model || '—' },
    {
      id: 'state',
      header: t('Status'),
      cell: (row) => requestStateLabel(t, row.state),
    },
    {
      id: 'duration',
      header: t('Duration'),
      cell: (row) => `${formatNumber(row.duration_ms, locale)} ms`,
    },
    {
      id: 'result',
      header: t('Stream end / outcome'),
      cell: (row) => `${row.stream_end || '—'} / ${row.stream_outcome || '—'}`,
    },
  ]
  return (
    <section className='space-y-3'>
      <h3 className='font-medium'>{t('WebSocket connection')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Counters include successful application messages only, excluding control frames and protocol overhead. Close source is the first observed endpoint or the gateway initiating shutdown.'
        )}
      </p>
      <p className='text-sm'>
        {t('Close source')}: {ws.close_source || '—'} ·{' '}
        {t('Gateway close code')}: {ws.gateway_close_code || '—'}
      </p>
      <StaticDataTable
        columns={columns}
        data={[
          { ...ws.client, side: t('Client') },
          { ...ws.upstream, side: t('Upstream') },
        ]}
      />
      <h3 className='font-medium'>{t('Generations on this connection')}</h3>
      {calls.isPending && <LoadingState />}
      {calls.isError && <ErrorState onRetry={() => void calls.refetch()} />}
      {calls.data && !calls.isError && (
        <StaticDataTable
          columns={callColumns}
          data={calls.data.items.filter(
            (row) => row.parent_id === props.data.id
          )}
          emptyContent={t('No retained generations for this connection.')}
        />
      )}
    </section>
  )
}
