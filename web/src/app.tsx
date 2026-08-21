import * as Dialog from '@radix-ui/react-dialog'
import * as Tabs from '@radix-ui/react-tabs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity, ArrowLeft, ArrowRight, Check, ChevronRight, CircleGauge, Cloud, Copy,
  Globe2, Home, KeyRound, Languages, ListTree, LockKeyhole, Moon, MoreHorizontal,
  Network, Plus, RefreshCw, Route as RouteIcon, Server, Settings, ShieldCheck, Signal,
  Sparkles, Sun, Users, X, Zap,
} from 'lucide-react'
import { Suspense, lazy, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink, Navigate, Outlet, Route, Routes, useLocation, useNavigate, useParams } from 'react-router'
import { api, errorCode } from './api'
import { RouteMap2D } from './route-map'
import { measureLatency, runNodeTest, type ProgressSample } from './test-engine'
import { readTheme, setMotion, transitionTheme } from './theme'
import type { MapMode, NodeInfo, NodeResult, TestPhase, TestResultResponse, ThemeMode } from './types'

const GlobeRoute = lazy(() => import('./globe-route'))

function supportsWebGL() {
  try {
    const canvas = document.createElement('canvas')
    return Boolean(canvas.getContext('webgl2') || canvas.getContext('webgl'))
  } catch { return false }
}

function useFormat() {
  const { i18n } = useTranslation()
  return {
    number: (value: number, maximumFractionDigits = 1) => new Intl.NumberFormat(i18n.language, { maximumFractionDigits }).format(value),
    bytes: (value: number) => `${new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(value / 1_000_000_000)} GB`,
    date: (value: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)),
    duration: (seconds: number) => seconds < 3600 ? `${Math.floor(seconds / 60)}m` : `${Math.floor(seconds / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`,
  }
}

function ErrorNotice({ error, retry }: { error: unknown; retry?: () => void }) {
  const { t } = useTranslation()
  const code = errorCode(error)
  return <div className="notice notice--error" role="alert"><span>{t(`errors.${code}`, { defaultValue: t('errors.UNKNOWN_ERROR') })}</span>{retry && <button className="button button--text" onClick={retry}>{t('common.retry')}</button>}</div>
}

function Brand() {
  return <div className="brand"><span className="brand-mark"><RouteIcon size={24} /></span><span>RouteGlass</span></div>
}

function ThemeTools() {
  const { t, i18n } = useTranslation()
  const [preference, setPreference] = useState(readTheme)
  const changeTheme = async (mode: ThemeMode) => {
    const next = { ...preference, mode }
    setPreference(next)
    await transitionTheme(next)
  }
  return <div className="top-actions">
    <button className="icon-button" aria-label={t('common.theme')} onClick={() => void changeTheme(preference.mode === 'dark' ? 'light' : 'dark')}>
      {preference.mode === 'dark' ? <Sun /> : <Moon />}
    </button>
    <button className="icon-button" aria-label={t('common.language')} onClick={() => void i18n.changeLanguage(i18n.language.startsWith('zh') ? 'en' : 'zh-CN')}><Languages /></button>
  </div>
}

type NavItem = { to: string; key: string; icon: typeof Home; end?: boolean; secondary?: boolean }

const publicNav: NavItem[] = [
  { to: '/', key: 'home', icon: Home, end: true },
  { to: '/test', key: 'test', icon: Zap },
  { to: '/tools', key: 'tools', icon: Network },
]

const adminNav: NavItem[] = [
  { to: '/admin', key: 'overview', icon: CircleGauge, end: true },
  { to: '/admin/access', key: 'access', icon: KeyRound },
  { to: '/admin/nodes', key: 'nodes', icon: Server },
  { to: '/admin/sessions', key: 'sessions', icon: Users, secondary: true },
  { to: '/admin/settings', key: 'settings', icon: Settings, secondary: true },
]

function AppShell({ admin = false }: { admin?: boolean }) {
  const { t } = useTranslation()
  const nav = admin ? adminNav : publicNav
  return <div className="app-shell">
    <aside className="nav-rail"><Brand /><nav>{nav.map(({ to, key, icon: Icon, end }) => <NavLink key={to} to={to} end={end} className={({ isActive }) => `nav-item ${isActive ? 'nav-item--active' : ''}`}><span><Icon /></span><small>{t(`nav.${key}`)}</small></NavLink>)}</nav></aside>
    <header className="top-bar"><Brand /><span className="top-bar-title">{admin ? t('admin.title') : ''}</span><ThemeTools /></header>
    <main className="app-main"><Outlet /></main>
    <nav className="bottom-nav">{nav.filter((item) => !item.secondary).map(({ to, key, icon: Icon, end }) => <NavLink key={to} to={to} end={end} className={({ isActive }) => `nav-item ${isActive ? 'nav-item--active' : ''}`}><span><Icon /></span><small>{t(`nav.${key}`)}</small></NavLink>)}{admin && <NavLink to="/admin/settings" className={({ isActive }) => `nav-item ${isActive ? 'nav-item--active' : ''}`}><span><MoreHorizontal /></span><small>{t('nav.more')}</small></NavLink>}</nav>
  </div>
}

