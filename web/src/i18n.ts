import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import type { Locale } from './types'

const en = {
  translation: {
    nav: { home: 'Home', test: 'Smart test', tools: 'Tools', overview: 'Overview', access: 'Access', nodes: 'Nodes', sessions: 'Sessions', settings: 'Settings', more: 'More' },
    common: { loading: 'Loading…', retry: 'Try again', cancel: 'Cancel', close: 'Close', copy: 'Copy', copied: 'Copied', save: 'Save', create: 'Create', revoke: 'Revoke', online: 'Online', offline: 'Offline', enabled: 'Enabled', disabled: 'Disabled', automatic: 'Automatic', recommended: 'Recommended', unknown: 'Unknown', signOut: 'Sign out', theme: 'Theme', language: 'Language', light: 'Light', dark: 'Dark', system: 'System' },
    home: { eyebrow: 'Private network diagnostics', title: 'See the route, not just the speed.', subtitle: 'Compare your private nodes, measure the real browser path, and understand every known return hop.', yourNetwork: 'Your network', start: 'Start smart test', chooseServer: 'Automatic server', chooseHint: "We'll choose the best available node", private: 'Private diagnostics', accessRequired: 'Access code required', authorized: 'Access active', lastTest: 'Last tested', ipv4: 'IPv4', ipv6: 'IPv6' },
    access: { title: 'Enter private access', description: 'Ask the RouteGlass administrator for the current six-digit code.', label: 'Access code', placeholder: '000 000', submit: 'Continue', invalid: 'Enter six digits.', rateLimited: 'Too many attempts. Try again in {{seconds}}s.' },
    servers: { title: 'Choose a server', automatic: 'Automatic · Recommended', manual: 'Manual test', unavailable: 'Speedtest endpoint unavailable', empty: 'No nodes are available.' },
    test: { title: 'Smart test', checking: 'Checking nodes', probing: 'Checking node quality', latency: 'Testing latency', download: 'Testing download', upload: 'Testing upload', route: 'Tracing return route', analyzing: 'Analyzing results', complete: 'Test complete', nodeProgress: 'Node {{current}} of {{total}}', measured: '{{value}} Mbps', failed: 'This test stopped before completion.', noCandidates: 'No eligible nodes responded.', restart: 'Start again' },
    result: { title: 'Your best route', excellent: 'Excellent', good: 'Good', fair: 'Fair', poor: 'Poor', bestFor: 'Best overall performance for your network', overall: 'Overall', lowLatency: 'Low latency', bandwidth: 'High bandwidth', download: 'Download', upload: 'Upload', latency: 'Latency', jitter: 'Jitter', loss: 'Loss', globe: 'Globe', route: 'Route', routeTitle: 'Return route', hop: 'Hop', network: 'Network', location: 'Location', unknownSegment: '{{count}} location-unknown hops', approximate: 'Approximate location', noRoute: 'No route geography was returned.' },
    admin: { title: 'RouteGlass Admin', login: 'Admin sign in', username: 'Username', password: 'Password', signIn: 'Sign in', changePassword: 'Change password', changePasswordHelp: 'Replace the one-time password before using the admin console.', currentPassword: 'Current password', newPassword: 'New password', confirmPassword: 'Confirm password', passwordMismatch: 'The new passwords do not match.', serverVersion: 'Server version', uptime: 'Uptime', nodesOnline: 'Nodes online', activeSessions: 'Active sessions', testsToday: 'Tests today', trafficToday: 'Traffic today', privateAccess: 'Private access', currentCode: 'Current code', changesIn: 'Changes in {{time}}', rotate: 'Rotate now', createInvite: 'Create invite', inviteReady: 'One-time invite', addNode: 'Add node', editNode: 'Edit node', deleteNode: 'Delete offline node', nodeName: 'Node name', region: 'Region', city: 'City', endpoint: 'HTTPS endpoint', latitude: 'Latitude', longitude: 'Longitude', nodeEnabled: 'Node enabled', installCommand: 'Run this command once on the node', tls: 'TLS', version: 'Version', sessionExpiry: 'Expires', testsUsed: 'Tests used', branding: 'Branding & appearance', seed: 'Theme seed', motion: 'Motion', mapDefault: 'Default route view', operational: 'Operational settings are loaded from the server configuration.' },
    errors: { NETWORK_ERROR: 'RouteGlass is not reachable. Check the connection and try again.', INVALID_ACCESS_CODE: 'That access code is not valid.', ACCESS_RATE_LIMITED: 'Access is temporarily rate limited.', SESSION_EXPIRED: 'Your private access session has expired.', AGENT_UNAVAILABLE: 'The node stopped responding.', AGENT_TLS_UNAVAILABLE: 'The node does not have a browser-trusted TLS endpoint.', TOKEN_EXPIRED: 'The short-lived test permission expired.', NO_ELIGIBLE_NODES: 'No eligible nodes are ready for testing.', UNAUTHORIZED: 'Sign in to continue.', UNKNOWN_ERROR: 'Something unexpected happened.' }
  }
}

