import { z } from 'zod'
import type {
  AccessStatus,
  AdminOverview,
  AdminSession,
  AgentGrant,
  ApiErrorBody,
  NetworkInfo,
  NodeInfo,
  NodeResult,
  PrivateAccessSettings,
  RouteHop,
  TestCreateResponse,
  TestResultResponse,
} from './types'

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: ApiErrorBody,
  ) {
    super(body.code || `HTTP_${status}`)
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method?.toUpperCase() ?? 'GET'
  const csrfKey = path.startsWith('/api/v1/admin/') ? 'routeglass.admin.csrf' : 'routeglass.access.csrf'
  const csrf = typeof sessionStorage !== 'undefined' ? sessionStorage.getItem(csrfKey) : null
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...(csrf && method !== 'GET' && method !== 'HEAD' ? { 'X-CSRF-Token': csrf } : {}),
      ...init?.headers,
    },
  })
  const text = await response.text()
  const data = text ? JSON.parse(text) : undefined
  if (!response.ok) {
    throw new ApiError(response.status, {
      code: data?.code ?? `HTTP_${response.status}`,
      message: data?.message,
      params: data?.params,
      retryAfterSeconds: data?.retry_after_seconds,
      requestId: data?.request_id,
    })
  }
  return data as T
}

const rawNode = (node: any): NodeInfo => ({
  id: String(node.id),
  name: String(node.name ?? node.id),
  region: String(node.region ?? node.country ?? 'unknown'),
  city: String(node.city ?? node.region ?? 'unknown'),
  country: node.country,
  endpoint: String(node.endpoint ?? ''),
  status: node.status === 'online' ? 'online' : node.status === 'degraded' ? 'degraded' : 'offline',
  tlsStatus: node.tls_status ?? (node.tls_ready ? 'ready' : 'unavailable'),
  ipv4: node.ipv4,
  ipv6: node.ipv6,
  asn: node.asn,
  version: node.version,
  latencyMs: node.latency_ms,
  activeTests: node.active_tests,
  todayTrafficBytes: node.today_traffic_bytes,
  enabled: node.enabled ?? true,
  latitude: node.latitude,
  longitude: node.longitude,
})

const rawHop = (hop: any): RouteHop => ({
  hop: Number(hop.hop ?? 0),
  ip: hop.ip ?? hop.address ?? null,
  asn: hop.asn ?? null,
  network: hop.network ?? null,
  hostname: hop.hostname ?? null,
  rttMs: hop.rtt_ms ?? hop.latency_ms ?? null,
  lossPercent: hop.loss_percent ?? null,
  country: hop.country ?? null,
  city: hop.city ?? hop.region ?? null,
  latitude: hop.latitude ?? null,
  longitude: hop.longitude ?? null,
  geoConfidence: hop.geo_confidence ?? hop.geo_quality ?? 'unknown',
})

async function networkPayload() { return request<any>('/api/v1/network') }

