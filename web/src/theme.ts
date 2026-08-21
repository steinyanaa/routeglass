import { argbFromHex, hexFromArgb, themeFromSourceColor } from '@material/material-color-utilities'
import { seedSchema } from './api'
import type { ThemeMode } from './types'

const THEME_KEY = 'routeglass.theme.v1'
const MOTION_KEY = 'routeglass.motion.v1'
export const DEFAULT_SEED = '#00639b'

export interface ThemePreference { mode: ThemeMode; seed: string }

export function readTheme(): ThemePreference {
  try {
    const value = JSON.parse(localStorage.getItem(THEME_KEY) ?? '{}')
    const mode: ThemeMode = ['light', 'dark', 'system'].includes(value.mode) ? value.mode : 'system'
    const seed = seedSchema.safeParse(value.seed).success ? value.seed : DEFAULT_SEED
    return { mode, seed }
  } catch {
    return { mode: 'system', seed: DEFAULT_SEED }
  }
}

export function resolvedDark(mode: ThemeMode) {
  return mode === 'dark' || (mode === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
}

export function applyThemePreference(preference: ThemePreference) {
  const dark = resolvedDark(preference.mode)
  const theme = themeFromSourceColor(argbFromHex(preference.seed))
  const scheme = dark ? theme.schemes.dark : theme.schemes.light
  const neutral = theme.palettes.neutral
  const root = document.documentElement
  const values: Record<string, number> = {
    primary: scheme.primary, 'on-primary': scheme.onPrimary,
    'primary-container': scheme.primaryContainer, 'on-primary-container': scheme.onPrimaryContainer,
    secondary: scheme.secondary, 'on-secondary': scheme.onSecondary,
    'secondary-container': scheme.secondaryContainer, 'on-secondary-container': scheme.onSecondaryContainer,
    tertiary: scheme.tertiary, 'on-tertiary': scheme.onTertiary,
    'tertiary-container': scheme.tertiaryContainer, 'on-tertiary-container': scheme.onTertiaryContainer,
    error: scheme.error, 'on-error': scheme.onError, 'error-container': scheme.errorContainer,
    'on-error-container': scheme.onErrorContainer, surface: scheme.surface, 'on-surface': scheme.onSurface,
    'surface-variant': scheme.surfaceVariant, 'on-surface-variant': scheme.onSurfaceVariant,
    outline: scheme.outline, 'outline-variant': scheme.outlineVariant,
    'inverse-surface': scheme.inverseSurface, 'inverse-on-surface': scheme.inverseOnSurface,
    'surface-container-lowest': neutral.tone(dark ? 4 : 100),
    'surface-container-low': neutral.tone(dark ? 10 : 96),
    'surface-container': neutral.tone(dark ? 12 : 94),
    'surface-container-high': neutral.tone(dark ? 17 : 92),
    'surface-container-highest': neutral.tone(dark ? 22 : 90),
  }
  for (const [name, value] of Object.entries(values)) root.style.setProperty(`--md-${name}`, hexFromArgb(value))
  root.dataset.theme = dark ? 'dark' : 'light'
  root.style.colorScheme = dark ? 'dark' : 'light'
  document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute('content', hexFromArgb(scheme.primary))
  localStorage.setItem(THEME_KEY, JSON.stringify(preference))
}

export async function transitionTheme(preference: ThemePreference) {
  if (motionDisabled()) return applyThemePreference(preference)
  const curtain = document.createElement('div')
  curtain.className = 'theme-curtain theme-curtain--closing'
  curtain.style.setProperty('--curtain-a', getComputedStyle(document.documentElement).getPropertyValue('--md-primary-container'))
  curtain.style.setProperty('--curtain-b', getComputedStyle(document.documentElement).getPropertyValue('--md-secondary-container'))
  document.body.append(curtain)
  try {
    await curtain.getAnimations()[0]?.finished
    applyThemePreference(preference)
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
    curtain.classList.replace('theme-curtain--closing', 'theme-curtain--opening')
    await curtain.getAnimations()[0]?.finished
  } finally {
    curtain.remove()
  }
}

export function motionDisabled() {
  return localStorage.getItem(MOTION_KEY) === 'disabled' || matchMedia('(prefers-reduced-motion: reduce)').matches
}

export function setMotion(enabled: boolean) {
  localStorage.setItem(MOTION_KEY, enabled ? 'enabled' : 'disabled')
  document.documentElement.dataset.motion = enabled ? 'enabled' : 'disabled'
}

export function initializeTheme() {
  document.documentElement.dataset.motion = localStorage.getItem(MOTION_KEY) === 'disabled' ? 'disabled' : 'enabled'
  applyThemePreference(readTheme())
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    const preference = readTheme()
    if (preference.mode === 'system') applyThemePreference(preference)
  })
}