const zhCN = {
  translation: {
    nav: { home: '首页', test: '测速', tools: '工具', overview: '概览', access: '私有访问', nodes: '节点', sessions: '会话', settings: '设置', more: '更多' },
    common: { loading: '正在加载…', retry: '重试', cancel: '取消', close: '关闭', copy: '复制', copied: '已复制', save: '保存', create: '创建', revoke: '撤销', online: '在线', offline: '离线', enabled: '已启用', disabled: '已停用', automatic: '自动', recommended: '推荐', unknown: '未知', signOut: '退出', theme: '主题', language: '语言', light: '浅色', dark: '深色', system: '跟随系统' },
    home: { eyebrow: '网络诊断', title: '看清速度与路径', subtitle: '比较节点，测速并查看回程。', yourNetwork: '你的网络', start: '开始测速', chooseServer: '自动选择', chooseHint: '按实测推荐', private: '私有诊断', accessRequired: '需要访问码', authorized: '访问已授权', lastTest: '上次测试', ipv4: 'IPv4', ipv6: 'IPv6' },
    access: { title: '输入访问码', description: '输入六位动态码。', label: '访问码', placeholder: '000 000', submit: '继续', invalid: '请输入六位数字。', rateLimited: '{{seconds}} 秒后重试。' },
    servers: { title: '选择服务器', automatic: '自动 · 推荐', manual: '手动测试', unavailable: '测速端点不可用', empty: '暂无可用节点。' },
    test: { title: '测速', checking: '检查节点', probing: '探测节点', latency: '测试延迟', download: '测试下载', upload: '测试上传', route: '追踪路由', analyzing: '分析结果', complete: '测试完成', nodeProgress: '节点 {{current}} / {{total}}', measured: '{{value}} Mbps', failed: '测试中断。', noCandidates: '节点无响应。', restart: '重新开始' },
    result: { title: '最佳节点', excellent: '优秀', good: '良好', fair: '一般', poor: '较差', bestFor: '综合表现最佳', overall: '综合', lowLatency: '低延迟', bandwidth: '高带宽', download: '下载', upload: '上传', latency: '延迟', jitter: '抖动', loss: '丢包', globe: '地球', route: '路由', routeTitle: '回程路由', hop: '跳', network: '网络', location: '位置', unknownSegment: '{{count}} 跳位置未知', approximate: '近似位置', noRoute: '暂无路由地理信息。' },
    admin: { title: 'RouteGlass 管理', login: '管理员登录', username: '用户名', password: '密码', signIn: '登录', changePassword: '修改密码', changePasswordHelp: '请先修改临时密码。', currentPassword: '当前密码', newPassword: '新密码', confirmPassword: '确认新密码', passwordMismatch: '两次密码不一致。', serverVersion: '服务端版本', uptime: '运行时间', nodesOnline: '在线节点', activeSessions: '活跃会话', testsToday: '今日测试', trafficToday: '今日流量', privateAccess: '私有访问', currentCode: '当前动态码', changesIn: '{{time}} 后更新', rotate: '立即轮换', createInvite: '创建邀请', inviteReady: '一次性邀请', addNode: '添加节点', editNode: '编辑节点', deleteNode: '删除离线节点', nodeName: '节点名称', region: '地区', city: '城市', endpoint: 'HTTPS 端点', latitude: '纬度', longitude: '经度', nodeEnabled: '启用节点', installCommand: '在节点执行', tls: 'TLS', version: '版本', sessionExpiry: '到期时间', testsUsed: '已用测试', branding: '品牌与外观', seed: '主题种子色', motion: '动效', mapDefault: '默认路由视图', operational: '服务端配置' },
    errors: { NETWORK_ERROR: '连接失败，请重试。', INVALID_ACCESS_CODE: '访问码不正确。', ACCESS_RATE_LIMITED: '请求过于频繁。', SESSION_EXPIRED: '会话已过期。', AGENT_UNAVAILABLE: '节点无响应。', AGENT_TLS_UNAVAILABLE: '节点 TLS 不可用。', TOKEN_EXPIRED: '测速授权已过期。', NO_ELIGIBLE_NODES: '暂无可测速节点。', UNAUTHORIZED: '请先登录。', UNKNOWN_ERROR: '操作失败。' }
  }
}

const stored = localStorage.getItem('routeglass.locale.v1')
const browserLocale: Locale = navigator.languages.some((value) => value.toLowerCase().startsWith('zh')) ? 'zh-CN' : 'en'

void i18n.use(initReactI18next).init({
  resources: { en, 'zh-CN': zhCN },
  lng: stored === 'en' || stored === 'zh-CN' ? stored : browserLocale,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

i18n.on('languageChanged', (language) => {
  const locale: Locale = language.startsWith('zh') ? 'zh-CN' : 'en'
  localStorage.setItem('routeglass.locale.v1', locale)
  document.documentElement.lang = locale
  document.documentElement.dir = i18n.dir(locale)
})

document.documentElement.lang = i18n.language
document.documentElement.dir = i18n.dir()

export default i18n
