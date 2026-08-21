import { useEffect, useState } from 'react'
import type { RouteHop } from './types'

export interface MapProps { hops: RouteHop[]; selectedHop: number | null; onSelect: (hop: number) => void }
export interface Feature { geometry: { type: 'Polygon' | 'MultiPolygon'; coordinates: unknown } }
export interface FeatureCollection { features: Feature[] }

export function useMapData() {
  const [data, setData] = useState<FeatureCollection | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    fetch('/data/ne_110m_admin_0_countries.geojson', { signal: controller.signal })
      .then((response) => response.json())
      .then(setData)
      .catch(() => undefined)
    return () => controller.abort()
  }, [])
  return data
}
