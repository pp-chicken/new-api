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
import type { PolicyEvent } from '@/features/system-settings/request-policies/api'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface MonitoredUpstream {
  channel_id: number
  host: string
  method: string
  started_at: string
  headers_ms: number
  first_byte_ms: number
  duration_ms: number
  status: number
  protocol: string
  request_id: string
  reused: boolean
  bytes: number
  end: string
  error_detail?: string
}

export interface MonitorSocket {
  messages_in: number
  messages_out: number
  bytes_in: number
  bytes_out: number
  last_activity: string
  close_code: number
  error: string
  error_detail?: string
}

export interface MonitoredBody {
  direction: 'request' | 'response' | 'upstream_request' | 'upstream_response'
  attempt?: number
  channel_id?: number
  content_type?: string
  encoding: 'utf-8' | 'base64'
  content: string
  total_bytes: number
  truncated: boolean
}

export interface MonitoredRequest {
  kind?: 'http' | 'websocket' | 'ws_request'
  parent_id?: string
  websocket?: {
    client: MonitorSocket
    upstream: MonitorSocket
    close_source: string
    gateway_close_code: number
  }

  id: string
  started_at: string
  ended_at?: string
  expires_at?: string
  bodies?: MonitoredBody[]
  bodies_unavailable?: 'expired' | 'evicted'
  duration_ms: number
  method: string
  path: string
  model: string
  user_id: number
  username?: string
  token_id?: number
  token_name?: string
  channel_id: number
  status: number
  bytes: number
  state: 'running' | 'completed' | 'failed'
  client_error: string
  client_error_detail?: string
  stream_end: string
  stream_outcome: string
  stream_error?: string
  stream_details?: {
    event?: string
    response_id?: string
    incomplete_reason?: string
    error_code?: string
    error_type?: string
    error_status?: number
    error_message?: string
    error_param?: string
    truncated?: boolean
  }
  upstreams: MonitoredUpstream[] | null
  events: PolicyEvent[] | null
  truncated: boolean
}

export interface MonitorList {
  items: MonitoredRequest[]
  capacity: number
  instance: string
  retention_seconds?: number
  body_limit_bytes?: number
}

export async function getRequests(
  q: string,
  state: string
): Promise<MonitorList> {
  const response = await api.get<{ success: boolean; data: MonitorList }>(
    '/api/performance/requests',
    { params: { q, state } }
  )
  return requireServerSuccess(response.data).data
}

export async function getRequest(id: string): Promise<MonitoredRequest> {
  const response = await api.get<{ success: boolean; data: MonitoredRequest }>(
    `/api/performance/requests/${encodeURIComponent(id)}`
  )
  const data = requireServerSuccess(response.data).data
  // 详情响应可能在网络传输期间过期，避免轮询将正文重新放入缓存。
  if (data.expires_at && Date.parse(data.expires_at) <= Date.now()) {
    return { ...data, bodies: undefined, bodies_unavailable: 'expired' }
  }
  return data
}