function AccessDialog({ open, onOpenChange, onSuccess }: { open: boolean; onOpenChange: (open: boolean) => void; onSuccess: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [code, setCode] = useState('')
  const [validation, setValidation] = useState('')
  const authorize = useMutation({ mutationFn: api.authorize, onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['access'] }); onOpenChange(false); onSuccess() } })
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const normalized = code.replace(/\D/g, '')
    if (!/^\d{6}$/.test(normalized)) return setValidation(t('access.invalid'))
    setValidation('')
    authorize.mutate(normalized)
  }
  return <Dialog.Root open={open} onOpenChange={onOpenChange}><Dialog.Portal><Dialog.Overlay className="dialog-scrim" /><Dialog.Content className="dialog sheet"><div className="sheet-handle" /><Dialog.Close className="dialog-close" aria-label={t('common.close')}><X /></Dialog.Close><div className="dialog-icon"><LockKeyhole /></div><Dialog.Title>{t('access.title')}</Dialog.Title><Dialog.Description>{t('access.description')}</Dialog.Description><form onSubmit={submit} className="form-stack"><label className="field"><span>{t('access.label')}</span><input autoFocus inputMode="numeric" autoComplete="one-time-code" maxLength={7} value={code} placeholder={t('access.placeholder')} onChange={(event) => setCode(event.target.value.replace(/[^\d ]/g, '').slice(0, 7))} aria-invalid={Boolean(validation || authorize.error)} /></label>{validation && <span className="field-error">{validation}</span>}{authorize.error && <ErrorNotice error={authorize.error} />}<button className="button button--primary button--large" disabled={authorize.isPending}>{authorize.isPending ? t('common.loading') : t('access.submit')}<ArrowRight /></button></form></Dialog.Content></Dialog.Portal></Dialog.Root>
}

function ServerDialog({ open, onOpenChange, nodes, onSelect }: { open: boolean; onOpenChange: (open: boolean) => void; nodes: NodeInfo[]; onSelect: (id?: string) => void }) {
  const { t } = useTranslation()
  return <Dialog.Root open={open} onOpenChange={onOpenChange}><Dialog.Portal><Dialog.Overlay className="dialog-scrim" /><Dialog.Content className="dialog sheet sheet--wide"><div className="sheet-handle" /><Dialog.Close className="dialog-close" aria-label={t('common.close')}><X /></Dialog.Close><Dialog.Title>{t('servers.title')}</Dialog.Title><div className="server-list"><button className="server-row server-row--selected" onClick={() => onSelect()}><span className="server-icon"><Sparkles /></span><span><strong>{t('servers.automatic')}</strong><small>{t('home.chooseHint')}</small></span><Check /></button>{nodes.map((node) => { const ready = node.status === 'online' && node.tlsStatus === 'ready'; return <button key={node.id} className="server-row" disabled={!ready} onClick={() => onSelect(node.id)}><span className={`status-dot status-dot--${node.status}`} /><span><strong>{node.name}</strong><small>{node.city} · {ready ? `${node.latencyMs ?? '—'} ms` : t('servers.unavailable')}</small></span><ChevronRight /></button> })}{!nodes.length && <div className="empty-state">{t('servers.empty')}</div>}</div></Dialog.Content></Dialog.Portal></Dialog.Root>
}

function HomePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const network = useQuery({ queryKey: ['network'], queryFn: api.network })
  const access = useQuery({ queryKey: ['access'], queryFn: api.accessStatus })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes })
  const [accessOpen, setAccessOpen] = useState(false)
  const [serverOpen, setServerOpen] = useState(false)
  const [selectedNode, setSelectedNode] = useState<NodeInfo | undefined>()
  const launch = () => {
    if (!access.data?.authorized) return setAccessOpen(true)
    navigate('/test', { state: { nodeId: selectedNode?.id } })
  }
  const choose = (id?: string) => { setSelectedNode(nodes.data?.find((node) => node.id === id)); setServerOpen(false) }
  return <div className="page home-page">
    <section className="hero expressive-surface">
      <div className="hero-copy"><span className="eyebrow"><ShieldCheck />{t('home.eyebrow')}</span><h1>{t('home.title')}</h1><p>{t('home.subtitle')}</p></div>
      <div className="network-card"><div className="section-heading"><span>{t('home.yourNetwork')}</span><Signal /></div>{network.isLoading ? <div className="skeleton skeleton--line" /> : network.error ? <ErrorNotice error={network.error} retry={() => void network.refetch()} /> : <><strong className="network-name">{network.data?.network ?? t('common.unknown')}</strong><span>{network.data?.asn} · {[network.data?.city, network.data?.region].filter(Boolean).join(', ') || t('common.unknown')}</span><div className="chip-row"><span className={`chip ${network.data?.ipv4 ? 'chip--ok' : ''}`}>{t('home.ipv4')} {network.data?.ipv4 ? '✓' : '—'}</span><span className={`chip ${network.data?.ipv6 ? 'chip--ok' : ''}`}>{t('home.ipv6')} {network.data?.ipv6 ? '✓' : '—'}</span></div></>}</div>
      <button className="start-button primary-action-enter" onClick={launch}><span className="start-button-icon"><Zap /></span><span>{t('home.start')}<small>{access.data?.authorized ? t('home.authorized') : t('home.accessRequired')}</small></span><ArrowRight /></button>
    </section>
    <section className="home-details"><button className="choice-surface" onClick={() => setServerOpen(true)}><span className="choice-icon"><Server /></span><span><strong>{selectedNode?.name ?? t('home.chooseServer')}</strong><small>{selectedNode ? `${t('servers.manual')} · ${selectedNode.city}` : t('home.chooseHint')}</small></span><ChevronRight /></button><div className="privacy-surface"><LockKeyhole /><span><strong>{t('home.private')}</strong><small>{access.data?.authorized ? t('home.authorized') : t('home.accessRequired')}</small></span><span className={`status-dot status-dot--${access.data?.authorized ? 'online' : 'degraded'}`} /></div></section>
    <AccessDialog open={accessOpen} onOpenChange={setAccessOpen} onSuccess={() => navigate('/test', { state: { nodeId: selectedNode?.id } })} />
    <ServerDialog open={serverOpen} onOpenChange={setServerOpen} nodes={nodes.data ?? []} onSelect={choose} />
  </div>
}

function TestPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const nodeId = (location.state as { nodeId?: string } | null)?.nodeId
  const [phase, setPhase] = useState<TestPhase>('discovering')
  const [sample, setSample] = useState<ProgressSample | null>(null)
  const [error, setError] = useState<unknown>()
  const controller = useRef<AbortController | null>(null)
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
    controller.current = new AbortController()
    const signal = controller.current.signal
    const run = async () => {
      try {
        setPhase('probing')
        const test = await api.createTest(nodeId)
        const eligible = test.candidates.filter((node) => node.status === 'online' && node.tlsStatus === 'ready')
        if (!eligible.length) throw new Error('NO_ELIGIBLE_NODES')
        const probed: Array<{ node: NodeInfo; quality: number }> = []
        for (const node of eligible) {
          setSample({ phase: 'probing', node, nodeIndex: probed.length + 1, nodeTotal: eligible.length })
          try {
            const grant = await api.grant(test.id, node, ['probe'])
            const metrics = await measureLatency(grant, signal, 5)
            await api.submitProbe(test.id, node.id, metrics)
            probed.push({ node: { ...node, latencyMs: metrics.latencyMs }, quality: metrics.latencyMs + metrics.jitterMs * 2 + metrics.lossPercent * 100 })
          } catch (probeError) {
            if (signal.aborted) throw probeError
          }
        }
        const candidates = (nodeId ? probed : probed.sort((a, b) => a.quality - b.quality).slice(0, 3)).map((entry) => entry.node)
        if (!candidates.length) throw new Error('NO_ELIGIBLE_NODES')
        const localResults: NodeResult[] = []
        for (let index = 0; index < candidates.length; index += 1) {
          if (signal.aborted) return
          try {
            const result = await runNodeTest(test.id, candidates[index]!, index + 1, candidates.length, signal, (progress) => { setPhase(progress.phase); setSample(progress) })
            localResults.push(result)
            await api.submitNodeResult(test.id, result)
          } catch (nodeError) {
            if (signal.aborted) throw nodeError
          }
        }
        if (!localResults.length) throw new Error('AGENT_UNAVAILABLE')
        setPhase('analyzing')
        const sorted = [...localResults].sort((a, b) => b.score - a.score)
        const result: TestResultResponse = { id: test.id, results: sorted, recommendedNodeId: sorted[0]!.node.id }
        setPhase('complete')
        navigate(`/results/${test.id}`, { replace: true, state: { result } })
      } catch (caught) {
        if (!signal.aborted) { setError(caught instanceof Error && caught.message === 'NO_ELIGIBLE_NODES' ? { code: 'NO_ELIGIBLE_NODES' } : caught); setPhase('failed') }
      }
    }
    void run()
    return () => controller.current?.abort('navigation')
  }, [navigate, nodeId])
  const phaseLabel: Record<TestPhase, string> = { idle: 'test.checking', authorizing: 'test.checking', discovering: 'test.checking', probing: 'test.probing', latency: 'test.latency', download: 'test.download', upload: 'test.upload', route: 'test.route', analyzing: 'test.analyzing', complete: 'test.complete', failed: 'test.failed', cancelled: 'test.failed' }
  const steps: TestPhase[] = ['probing', 'latency', 'download', 'upload', 'route', 'analyzing']
  const step = Math.max(0, steps.indexOf(phase))
  return <div className="page test-page"><section className="test-stage expressive-surface"><div className={`test-orb test-orb--${phase}`}><span /><span /><Zap /></div><span className="eyebrow">{sample ? t('test.nodeProgress', { current: sample.nodeIndex, total: sample.nodeTotal }) : t('test.title')}</span><h1>{t(phaseLabel[phase])}</h1>{sample?.node && <p>{sample.node.name} · {sample.node.city}</p>}{sample?.mbps != null && <div className="live-speed"><strong>{Math.round(sample.mbps)}</strong><span>Mbps</span></div>}<div className="wavy-progress" role="progressbar" aria-valuenow={(step + 1) / steps.length * 100}><span style={{ transform: `scaleX(${phase === 'failed' ? 1 : (step + .55) / steps.length})` }} /></div>{error != null && <ErrorNotice error={error instanceof Object && 'code' in error ? new Error((error as { code: string }).code) : error} />}{phase === 'failed' && <button className="button button--primary" onClick={() => navigate('/')}>{t('test.restart')}</button>}<button className="button button--text cancel-test" onClick={() => { controller.current?.abort('cancelled'); navigate('/') }}>{t('common.cancel')}</button></section></div>
}

