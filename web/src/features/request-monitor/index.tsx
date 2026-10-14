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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getRequests, type MonitoredRequest } from './api'
import { requestStateLabel } from './labels'
import { RequestDetail } from './request-detail'

export function RequestMonitor() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [state, setState] = useState('')
  const [live, setLive] = useState(true)
  const [selected, setSelected] = useState<string | null>(null)
  const requests = useQuery({
    queryKey: ['request-monitor', query, state],
    queryFn: () => getRequests(query, state),
    refetchInterval: live ? 3000 : false,
    refetchOnWindowFocus: live,
    gcTime: 0,
  })
  const columns: StaticDataTableColumn<MonitoredRequest>[] = [
    {
      id: 'kind',
      header: t('Record type'),
      cell: (row) => {
        if (row.kind === 'websocket') return t('WebSocket connection')
        if (row.kind === 'ws_request') return t('WebSocket generation')
        return 'HTTP'
      },
    },
    {
      id: 'time',
      header: t('Time'),
      cell: (row) => new Date(row.started_at).toLocaleString(locale),
    },
    {
      id: 'id',
      header: t('Request ID'),
      cell: (row) => (
        <Button variant='link' onClick={() => setSelected(row.id)}>
          {row.id}
        </Button>
      ),
    },
    {
      id: 'path',
      header: t('Request path'),
      cell: (row) => (
        <span className='break-all'>
          {row.method} {row.path}
        </span>
      ),
    },
    { id: 'model', header: t('Model'), cell: (row) => row.model || '—' },
    {
      id: 'state',
      header: t('Status'),
      cell: (row) => (
        <span className={row.state === 'failed' ? 'text-destructive' : ''}>
          {requestStateLabel(t, row.state, row.kind)}
        </span>
      ),
    },
    {
      id: 'http',
      header: t('Downstream HTTP status'),
      cell: (row) => (row.kind === 'ws_request' ? '—' : row.status || '—'),
    },
    {
      id: 'duration',
      header: t('Duration'),
      cell: (row) => `${formatNumber(row.duration_ms, locale)} ms`,
    },
    {
      id: 'channel',
      header: t('Channel ID'),
      cell: (row) => row.channel_id || '—',
    },
    {
      id: 'reason',
      header: t('Connection end'),
      cell: (row) => row.client_error || row.stream_end || '—',
    },
  ]

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Request monitor')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Monitor the latest {{count}} requests on this instance. Records are cleared on restart. Multi-instance deployments require sticky routing.',
              { count: requests.data?.capacity ?? 500 }
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Only diagnostic metadata is recorded. Bodies, credentials and query parameters are excluded. Upstream HTTP details cover the shared relay transport; other transports may have no upstream records.'
            )}
          </p>
          <form
            className='flex flex-wrap items-center gap-2'
            onSubmit={(event) => {
              event.preventDefault()
              setQuery(search.trim())
            }}
          >
            <Input
              aria-label={t('Search request ID, model or path')}
              placeholder={t('Search request ID, model or path')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className='w-full sm:max-w-sm'
            />
            <Button type='submit' variant='outline'>
              {t('Search')}
            </Button>
            <NativeSelect
              aria-label={t('Request status')}
              value={state}
              onChange={(event) => setState(event.target.value)}
            >
              <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
              <NativeSelectOption value='running'>
                {t('In progress')}
              </NativeSelectOption>
              <NativeSelectOption value='failed'>
                {t('Failed')}
              </NativeSelectOption>
              <NativeSelectOption value='completed'>
                {t('Completed')}
              </NativeSelectOption>
            </NativeSelect>
            <Button
              type='button'
              variant='outline'
              onClick={() => setLive(!live)}
            >
              {live ? t('Pause refresh') : t('Resume refresh')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={requests.isFetching}
              onClick={() => void requests.refetch()}
            >
              {t('Refresh')}
            </Button>
          </form>
          {requests.isPending && <LoadingState />}
          {requests.isError && (
            <ErrorState onRetry={() => void requests.refetch()} />
          )}
          {requests.data && !requests.isError && (
            <>
              <p className='text-muted-foreground text-xs break-all'>
                {t('Instance')}: {requests.data.instance} · {t('Records')}:{' '}
                {formatNumber(requests.data.items.length, locale)}
              </p>
              <StaticDataTable
                columns={columns}
                data={requests.data.items}
                getRowKey={(row) => row.id}
                emptyContent={t(
                  'No matching requests. Send an API request or change the filters.'
                )}
              />
            </>
          )}
          <RequestDetail
            id={selected}
            live={live}
            onClose={() => setSelected(null)}
            onSelect={setSelected}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
