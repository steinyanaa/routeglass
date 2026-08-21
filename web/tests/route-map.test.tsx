import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import i18n from '../src/i18n'
import { RouteMap2D } from '../src/route-map'
import type { RouteHop } from '../src/types'

const hops: RouteHop[] = [
  { hop: 1, ip: '192.0.2.1', asn: 'AS1', network: 'Origin', hostname: null, rttMs: 1, lossPercent: 0, country: 'US', city: 'Los Angeles', latitude: 34, longitude: -118, geoConfidence: 'exact' },
  { hop: 2, ip: null, asn: null, network: null, hostname: null, rttMs: null, lossPercent: null, country: null, city: null, latitude: null, longitude: null, geoConfidence: 'unknown' },
  { hop: 3, ip: '192.0.2.3', asn: 'AS3', network: 'Transit', hostname: null, rttMs: 60, lossPercent: 0, country: 'JP', city: 'Tokyo', latitude: 35, longitude: 139, geoConfidence: 'approximate' },
]

describe('2D route map', () => {
  it('draws only known coordinates and marks unknown spans', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ features: [] }), { status: 200 }))
    const onSelect = vi.fn()
    const { container } = render(<I18nextProvider i18n={i18n}><RouteMap2D hops={hops} selectedHop={null} onSelect={onSelect} /></I18nextProvider>)
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(container.querySelectorAll('.map-point')).toHaveLength(2)
    expect(container.querySelector('.map-arc--unknown')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Hop 1/i }))
    expect(onSelect).toHaveBeenCalledWith(1)
  })
})