type SortMode = 'overall' | 'latency' | 'bandwidth'

function ResultsPage() {
  const { t } = useTranslation()
  const format = useFormat()
  const { id = '' } = useParams()
  const location = useLocation()
  const local = (location.state as { result?: TestResultResponse } | null)?.result
  const remote = useQuery({ queryKey: ['result', id], queryFn: () => api.result(id), enabled: !local })
  const result = local ?? remote.data
  const [sort, setSort] = useState<SortMode>('overall')
  const [mapMode, setMapMode] = useState<MapMode>(() => (localStorage.getItem('routeglass.map.v1') === 'route' || !supportsWebGL() ? 'route' : 'globe'))
  const [selectedHop, setSelectedHop] = useState<number | null>(null)
  const sorted = useMemo(() => [...(result?.results ?? [])].sort((a, b) => sort === 'latency' ? a.latencyMs - b.latencyMs : sort === 'bandwidth' ? (b.downloadMbps + b.uploadMbps) - (a.downloadMbps + a.uploadMbps) : b.score - a.score), [result, sort])
  const recommended = result?.results.find((entry) => entry.node.id === result.recommendedNodeId) ?? sorted[0]
  const chooseMap = (mode: MapMode) => { setMapMode(mode); localStorage.setItem('routeglass.map.v1', mode) }
  if (remote.error) return <div className="page"><ErrorNotice error={remote.error} retry={() => void remote.refetch()} /></div>
  if (!recommended) return <div className="page"><div className="skeleton skeleton--hero" /></div>
  const grade = recommended.score >= 90 ? 'excellent' : recommended.score >= 75 ? 'good' : recommended.score >= 55 ? 'fair' : 'poor'
  return <div className="page results-page"><section className="recommendation expressive-surface"><div className="score-ring"><strong>{recommended.score}</strong><span>{t(`result.${grade}`)}</span></div><div><span className="eyebrow"><Sparkles />{t('common.recommended')}</span><h1>{recommended.node.name}</h1><p>{recommended.explanation || `${format.number(recommended.latencyMs)} ms · ${format.number(recommended.lossPercent)}% · ${format.number(recommended.downloadMbps)} Mbps`}</p><strong>{t('result.bestFor')}</strong></div></section><section className="metrics-row">{[['download', recommended.downloadMbps, 'Mbps'], ['upload', recommended.uploadMbps, 'Mbps'], ['latency', recommended.latencyMs, 'ms'], ['jitter', recommended.jitterMs, 'ms'], ['loss', recommended.lossPercent, '%']].map(([key, value, unit]) => <div className="metric" key={String(key)}><span>{t(`result.${key}`)}</span><strong>{format.number(Number(value))}</strong><small>{unit}</small></div>)}</section><section className="surface ranking"><Tabs.Root value={sort} onValueChange={(value) => setSort(value as SortMode)}><Tabs.List className="segmented"><Tabs.Trigger value="overall">{t('result.overall')}</Tabs.Trigger><Tabs.Trigger value="latency">{t('result.lowLatency')}</Tabs.Trigger><Tabs.Trigger value="bandwidth">{t('result.bandwidth')}</Tabs.Trigger></Tabs.List></Tabs.Root><div className="ranking-list">{sorted.map((entry, index) => <div className="ranking-row" key={entry.node.id}><span className="rank">{index + 1}</span><span><strong>{entry.node.name}</strong><small>{entry.node.city}</small></span><span><strong>{entry.score}</strong><small>{format.number(entry.latencyMs)} ms · {format.number(entry.downloadMbps)} Mbps</small></span></div>)}</div></section><section className="surface route-section"><div className="section-heading"><div><span className="eyebrow"><RouteIcon />{t('result.routeTitle')}</span><h2>{recommended.node.city} → {recommended.route.at(-1)?.city ?? t('common.unknown')}</h2></div><div className="segmented compact"><button className={mapMode === 'globe' ? 'active' : ''} onClick={() => chooseMap('globe')}><Globe2 />{t('result.globe')}</button><button className={mapMode === 'route' ? 'active' : ''} onClick={() => chooseMap('route')}><ListTree />{t('result.route')}</button></div></div>{mapMode === 'globe' ? <Suspense fallback={<div className="map-loading" />}><GlobeRoute hops={recommended.route} selectedHop={selectedHop} onSelect={setSelectedHop} /></Suspense> : <RouteMap2D hops={recommended.route} selectedHop={selectedHop} onSelect={setSelectedHop} />}<RouteTable hops={recommended.route} selected={selectedHop} onSelect={setSelectedHop} /></section></div>
}

