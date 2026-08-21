vi.mock('@material/material-color-utilities', () => ({
  argbFromHex: () => 0xff00639b,
  hexFromArgb: (value: number) => `#${(value & 0xffffff).toString(16).padStart(6, '0')}`,
  themeFromSourceColor: () => {
    const scheme = new Proxy({}, { get: () => 0xff00639b })
    return { schemes: { light: scheme, dark: scheme }, palettes: { neutral: { tone: () => 0xfff0f0f0 } } }
  },
}))

import { applyThemePreference, DEFAULT_SEED, readTheme, setMotion } from '../src/theme'

describe('theme preferences', () => {
  beforeEach(() => localStorage.clear())

  it('rejects stale and invalid stored values', () => {
    localStorage.setItem('routeglass.theme.v1', JSON.stringify({ mode: 'sepia', seed: 'javascript:red' }))
    expect(readTheme()).toEqual({ mode: 'system', seed: DEFAULT_SEED })
  })

  it('maps a valid seed to semantic Material roles', () => {
    applyThemePreference({ mode: 'light', seed: '#00639b' })
    expect(document.documentElement.style.getPropertyValue('--md-primary')).toMatch(/^#[0-9a-f]{6}$/i)
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('persists the application motion override', () => {
    setMotion(false)
    expect(document.documentElement.dataset.motion).toBe('disabled')
    expect(localStorage.getItem('routeglass.motion.v1')).toBe('disabled')
  })
})
