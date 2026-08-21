import { useTranslation } from 'react-i18next'
import { useMapData, type Feature, type MapProps } from './map-data'

const project = ([longitude, latitude]: number[], width: number, height: number): [number, number] => [
  ((longitude ?? 0) + 180) / 360 * width,
  (90 - (latitude ?? 0)) / 180 * height,
]

function ringPath(ring: number[][], width: number, height: number) {
  return ring.map((coordinate, index) => {
    const [x, y] = project(coordinate, width, height)
    return `${index ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ') + ' Z'
}

function featurePath(feature: Feature, width: number, height: number) {
  const polygons = feature.geometry.type === 'Polygon'
    ? [feature.geometry.coordinates as number[][][]]
    : feature.geometry.coordinates as number[][][][]
  return polygons.flatMap((polygon) => polygon.map((ring) => ringPath(ring, width, height))).join(' ')
}

export function RouteMap2D({ hops, selectedHop, onSelect }: MapProps) {
  const data = useMapData()
  const { t } = useTranslation()
  const width = 1000
  const height = 500
  const known = hops.filter((hop) => hop.latitude != null && hop.longitude != null)
  return (
    <div className="route-map" aria-label={t('result.routeTitle')}>
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-labelledby="route-map-title">
        <title id="route-map-title">{t('result.routeTitle')}</title>
        <rect width={width} height={height} className="map-ocean" />
        <g className="map-land">
          {data?.features.map((feature, index) => <path key={index} d={featurePath(feature, width, height)} />)}
        </g>
        <g className="map-arcs">
          {known.slice(1).map((hop, index) => {
            const previous = known[index]!
            const start = project([previous.longitude!, previous.latitude!], width, height)
            const end = project([hop.longitude!, hop.latitude!], width, height)
            const mx = (start[0] + end[0]) / 2
            const my = Math.min(start[1], end[1]) - Math.min(70, Math.abs(end[0] - start[0]) * .12)
            const unknown = hop.hop - previous.hop > 1
            return <path key={`${previous.hop}-${hop.hop}`} d={`M${start[0]},${start[1]} Q${mx},${my} ${end[0]},${end[1]}`} className={unknown ? 'map-arc map-arc--unknown' : 'map-arc'} />
          })}
        </g>
        <g>
          {known.map((hop) => {
            const [x, y] = project([hop.longitude!, hop.latitude!], width, height)
            return (
              <g key={hop.hop} transform={`translate(${x} ${y})`} className={`map-point ${hop.geoConfidence === 'approximate' ? 'map-point--approximate' : ''} ${selectedHop === hop.hop ? 'map-point--selected' : ''}`} onClick={() => onSelect(hop.hop)} role="button" tabIndex={0} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') onSelect(hop.hop) }} aria-label={`${t('result.hop')} ${hop.hop}, ${hop.city ?? hop.country ?? t('common.unknown')}`}>
                <circle r={selectedHop === hop.hop ? 9 : 6} />
                <text x="11" y="4">{hop.hop}</text>
              </g>
            )
          })}
        </g>
      </svg>
    </div>
  )
}
