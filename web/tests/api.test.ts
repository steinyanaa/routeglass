import { afterEach, vi } from 'vitest'
import { api } from '../src/api'
import type { NodeInfo } from '../src/types'

afterEach(() => vi.restoreAllMocks())

describe('Go API adapters', () => {
  it('treats an unauthenticated access status as a public session', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ code: 'access_required' }), { status: 401 }))
    await expect(api.accessStatus()).resolves.toEqual({ authorized: false })
  })

  it('normalizes the current nodes envelope and TLS flag', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ nodes: [{ id: 'n1', name: 'Tokyo', endpoint: 'https://node:9443', status: 'online', tls_ready: true }] }), { status: 200 }))
    const nodes = await api.nodes()
    expect(nodes[0]).toMatchObject({ id: 'n1', tlsStatus: 'ready', status: 'online' })
  })

  it('maps the observed address family and keeps access CSRF for test writes', async () => {
	const fetchMock = vi.spyOn(globalThis, 'fetch')
		.mockResolvedValueOnce(new Response(JSON.stringify({ observed_ip: '192.0.2.1', ip_family: 'ipv4', network: 'Example', asn: 'AS64500' }), { status: 200 }))
		.mockResolvedValueOnce(new Response(JSON.stringify({ id: 'session-1', csrf: 'access-csrf', expires_at: 2_000_000_000 }), { status: 201 }))
		.mockResolvedValueOnce(new Response(JSON.stringify({ id: 'test-1' }), { status: 201 }))
		.mockResolvedValueOnce(new Response(JSON.stringify({ nodes: [] }), { status: 200 }))
	const network = await api.network()
	expect(network).toMatchObject({ ip: '192.0.2.1', ipv4: true, ipv6: false })
	await api.authorize('123456')
	await api.createTest()
	const headers = fetchMock.mock.calls[2]?.[1]?.headers as Record<string, string>
	expect(headers['X-CSRF-Token']).toBe('access-csrf')
  })

	it('submits the server metric field contract', async () => {
		const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 200 }))
		const node: NodeInfo = { id: 'n1', name: 'Tokyo', region: 'JP', city: 'Tokyo', endpoint: 'https://node:9443', status: 'online', tlsStatus: 'ready' }
		await api.submitNodeResult('test-1', { node, latencyMs: 20, jitterMs: 2, lossPercent: 0.1, downloadMbps: 300, uploadMbps: 80, score: 90, route: [] })
		expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ node_id: 'n1', latency_ms: 20, loss_percent: 0.1, jitter_ms: 2, download_mbps: 300, upload_mbps: 80 })
	})

  it('requests a scoped grant and combines it with the direct endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ grant: 'signed-token' }), { status: 201 }))
    const node: NodeInfo = { id: 'n1', name: 'Tokyo', region: 'JP', city: 'Tokyo', endpoint: 'https://node:9443', status: 'online', tlsStatus: 'ready' }
    const grant = await api.grant('test-1', node, ['probe'])
    expect(grant).toMatchObject({ nodeId: 'n1', endpoint: node.endpoint, token: 'signed-token' })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/tests/test-1/grants')
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ node_id: 'n1', scopes: ['probe'] })
  })
})
