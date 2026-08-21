import '@testing-library/jest-dom/vitest'
import { vi } from 'vitest'

const storage = new Map<string, string>()
const localStorageMock: Storage = {
  get length() { return storage.size },
  clear: () => storage.clear(),
  getItem: (key) => storage.get(key) ?? null,
  key: (index) => [...storage.keys()][index] ?? null,
  removeItem: (key) => { storage.delete(key) },
  setItem: (key, value) => { storage.set(key, String(value)) },
}
Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true })
Object.defineProperty(window, 'localStorage', { value: localStorageMock, configurable: true })
Object.defineProperty(globalThis, 'sessionStorage', { value: localStorageMock, configurable: true })
Object.defineProperty(window, 'sessionStorage', { value: localStorageMock, configurable: true })

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
})

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.defineProperty(window, 'ResizeObserver', { value: ResizeObserverMock })
