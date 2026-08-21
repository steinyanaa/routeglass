export type Locale = 'en' | 'zh-CN'
export type ThemeMode = 'light' | 'dark' | 'system'
export type MapMode = 'globe' | 'route'
export type GeoConfidence = 'exact' | 'approximate' | 'unknown'
export type TestPhase =
  | 'idle'
  | 'authorizing'
  | 'discovering'
  | 'probing'
  | 'latency'
  | 'download'
  | 'upload'
  | 'route'
  | 'analyzing'
  | 'complete'
  | 'failed'
  | 'cancelled'

export interface NetworkInfo {
  ip: string
  asn: string
  network: string
  city?: string
  region?: string
  country?: string
  ipv4: boolean
  ipv6: boolean
}

export interface NodeInfo {
  id: string
  name: string
  region: string
  city: string
  country?: string
  endpoint: string
  status: 'online' | 'offline' | 'degraded'
  tlsStatus: 'ready' | 'pending' | 'unavailable'
  ipv4?: string
  ipv6?: string
  asn?: string
  version?: string
  latencyMs?: number
  activeTests?: number
  todayTrafficBytes?: number
  enabled?: boolean
  latitude?: number
  longitude?: number
}

export interface AccessStatus {
  authorized: boolean
  expiresAt?: string
}

export interface AgentGrant {
  nodeId: string
  endpoint: string
  token: string
  expiresAt: string
  maxDownloadBytes: number
  maxUploadBytes: number
  downloadSeconds?: number
  uploadSeconds?: number
  downloadStreams?: number
  uploadStreams?: number
}

export interface RouteHop {
  hop: number
  ip: string | null
  asn: string | null
  network: string | null
  hostname: string | null
  rttMs: number | null
  lossPercent: number | null
  country: string | null
  city: string | null
  latitude: number | null
  longitude: number | null
  geoConfidence: GeoConfidence
}

export interface TestMetrics {
  latencyMs: number
  jitterMs: number
  lossPercent: number
  downloadMbps: number
  uploadMbps: number
}

export interface NodeResult extends TestMetrics {
  node: NodeInfo
  score: number
  route: RouteHop[]
  explanation?: string
}

export interface TestCreateResponse {
  id: string
  candidates: NodeInfo[]
}

export interface TestResultResponse {
  id: string
  results: NodeResult[]
  recommendedNodeId: string
}

export interface ApiErrorBody {
  code: string
  message?: string
  params?: Record<string, string | number>
  retryAfterSeconds?: number
  requestId?: string
}

export interface AdminOverview {
  version: string
  uptimeSeconds: number
  nodesOnline: number
  activeSessions: number
  testsToday: number
  trafficTodayBytes: number
}

export interface PrivateAccessSettings {
  enabled: boolean
  currentCode: string
  changesAt: string
  intervalMinutes: number
  sessionMinutes: number
}

export interface AdminSession {
  id: string
  ip: string
  asn?: string
  network?: string
  region?: string
  createdAt: string
  expiresAt: string
  testsUsed: number
}