function RouteTable({ hops, selected, onSelect }: { hops: NodeResult['route']; selected: number | null; onSelect: (hop: number) => void }) {
  const { t } = useTranslation()
  return <div className="route-table-wrap"><table className="route-table"><thead><tr><th>{t('result.hop')}</th><th>ASN</th><th>{t('result.network')}</th><th>{t('result.location')}</th><th>RTT</th></tr></thead><tbody>{hops.map((hop) => <tr key={hop.hop} className={selected === hop.hop ? 'selected' : ''} onClick={() => onSelect(hop.hop)}><td>{hop.hop}</td><td>{hop.asn ?? '—'}</td><td>{hop.network ?? hop.hostname ?? '—'}</td><td>{hop.city ?? hop.country ?? t('common.unknown')}{hop.geoConfidence === 'approximate' && <small>{t('result.approximate')}</small>}</td><td>{hop.rttMs == null ? '—' : `${hop.rttMs.toFixed(1)} ms`}</td></tr>)}</tbody></table></div>
}

function ToolsPage() {
  const { t } = useTranslation()
  return <div className="page"><section className="surface empty-tool"><Network /><h1>{t('nav.tools')}</h1><p>{t('admin.operational')}</p></section></div>
}

function InvitePage() {
  const { token = '' } = useParams()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const invite = useMutation({ mutationFn: () => api.redeemInvite(token), onSuccess: () => navigate('/', { replace: true }) })
  const started = useRef(false)
  useEffect(() => { if (token && !started.current) { started.current = true; invite.mutate() } }, [token, invite])
  return <div className="page test-page"><section className="test-stage expressive-surface"><div className="test-orb"><span /><span /><KeyRound /></div><h1>{t('access.title')}</h1>{invite.error && <ErrorNotice error={invite.error} />}<button className="button button--primary" onClick={() => invite.mutate()}>{t('common.retry')}</button></section></div>
}

function AdminLogin() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const login = useMutation({ mutationFn: () => api.adminLogin(username, password), onSuccess: (result) => navigate(result.forcePasswordChange ? '/admin/change-password' : '/admin', { replace: true }) })
  return <div className="login-page"><div className="login-panel expressive-surface"><Brand /><div className="dialog-icon"><ShieldCheck /></div><h1>{t('admin.login')}</h1><form className="form-stack" onSubmit={(event) => { event.preventDefault(); login.mutate() }}><label className="field"><span>{t('admin.username')}</span><input autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} required /></label><label className="field"><span>{t('admin.password')}</span><input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>{login.error && <ErrorNotice error={login.error} />}<button className="button button--primary button--large" disabled={login.isPending}>{t('admin.signIn')}<ArrowRight /></button></form></div></div>
}