export const api = {
  network: async () => {
    const data = await networkPayload()
    return {
      ip: String(data.ip ?? data.observed_ip ?? ''), asn: String(data.asn ?? ''),
      network: String(data.network ?? data.isp ?? 'unknown'), city: data.city, region: data.region,
      country: data.country,
      ipv4: Boolean(data.ipv4) || data.ip_family === 'ipv4',
      ipv6: Boolean(data.ipv6) || data.ip_family === 'ipv6',
    } satisfies NetworkInfo
  },
  accessStatus: async () => {
    try {
      const data = await request<any>('/api/v1/access/session')
      if (data.csrf) sessionStorage.setItem('routeglass.access.csrf', data.csrf)
      const expires = data.expires_at ?? data.Expires
      return { authorized: true, expiresAt: typeof expires === 'number' ? new Date(expires * 1000).toISOString() : expires } satisfies AccessStatus
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) return { authorized: false }
      throw error
    }
  },
  authorize: (code: string) =>
    request<any>('/api/v1/access/code', {
      method: 'POST',
      body: JSON.stringify({ code }),
    }).then((data) => { if (data.csrf) sessionStorage.setItem('routeglass.access.csrf', data.csrf); return { authorized: true, expiresAt: data.expires_at ?? (data.Expires ? new Date(data.Expires * 1000).toISOString() : undefined) } }),
  redeemInvite: (token: string) => request<any>('/api/v1/access/invite', {
    method: 'POST', body: JSON.stringify({ token }),
  }).then((data) => { if (data.csrf) sessionStorage.setItem('routeglass.access.csrf', data.csrf); return { authorized: true, expiresAt: data.expires_at ?? (data.Expires ? new Date(data.Expires * 1000).toISOString() : undefined) } satisfies AccessStatus }),
  nodes: async () => {
    const data = await networkPayload()
    return (Array.isArray(data) ? data : data.nodes ?? []).map(rawNode) as NodeInfo[]
  },
  createTest: async (nodeId?: string) => {
    const test = await request<{ id: string }>('/api/v1/tests', {
      method: 'POST',
      body: JSON.stringify({ mode: nodeId ? 'manual' : 'smart', node_id: nodeId }),
    })
    const nodes = await api.nodes()
    return { id: test.id, candidates: nodeId ? nodes.filter((node) => node.id === nodeId) : nodes } satisfies TestCreateResponse
  },
  grant: (testId: string, node: NodeInfo, scopes: string[]) =>
    request<any>(`/api/v1/tests/${encodeURIComponent(testId)}/grants`, {
      method: 'POST',
      body: JSON.stringify({ node_id: node.id, scopes }),
    }).then((data) => ({ nodeId: node.id, endpoint: node.endpoint, token: data.grant ?? data.token, expiresAt: data.expires_at ?? new Date(Date.now() + 60_000).toISOString(), maxDownloadBytes: data.max_download_bytes ?? 150_000_000, maxUploadBytes: data.max_upload_bytes ?? 100_000_000, downloadSeconds: data.download_seconds ?? 8, uploadSeconds: data.upload_seconds ?? 8, downloadStreams: data.max_streams ?? 4, uploadStreams: Math.min(data.max_streams ?? 4, 2) } satisfies AgentGrant)),
  submitProbe: (testId: string, nodeId: string, metrics: { latencyMs: number; jitterMs: number; lossPercent: number }) => request<void>(`/api/v1/tests/${encodeURIComponent(testId)}/probes`, {
    method: 'POST', body: JSON.stringify({ node_id: nodeId, latency_ms: metrics.latencyMs, jitter_ms: metrics.jitterMs, loss_percent: metrics.lossPercent }),
  }),
  route: async (testId: string, nodeId: string) => {
    await request<any>(`/api/v1/tests/${encodeURIComponent(testId)}/route`, {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId }),
    })
    for (let attempt = 0; attempt < 44; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 500))
      const snapshot = await request<any>(`/api/v1/tests/${encodeURIComponent(testId)}`)
      const hops = (snapshot.route_hops ?? []).filter((hop: any) => hop.node_id === nodeId).map(rawHop)
      if (hops.length > 0 || snapshot.status === 'route_complete' || snapshot.status === 'route_failed') return { hops }
    }
    return { hops: [] }
  },
  submitNodeResult: (testId: string, result: NodeResult) =>
    request<void>(`/api/v1/tests/${encodeURIComponent(testId)}/results`, {
      method: 'POST',
      body: JSON.stringify({ node_id: result.node.id, latency_ms: result.latencyMs, loss_percent: result.lossPercent, jitter_ms: result.jitterMs, download_mbps: result.downloadMbps, upload_mbps: result.uploadMbps }),
    }),
  result: async (testId: string) => {
    const [data, nodes] = await Promise.all([request<any>(`/api/v1/tests/${encodeURIComponent(testId)}`), api.nodes()])
    const results = (data.results ?? []).map((entry: any) => ({
      node: nodes.find((node) => node.id === (entry.node_id ?? entry.node?.id)) ?? rawNode(entry.node ?? { id: entry.node_id, name: entry.node_id }),
      latencyMs: entry.latency_ms ?? 0, jitterMs: entry.jitter_ms ?? 0, lossPercent: entry.loss_percent ?? 0,
      downloadMbps: entry.download_mbps ?? 0, uploadMbps: entry.upload_mbps ?? 0, score: entry.score ?? 0,
      route: (entry.route ?? data.route_hops?.filter((hop: any) => hop.node_id === entry.node_id) ?? []).map(rawHop), explanation: entry.explanation,
    })) as NodeResult[]
    const sorted = [...results].sort((a, b) => b.score - a.score)
    return { id: data.id ?? testId, results, recommendedNodeId: data.recommended_node_id ?? sorted[0]?.node.id ?? '' } satisfies TestResultResponse
  },
  adminLogin: (username: string, password: string) =>
    request<any>('/api/v1/admin/login', {
      method: 'POST', body: JSON.stringify({ username, password }),
    }).then((data) => { if (data.csrf) sessionStorage.setItem('routeglass.admin.csrf', data.csrf); return { forcePasswordChange: data.must_change_password ?? data.forcePasswordChange } }),
  changeAdminPassword: (currentPassword: string, newPassword: string) => request<void>('/api/v1/admin/password', {
    method: 'POST', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  }),
  adminOverview: () => request<any>('/api/v1/admin/overview').then((data) => { if (data.csrf) sessionStorage.setItem('routeglass.admin.csrf', data.csrf); return { version: data.version ?? 'v0.1.0', uptimeSeconds: data.uptime_seconds ?? 0, nodesOnline: data.online_nodes ?? data.nodesOnline ?? 0, activeSessions: data.active_sessions ?? data.activeSessions ?? 0, testsToday: data.tests_24h ?? data.testsToday ?? 0, trafficTodayBytes: data.traffic_today_bytes ?? 0 } satisfies AdminOverview }),
  adminAccess: () => request<any>('/api/v1/admin/access').then((data) => ({ enabled: data.enabled ?? true, currentCode: data.code ?? data.currentCode ?? '', changesAt: data.changes_at ?? new Date(Date.now() + (data.rotates_seconds ?? 600) * 1000).toISOString(), intervalMinutes: (data.rotates_seconds ?? 600) / 60, sessionMinutes: data.session_minutes ?? 30 } satisfies PrivateAccessSettings)),
  rotateCode: () => request<any>('/api/v1/admin/access', { method: 'POST' }).then((data) => ({ enabled: data.enabled ?? true, currentCode: data.code ?? '', changesAt: new Date(Date.now() + (data.rotates_seconds ?? 600) * 1000).toISOString(), intervalMinutes: (data.rotates_seconds ?? 600) / 60, sessionMinutes: 30 } satisfies PrivateAccessSettings)),
  createInvite: () => request<any>('/api/v1/admin/invites', { method: 'POST' }).then((data) => ({ url: data.url ?? `${location.origin}/invite/${data.token}`, expiresAt: data.expires_at ? new Date(data.expires_at * 1000).toISOString() : data.expiresAt })),
  adminNodes: async () => { const data = await request<any>('/api/v1/admin/nodes'); return (data.nodes ?? data).map(rawNode) as NodeInfo[] },
  addNode: (data: { name: string; region: string; city: string }) =>
    request<any>('/api/v1/admin/nodes', {
      method: 'POST', body: JSON.stringify(data),
    }).then((result) => ({ id: result.id, installCommand: result.install_command ?? `curl -fsSL ${location.origin}/install/agent.sh | sudo bash -s -- --join ${result.token}`, expiresAt: result.expires_at ? new Date(result.expires_at * 1000).toISOString() : result.expiresAt })),
  updateNode: (id: string, data: Pick<NodeInfo, 'name' | 'region' | 'city'> & Partial<Pick<NodeInfo, 'endpoint' | 'latitude' | 'longitude' | 'enabled'>>) =>
    request<void>(`/api/v1/admin/nodes/${encodeURIComponent(id)}`, {
      method: 'PUT', body: JSON.stringify(data),
    }),
  deleteNode: (id: string) => request<void>(`/api/v1/admin/nodes/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  sessions: async () => { const data = await request<any>('/api/v1/admin/sessions'); return (data.sessions ?? data).map((session: any) => ({ id: session.id, ip: session.ip, asn: session.asn, network: session.network, region: session.region, createdAt: typeof session.created_at === 'number' ? new Date(session.created_at * 1000).toISOString() : session.created_at, expiresAt: typeof session.expires_at === 'number' ? new Date(session.expires_at * 1000).toISOString() : session.expires_at, testsUsed: session.tests_used ?? 0 })) as AdminSession[] },
  revokeSession: (id: string) => request<void>(`/api/v1/admin/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
}

export const seedSchema = z.string().regex(/^#[0-9a-f]{6}$/i)

export function errorCode(error: unknown): string {
  if (error instanceof ApiError) return error.body.code.toUpperCase()
  if (error instanceof Error && /^[A-Z][A-Z0-9_]+$/.test(error.message)) return error.message
  if (error instanceof TypeError) return 'NETWORK_ERROR'
  return 'UNKNOWN_ERROR'
}
