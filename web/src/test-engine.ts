import { api } from './api'
import type { AgentGrant, NodeInfo, NodeResult, TestMetrics, TestPhase } from './types'

export interface ProgressSample {
  phase: TestPhase
  node: NodeInfo
  nodeIndex: number
  nodeTotal: number
  mbps?: number
}

export const median = (values: number[]) => {
  if (!values.length) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 ? sorted[middle]! : (sorted[middle - 1]! + sorted[middle]!) / 2
}

export const jitter = (values: number[]) => {
  if (values.length < 2) return 0
  const differences = values.slice(1).map((value, index) => Math.abs(value - values[index]!))
  return median(differences)
}

export const calculateMbps = (bytes: number, milliseconds: number) =>
  milliseconds <= 0 ? 0 : (bytes * 8) / (milliseconds / 1000) / 1_000_000

export function scoreMetrics(metrics: TestMetrics) {
  const latency = 100 * Math.exp(-metrics.latencyMs / 145)
  const jitterScore = 100 * Math.exp(-metrics.jitterMs / 45)
  const loss = metrics.lossPercent >= 5 ? 0 : 100 * Math.exp(-metrics.lossPercent / 1.3)
  const download = 100 * (1 - Math.exp(-metrics.downloadMbps / 120))
  const upload = 100 * (1 - Math.exp(-metrics.uploadMbps / 75))
  return Math.max(0, Math.min(100, Math.round(latency * .3 + loss * .25 + jitterScore * .15 + download * .2 + upload * .1)))
}

const headers = (grant: AgentGrant) => ({ Authorization: `Bearer ${grant.token}`, 'Cache-Control': 'no-store' })
const endpoint = (grant: AgentGrant, path: string) => `${grant.endpoint.replace(/\/$/, '')}/agent/v1${path}`

export async function measureLatency(grant: AgentGrant, signal: AbortSignal, count = 10) {
  const samples: number[] = []
  let failures = 0
  for (let index = 0; index < count; index += 1) {
    const started = performance.now()
    try {
      const response = await fetch(endpoint(grant, `/probe?nonce=${crypto.randomUUID()}`), {
        cache: 'no-store', headers: headers(grant), signal,
      })
      if (!response.ok) throw new Error(`probe ${response.status}`)
      await response.arrayBuffer()
      samples.push(performance.now() - started)
    } catch (error) {
      if (signal.aborted) throw error
      failures += 1
    }
  }
  if (!samples.length) throw new Error('AGENT_UNAVAILABLE')
  return { latencyMs: median(samples), jitterMs: jitter(samples), lossPercent: failures / count * 100 }
}

export async function measureDownload(
  grant: AgentGrant,
  signal: AbortSignal,
  onSample: (mbps: number) => void,
) {
  const controller = new AbortController()
  const stop = () => controller.abort(signal.reason)
  signal.addEventListener('abort', stop, { once: true })
  const duration = (grant.downloadSeconds ?? 8) * 1000
  const streamCount = grant.downloadStreams ?? 4
  const started = performance.now()
  let bytes = 0
  let reserved = 0
  let lastSample = started
  const reader = async (stream: number) => {
    while (!controller.signal.aborted && performance.now() - started < duration) {
      const remaining = grant.maxDownloadBytes - reserved
      if (remaining <= 0) break
      const requested = Math.min(4 * 1024 * 1024, remaining)
      reserved += requested
      const response = await fetch(endpoint(grant, `/download?bytes=${requested}&stream=${stream}&nonce=${crypto.randomUUID()}`), {
        cache: 'no-store', headers: headers(grant), signal: controller.signal,
      })
      if (!response.ok || !response.body) throw new Error(`download ${response.status}`)
      const body = response.body.getReader()
      while (true) {
        const chunk = await body.read()
        if (chunk.done) break
        bytes += chunk.value.byteLength
        const now = performance.now()
        if (now - lastSample >= 250) {
          onSample(calculateMbps(bytes, now - started))
          lastSample = now
        }
      }
    }
  }
  const timeout = window.setTimeout(() => controller.abort('time-cap'), duration)
  try {
    const settled = await Promise.allSettled(Array.from({ length: streamCount }, (_, index) => reader(index)))
    if (!bytes && settled.some((item) => item.status === 'rejected') && !signal.aborted) throw new Error('AGENT_UNAVAILABLE')
  } finally {
    clearTimeout(timeout)
    signal.removeEventListener('abort', stop)
  }
  const elapsed = Math.min(performance.now() - started, duration)
  return calculateMbps(bytes, elapsed)
}

function randomBlob(size = 512 * 1024) {
  const bytes = new Uint8Array(size)
  for (let offset = 0; offset < size; offset += 65_536) {
    crypto.getRandomValues(bytes.subarray(offset, Math.min(offset + 65_536, size)))
  }
  return new Blob([bytes], { type: 'application/octet-stream' })
}

export async function measureUpload(
  grant: AgentGrant,
  signal: AbortSignal,
  onSample: (mbps: number) => void,
) {
  const duration = (grant.uploadSeconds ?? 8) * 1000
  const streamCount = grant.uploadStreams ?? 2
  const block = randomBlob()
  const started = performance.now()
  let reserved = 0
  let completed = 0
  let lastSample = started
  const worker = async (stream: number) => {
    while (!signal.aborted && performance.now() - started < duration) {
      const remaining = grant.maxUploadBytes - reserved
      if (remaining <= 0) break
      const body = remaining >= block.size ? block : block.slice(0, remaining)
      reserved += body.size
      const response = await fetch(endpoint(grant, `/upload?stream=${stream}&nonce=${crypto.randomUUID()}`), {
        method: 'POST', headers: { ...headers(grant), 'Content-Type': 'application/octet-stream' }, body, signal,
      })
      if (!response.ok) throw new Error(`upload ${response.status}`)
      completed += body.size
      const now = performance.now()
      if (now - lastSample >= 250) {
        onSample(calculateMbps(completed, now - started))
        lastSample = now
      }
    }
  }
  await Promise.all(Array.from({ length: streamCount }, (_, index) => worker(index)))
  return calculateMbps(completed, Math.min(performance.now() - started, duration))
}

export async function runNodeTest(
  testId: string,
  node: NodeInfo,
  index: number,
  total: number,
  signal: AbortSignal,
  onProgress: (sample: ProgressSample) => void,
): Promise<NodeResult> {
  let grant = await api.grant(testId, node, ['probe'])
  onProgress({ phase: 'latency', node, nodeIndex: index, nodeTotal: total })
  const latency = await measureLatency(grant, signal)
  grant = await api.grant(testId, node, ['download'])
  onProgress({ phase: 'download', node, nodeIndex: index, nodeTotal: total, mbps: 0 })
  const downloadMbps = await measureDownload(grant, signal, (mbps) => onProgress({ phase: 'download', node, nodeIndex: index, nodeTotal: total, mbps }))
  grant = await api.grant(testId, node, ['upload'])
  onProgress({ phase: 'upload', node, nodeIndex: index, nodeTotal: total, mbps: 0 })
  const uploadMbps = await measureUpload(grant, signal, (mbps) => onProgress({ phase: 'upload', node, nodeIndex: index, nodeTotal: total, mbps }))
  onProgress({ phase: 'route', node, nodeIndex: index, nodeTotal: total })
  const route = await api.route(testId, node.id)
  const metrics = { ...latency, downloadMbps, uploadMbps }
  return { node, ...metrics, score: scoreMetrics(metrics), route: route.hops }
}