function AdminChangePassword() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [mismatch, setMismatch] = useState(false)
  const change = useMutation({ mutationFn: () => api.changeAdminPassword(currentPassword, newPassword), onSuccess: () => navigate('/admin', { replace: true }) })
  return <div className="login-page"><div className="login-panel expressive-surface"><Brand /><div className="dialog-icon"><KeyRound /></div><h1>{t('admin.changePassword')}</h1><p>{t('admin.changePasswordHelp')}</p><form className="form-stack" onSubmit={(event) => { event.preventDefault(); if (newPassword !== confirmPassword) { setMismatch(true); return } setMismatch(false); change.mutate() }}><label className="field"><span>{t('admin.currentPassword')}</span><input type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} required /></label><label className="field"><span>{t('admin.newPassword')}</span><input type="password" autoComplete="new-password" minLength={12} value={newPassword} onChange={(event) => setNewPassword(event.target.value)} required /></label><label className="field"><span>{t('admin.confirmPassword')}</span><input type="password" autoComplete="new-password" minLength={12} value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} required /></label>{mismatch && <p className="error-text">{t('admin.passwordMismatch')}</p>}{change.error && <ErrorNotice error={change.error} />}<button className="button button--primary button--large" disabled={change.isPending}>{t('admin.changePassword')}<ArrowRight /></button></form></div></div>
}

function AdminOverviewPage() {
  const { t } = useTranslation(); const format = useFormat()
  const query = useQuery({ queryKey: ['admin', 'overview'], queryFn: api.adminOverview, refetchInterval: 30_000 })
  if (query.error) return <ErrorNotice error={query.error} retry={() => void query.refetch()} />
  const data = query.data
  const items = [
    ['serverVersion', data?.version ?? '—', Cloud], ['uptime', data ? format.duration(data.uptimeSeconds) : '—', Activity],
    ['nodesOnline', data?.nodesOnline ?? '—', Server], ['activeSessions', data?.activeSessions ?? '—', Users],
    ['testsToday', data?.testsToday ?? '—', Zap], ['trafficToday', data ? format.bytes(data.trafficTodayBytes) : '—', Signal],
  ] as const
  return <AdminPage title={t('nav.overview')}><div className="admin-metrics">{items.map(([key, value, Icon]) => <div className="admin-metric" key={key}><Icon /><span>{t(`admin.${key}`)}</span><strong>{value}</strong></div>)}</div></AdminPage>
}

function AdminAccessPage() {
  const { t } = useTranslation(); const queryClient = useQueryClient(); const [invite, setInvite] = useState('')
  const query = useQuery({ queryKey: ['admin', 'access'], queryFn: api.adminAccess, refetchInterval: 10_000 })
  const rotate = useMutation({ mutationFn: api.rotateCode, onSuccess: (data) => queryClient.setQueryData(['admin', 'access'], data) })
  const createInvite = useMutation({ mutationFn: api.createInvite, onSuccess: (data) => setInvite(data.url) })
  if (query.error) return <ErrorNotice error={query.error} retry={() => void query.refetch()} />
  return <AdminPage title={t('admin.privateAccess')}><section className="access-code-card expressive-surface"><span className="eyebrow"><KeyRound />{query.data?.enabled ? t('common.enabled') : t('common.disabled')}</span><span>{t('admin.currentCode')}</span><strong className="access-code">{(query.data?.currentCode ?? '------').replace(/(\d{3})(\d{3})/, '$1 $2')}</strong><span>{query.data?.changesAt ? t('admin.changesIn', { time: new Intl.DateTimeFormat(undefined, { minute: '2-digit', second: '2-digit' }).format(new Date(query.data.changesAt)) }) : '—'}</span><div className="button-row"><CopyButton value={query.data?.currentCode ?? ''} /><button className="button button--tonal" onClick={() => rotate.mutate()}><RefreshCw />{t('admin.rotate')}</button></div></section><section className="surface"><div className="section-heading"><h2>{t('admin.createInvite')}</h2><button className="button button--primary" onClick={() => createInvite.mutate()}><Plus />{t('common.create')}</button></div>{invite && <div className="command-box"><code>{invite}</code><CopyButton value={invite} /></div>}</section></AdminPage>
}

function CopyButton({ value }: { value: string }) { const { t } = useTranslation(); const [copied, setCopied] = useState(false); return <button className="button button--tonal" onClick={async () => { await navigator.clipboard.writeText(value); setCopied(true); setTimeout(() => setCopied(false), 1400) }}>{copied ? <Check /> : <Copy />}{t(copied ? 'common.copied' : 'common.copy')}</button> }

function AdminNodesPage() {
  const { t } = useTranslation(); const queryClient = useQueryClient(); const query = useQuery({ queryKey: ['admin', 'nodes'], queryFn: api.adminNodes, refetchInterval: 15_000 }); const [open, setOpen] = useState(false); const [form, setForm] = useState({ name: '', region: '', city: '' }); const [command, setCommand] = useState('')
  const add = useMutation({ mutationFn: () => api.addNode(form), onSuccess: async (data) => { setCommand(data.installCommand); await queryClient.invalidateQueries({ queryKey: ['admin', 'nodes'] }) } })
  return <AdminPage title={t('nav.nodes')} action={<button className="button button--primary" onClick={() => { setCommand(''); setOpen(true) }}><Plus />{t('admin.addNode')}</button>}>{query.error && <ErrorNotice error={query.error} retry={() => void query.refetch()} />}<div className="node-grid">{query.data?.map((node) => <article className="node-card" key={node.id}><div className="section-heading"><span className={`status-dot status-dot--${node.status}`} /><span className="chip">{node.tlsStatus === 'ready' ? <ShieldCheck /> : <LockKeyhole />}{t('admin.tls')}: {node.tlsStatus}</span></div><h2>{node.name}</h2><p>{node.city} · {node.region}</p><dl><div><dt>IP</dt><dd>{node.ipv4 ?? '—'}</dd></div><div><dt>ASN</dt><dd>{node.asn ?? '—'}</dd></div><div><dt>{t('admin.version')}</dt><dd>{node.version ?? '—'}</dd></div></dl><NavLink className="button button--tonal" to={`/admin/nodes/${node.id}`}>{t('admin.editNode')}<ChevronRight /></NavLink></article>)}</div><Dialog.Root open={open} onOpenChange={setOpen}><Dialog.Portal><Dialog.Overlay className="dialog-scrim" /><Dialog.Content className="dialog sheet"><Dialog.Close className="dialog-close"><X /></Dialog.Close><Dialog.Title>{t('admin.addNode')}</Dialog.Title>{command ? <div className="form-stack"><p>{t('admin.installCommand')}</p><div className="command-box"><code>{command}</code></div><CopyButton value={command} /></div> : <form className="form-stack" onSubmit={(event) => { event.preventDefault(); add.mutate() }}>{(['name', 'region', 'city'] as const).map((field) => <label className="field" key={field}><span>{t(`admin.${field === 'name' ? 'nodeName' : field}`)}</span><input value={form[field]} onChange={(event) => setForm({ ...form, [field]: event.target.value })} required /></label>)}{add.error && <ErrorNotice error={add.error} />}<button className="button button--primary" disabled={add.isPending}>{t('common.create')}</button></form>}</Dialog.Content></Dialog.Portal></Dialog.Root></AdminPage>
}

function AdminNodeSettingsPage() {
  const { t } = useTranslation(); const { id = '' } = useParams(); const navigate = useNavigate(); const queryClient = useQueryClient()
  const query = useQuery({ queryKey: ['admin', 'nodes'], queryFn: api.adminNodes })
  const node = query.data?.find((item) => item.id === id)
  if (query.isLoading) return <AdminPage title={t('admin.editNode')}><p>{t('common.loading')}</p></AdminPage>
  if (!node) return <AdminPage title={t('admin.editNode')}><ErrorNotice error={query.error ?? new Error('NODE_NOT_FOUND')} retry={() => void query.refetch()} /></AdminPage>
  return <AdminNodeEditor key={node.id} node={node} navigate={navigate} queryClient={queryClient} />
}

function AdminNodeEditor({ node, navigate, queryClient }: { node: NodeInfo; navigate: ReturnType<typeof useNavigate>; queryClient: ReturnType<typeof useQueryClient> }) {
  const { t } = useTranslation(); const id = node.id
  const [form, setForm] = useState({ name: node.name, region: node.region, city: node.city, endpoint: node.endpoint, latitude: node.latitude?.toString() ?? '', longitude: node.longitude?.toString() ?? '', enabled: node.enabled ?? true })
  const save = useMutation({ mutationFn: () => api.updateNode(id, { name: form.name, region: form.region, city: form.city, endpoint: form.endpoint, latitude: form.latitude === '' ? undefined : Number(form.latitude), longitude: form.longitude === '' ? undefined : Number(form.longitude), enabled: form.enabled }), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'nodes'] }) })
  const remove = useMutation({ mutationFn: () => api.deleteNode(id), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['admin', 'nodes'] }); navigate('/admin/nodes') } })
  return <AdminPage title={node.name} action={<NavLink className="button button--text" to="/admin/nodes"><ArrowLeft />{t('nav.nodes')}</NavLink>}><form className="surface settings-form" onSubmit={(event) => { event.preventDefault(); save.mutate() }}><label className="field"><span>{t('admin.nodeName')}</span><input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} required /></label><label className="field"><span>{t('admin.region')}</span><input value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} /></label><label className="field"><span>{t('admin.city')}</span><input value={form.city} onChange={(event) => setForm({ ...form, city: event.target.value })} /></label><label className="field"><span>{t('admin.endpoint')}</span><input value={form.endpoint} onChange={(event) => setForm({ ...form, endpoint: event.target.value })} /></label><label className="field"><span>{t('admin.latitude')}</span><input type="number" min="-90" max="90" step="any" value={form.latitude} onChange={(event) => setForm({ ...form, latitude: event.target.value })} /></label><label className="field"><span>{t('admin.longitude')}</span><input type="number" min="-180" max="180" step="any" value={form.longitude} onChange={(event) => setForm({ ...form, longitude: event.target.value })} /></label><label className="toggle-row"><span><strong>{t('admin.nodeEnabled')}</strong><small>{node.status}</small></span><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} /></label>{(save.error || remove.error) && <ErrorNotice error={save.error ?? remove.error} />}<div className="form-actions"><button className="button button--primary" disabled={save.isPending}>{t('common.save')}</button><button type="button" className="button button--text button--danger" disabled={node.status !== 'offline' || remove.isPending} onClick={() => remove.mutate()}>{t('admin.deleteNode')}</button></div></form></AdminPage>
}

function AdminSessionsPage() {
  const { t } = useTranslation(); const format = useFormat(); const queryClient = useQueryClient(); const query = useQuery({ queryKey: ['admin', 'sessions'], queryFn: api.sessions }); const revoke = useMutation({ mutationFn: api.revokeSession, onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'sessions'] }) })
  return <AdminPage title={t('nav.sessions')}><div className="session-list">{query.data?.map((session) => <div className="session-row" key={session.id}><span className="server-icon"><Users /></span><span><strong>{session.network ?? session.asn ?? session.ip}</strong><small>{session.ip} · {session.region ?? t('common.unknown')}</small></span><span><small>{t('admin.sessionExpiry')}</small><strong>{format.date(session.expiresAt)}</strong></span><span><small>{t('admin.testsUsed')}</small><strong>{session.testsUsed}</strong></span><button className="button button--text button--danger" onClick={() => revoke.mutate(session.id)}>{t('common.revoke')}</button></div>)}</div></AdminPage>
}

function AdminSettingsPage() {
  const { t } = useTranslation(); const [theme, setThemeState] = useState(readTheme); const [motion, setMotionState] = useState(document.documentElement.dataset.motion !== 'disabled'); const [map, setMap] = useState(localStorage.getItem('routeglass.map.v1') === 'route' ? 'route' : 'globe')
  return <AdminPage title={t('nav.settings')}><section className="surface settings-form"><h2>{t('admin.branding')}</h2><label className="field"><span>{t('common.theme')}</span><select value={theme.mode} onChange={(event) => { const next = { ...theme, mode: event.target.value as ThemeMode }; setThemeState(next); void transitionTheme(next) }}><option value="system">{t('common.system')}</option><option value="light">{t('common.light')}</option><option value="dark">{t('common.dark')}</option></select></label><label className="field"><span>{t('admin.seed')}</span><input type="color" value={theme.seed} onChange={(event) => { const next = { ...theme, seed: event.target.value }; setThemeState(next); void transitionTheme(next) }} /></label><label className="toggle-row"><span><strong>{t('admin.motion')}</strong><small>prefers-reduced-motion</small></span><input type="checkbox" checked={motion} onChange={(event) => { setMotionState(event.target.checked); setMotion(event.target.checked) }} /></label><label className="field"><span>{t('admin.mapDefault')}</span><select value={map} onChange={(event) => { setMap(event.target.value); localStorage.setItem('routeglass.map.v1', event.target.value) }}><option value="globe">{t('result.globe')}</option><option value="route">{t('result.route')}</option></select></label><p>{t('admin.operational')}</p></section></AdminPage>
}

function AdminPage({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) { return <div className="page admin-page"><div className="page-title"><div><span className="eyebrow"><ShieldCheck />RouteGlass</span><h1>{title}</h1></div>{action}</div>{children}</div> }

export default function App() {
  return <Routes>
    <Route element={<AppShell />}><Route index element={<HomePage />} /><Route path="test" element={<TestPage />} /><Route path="results/:id" element={<ResultsPage />} /><Route path="tools" element={<ToolsPage />} /><Route path="invite/:token" element={<InvitePage />} /></Route>
    <Route path="admin/login" element={<AdminLogin />} />
    <Route path="admin/change-password" element={<AdminChangePassword />} />
    <Route path="admin" element={<AppShell admin />}><Route index element={<AdminOverviewPage />} /><Route path="access" element={<AdminAccessPage />} /><Route path="nodes" element={<AdminNodesPage />} /><Route path="nodes/:id" element={<AdminNodeSettingsPage />} /><Route path="sessions" element={<AdminSessionsPage />} /><Route path="settings" element={<AdminSettingsPage />} /></Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>
}
